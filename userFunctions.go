package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var HASH_COST, _ = strconv.Atoi(os.Getenv("HASH_COST"))
var EXTRA_KEY_STRING = os.Getenv("EXTRA_KEY_STRING")

func signupFunc(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient *firestore.Client) {
	decoder := json.NewDecoder(r.Body)
	var newUser struct {
		NationName     string `json:"NationName"`
		PasswordString string `json:"PasswordString"`
		RegionName     string `json:"RegionName"`
	}
	ourTx, err := dbPool.Begin(r.Context())
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
	createdHash, _ := bcrypt.GenerateFromPassword([]byte(newUser.PasswordString), HASH_COST)
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
	_, err = loanIssue(r.Context(), &loanFormat{LoanRate: 2.5, Lender: newUser.RegionName, Lendee: newUser.NationName, LentValue: 10000}, ourTx, fsClient)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Loan Err", err)
		return
	}
	log.Println("User Created")
	ourTx.Commit(r.Context())
	w.WriteHeader(http.StatusCreated)
}

func userVerification(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool) {
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
	err = dbPool.QueryRow(r.Context(), "SELECT account_pass_hash, region_name, permission FROM accounts, nation_permissions WHERE account_name = $1 AND account_name = nation_name AND account_type = 'nation';", user.NationName).Scan(&dbPassHash, &userReturn.UserRegion, &userReturn.UserPermission)
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
	authKeyHex := md5.Sum([]byte(user.NationName + EXTRA_KEY_STRING))
	userReturn.AuthKey = hex.EncodeToString(authKeyHex[:])
	if err != nil {
		log.Println("JSON err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
	outEncoder.Encode(userReturn)
}

func registerRegion(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool) {
	log.Println("Region Signup Request")
	decoder := json.NewDecoder(r.Body)
	var newRegion struct {
		RegionName   string
		RegionTicker string
	}
	var err error
	err = decoder.Decode(&newRegion)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("JSON Err", err)
		return
	}
	ourConn, err := dbPool.Begin(r.Context())
	defer ourConn.Rollback(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err 1", err)
		return
	}
	ret1, _ := ourConn.Query(r.Context(), "INSERT INTO accounts (account_name, account_type, cash_in_hand) VALUES ($1, $2)", newRegion.RegionName, "region", 1000000)
	errString := ret1.Scan().Error()
	if errString != "" && errString != pgx.ErrNoRows.Error() {
		w.WriteHeader(http.StatusInternalServerError)
		log.Print("DB Err 2", ret1.Err())
		return
	}
	err = ourConn.Commit(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("TX Error", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func nationInfo(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool) {
	respEncoder := json.NewEncoder(w)
	returnHello := struct {
		NationName   string
		Region       string
		CashInHand   float32
		CashInEscrow float32
	}{}
	requedNat := r.PathValue("natName")
	log.Println("Nation info requested for", requedNat)
	err := dbPool.QueryRow(r.Context(), "SELECT account_name, nation_permissions.region_name, cash_in_hand, cash_in_escrow FROM accounts, nation_permissions WHERE nation_name = $1 AND nation_permissions.nation_name = accounts.account_name;", requedNat).Scan(&returnHello.NationName, &returnHello.Region, &returnHello.CashInHand, &returnHello.CashInEscrow)
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

func nationCashDetails(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient *firestore.Client) {
	log.Println("Requested cash details")
	encoder := json.NewEncoder(w)
	theReturn := cashReturn{}
	theNation := r.PathValue("natName")
	err := dbPool.QueryRow(r.Context(), `SELECT cash_in_hand, cash_in_escrow FROM accounts WHERE account_name = $1;`, theNation).Scan(&theReturn.CashInHand, &theReturn.CashInEscrow)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	theReturn.Transactions, err = getUserCashTransactions(r.Context(), *fsClient, theNation)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	encoder.Encode(theReturn)
}
