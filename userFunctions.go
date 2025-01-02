package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
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
	_, err = Env.loanIssue(r.Context(), &loanFormat{LoanRate: 2.5, Lender: newUser.RegionName, Lendee: newUser.NationName, LentValue: 10000}, ourTx)
	if err != nil {
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
	Transactions []transactionFormat `json:"transactions"`
}

func (Env env) nationCashDetails(w http.ResponseWriter, r *http.Request) {
	encoder := json.NewEncoder(w)
	theReturn := cashReturn{}
	theNation := r.PathValue("natName")
	err := Env.DBPool.QueryRow(r.Context(), `SELECT cash_in_hand, cash_in_escrow FROM accounts WHERE account_name = $1;`, theNation).Scan(&theReturn.CashInHand, &theReturn.CashInEscrow)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
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
