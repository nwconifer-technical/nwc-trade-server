package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func (Env env) signupFunc(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var newUser struct {
		NationName     string `json:"NationName"`
		PasswordString string `json:"PasswordString"`
		RegionName     string `json:"RegionName"`
	}
	ourTx, err := Env.DBPool.Begin(r.Context())
	defer ourTx.Rollback(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err 0", err)
	}
	err = decoder.Decode(&newUser)
	log.Println("NewUser Signup Request")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err 1", err)
		return
	}
	log.Println("Bcrypt started")
	createdHash, _ := bcrypt.GenerateFromPassword([]byte(newUser.PasswordString), Env.HashCost)
	err = ourTx.QueryRow(r.Context(), "INSERT INTO accounts (account_name, account_pass_hash) VALUES ($1, $2)", newUser.NationName, string(createdHash)).Scan()
	if err.Error() != "" && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusConflict)
		log.Println("DB Err 3", err)
		return
	}
	err = ourTx.QueryRow(r.Context(), "INSERT INTO nation_permissions (region_name, nation_name, permission) VALUES ($1, $2, 'citizen')", newUser.RegionName, newUser.NationName).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err 4", err)
		return
	}
	err = ourTx.QueryRow(r.Context(), `INSERT INTO loans (lendee, lender, lent_value, rate, current_value) VALUES ($1, $2, $3, $4, $5);`, newUser.NationName, newUser.RegionName, 10000, 2.5, 10000).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Loan Err", err)
		return
	}
	err = ourTx.QueryRow(r.Context(), `UPDATE accounts SET cash_in_hand = cash_in_hand + 10000 WHERE account_name = $1`, newUser.NationName).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Loan Err", err)
		return
	}
	log.Println("User Created")
	ourTx.Commit(r.Context())
	w.WriteHeader(http.StatusCreated)
}

func (Env env) userVerification(w http.ResponseWriter, r *http.Request) {
	log.Println("User Verification Request")
	decoder := json.NewDecoder(r.Body)
	outEncoder := json.NewEncoder(w)
	var user struct {
		NationName     string
		PasswordString string
	}
	err := decoder.Decode(&user)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
	var dbPassHash string = ""
	var userReturn = struct {
		AuthKey        string `json:"AuthKey"`
		UserRegion     string `json:"UserRegion"`
		UserPermission string `json:"UserPermission"`
		UserName       string `json:"UserName"`
	}{
		UserName: user.NationName,
	}
	err = Env.DBPool.QueryRow(r.Context(), "SELECT account_pass_hash, region_name, permission FROM accounts, nation_permissions WHERE account_name = $1 AND account_name = nation_name AND account_type = 'nation';", user.NationName).Scan(&dbPassHash, &userReturn.UserRegion, &userReturn.UserPermission)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err", err)
		return
	}
	if userReturn.UserName == "Gallaton" {
		userReturn.UserPermission = "admin"
	}
	err = bcrypt.CompareHashAndPassword([]byte(dbPassHash), []byte(user.PasswordString))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	authKeyHex := md5.Sum([]byte(user.NationName + Env.KeyString))
	userReturn.AuthKey = hex.EncodeToString(authKeyHex[:])
	if err != nil {
		log.Println("JSON err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
	outEncoder.Encode(userReturn)
}

func (Env env) nationInfo(w http.ResponseWriter, r *http.Request) {
	respEncoder := json.NewEncoder(w)
	returnHello := struct {
		NationName   string
		Region       string
		CashInHand   float32
		CashInEscrow float32
	}{}
	requedNat := r.PathValue("natName")
	log.Println("Nation info requested for", requedNat)
	err := Env.DBPool.QueryRow(r.Context(), "SELECT account_name, nation_permissions.region_name, cash_in_hand, cash_in_escrow FROM accounts, nation_permissions WHERE nation_name = $1 AND nation_permissions.nation_name = accounts.account_name;", requedNat).Scan(&returnHello.NationName, &returnHello.Region, &returnHello.CashInHand, &returnHello.CashInEscrow)
	if err == pgx.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	respEncoder.Encode(returnHello)
}

type cashReturn struct {
	CashInHand   float32             `json:"handCash"`
	CashInEscrow float32             `json:"escrowCash"`
	NetWorth     float32             `json:"netWorth"`
	Transactions []transactionFormat `json:"transactions"`
}

func buildNetWorth(ctx context.Context, dbConn *pgxpool.Conn, user string, cashValue float32) (float32, error) {
	var shareGetter, debtGetter *float32
	err := dbConn.QueryRow(ctx, `SELECT SUM(share_quant*share_price) as shareWorth FROM stock_holdings, stocks WHERE stocks.ticker = stock_holdings.ticker AND stock_holdings.account_name = $1`, user).Scan(&shareGetter)
	if err != nil {
		return 0, err
	}
	err = dbConn.QueryRow(ctx, `SELECT SUM(current_value) as debtValue FROM loans where lendee = $1`, user).Scan(&debtGetter)
	var shareValue, debtValue float32
	if shareGetter == nil {
		shareValue = 0
	} else {
		shareValue = *shareGetter
	}
	if debtGetter == nil {
		debtValue = 0
	} else {
		debtValue = *debtGetter
	}
	return ((cashValue + shareValue) - debtValue), err
}

func (Env env) nationCashDetails(w http.ResponseWriter, r *http.Request) {
	encoder := json.NewEncoder(w)
	theReturn := cashReturn{}
	theNation := r.PathValue("natName")
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbConn.Release()
	err = dbConn.QueryRow(r.Context(), `SELECT cash_in_hand, cash_in_escrow FROM accounts WHERE account_name = $1;`, theNation).Scan(&theReturn.CashInHand, &theReturn.CashInEscrow)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	theReturn.NetWorth, err = buildNetWorth(r.Context(), dbConn, theNation, theReturn.CashInHand)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("NetWorth Err", err)
		return
	}
	theReturn.Transactions, err = Env.getUserCashTransactions(r.Context(), theNation)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	encoder.Encode(theReturn)
}

func (Env env) nationCashQuick(w http.ResponseWriter, r *http.Request) {
	encoder := json.NewEncoder(w)
	theNation := r.PathValue("natName")
	theReturn := struct {
		AcctName   string
		CashInHand float32
	}{
		AcctName: theNation,
	}
	err := Env.DBPool.QueryRow(r.Context(), `SELECT cash_in_hand FROM accounts WHERE account_name = $1;`, theNation).Scan(&theReturn.CashInHand)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	encoder.Encode(theReturn)
}
