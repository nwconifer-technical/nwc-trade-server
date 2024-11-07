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
	"google.golang.org/api/iterator"
)

var HASH_COST, _ = strconv.Atoi(os.Getenv("HASH_COST"))
var EXTRA_KEY_STRING = os.Getenv("EXTRA_KEY_STRING")

func signupFunc(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool) {
	decoder := json.NewDecoder(r.Body)
	var newUser struct {
		NationName     string `json:"NationName"`
		PasswordString string `json:"PasswordString"`
		RegionName     string `json:"RegionName"`
	}
	ourConn, err := dbPool.Acquire(r.Context())
	defer ourConn.Release()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err 0", err)
	}
	err = decoder.Decode(&newUser)
	log.Println("NewUser Signup Request", newUser)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err 1", err)
		return
	}
	log.Println("Bcrypt started")
	createdHash, _ := bcrypt.GenerateFromPassword([]byte(newUser.PasswordString), HASH_COST)
	err = ourConn.QueryRow(r.Context(), "INSERT INTO accounts (account_name, account_pass_hash) VALUES ($1, $2)", newUser.NationName, string(createdHash)).Scan()
	if err.Error() != "" && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusConflict)
		log.Println("DB Err 3", err)
		return
	}
	err = ourConn.QueryRow(r.Context(), "INSERT INTO nation_permissions (region_name, nation_name, permission) VALUES ($1, $2, 'citizen')", newUser.RegionName, newUser.NationName).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err 4", err)
		return
	}
	log.Println("User Created")
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
	var dbUserRegion string = ""
	err = dbPool.QueryRow(r.Context(), "SELECT account_pass_hash, region_name FROM accounts, nation_permissions WHERE account_name = $1 AND account_name = nation_name", user.NationName).Scan(&dbPassHash, &dbUserRegion)
	if err != nil {
		errStr := err.Error()
		if errStr == pgx.ErrNoRows.Error() {
			w.WriteHeader(http.StatusNotFound)
			return
		} else if errStr != "" {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("DB Err", errStr)
			return
		}
	}
	err = bcrypt.CompareHashAndPassword([]byte(dbPassHash), []byte(user.PasswordString))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	authKey := md5.Sum([]byte(user.NationName + EXTRA_KEY_STRING))
	returnValue := struct {
		AuthKey    string `json:"AuthKey"`
		UserRegion string `json:"UserRegion"`
		UserName   string `json:"UserName"`
	}{
		UserName:   user.NationName,
		AuthKey:    hex.EncodeToString(authKey[:]),
		UserRegion: dbUserRegion,
	}
	if err != nil {
		log.Println("JSON err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
	outEncoder.Encode(returnValue)
	w.WriteHeader(http.StatusAccepted)
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
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err 1", err)
		return
	}
	ret1, _ := ourConn.Query(r.Context(), "INSERT INTO region_accounts (region_name, region_ticker) VALUES ($1, $2)", newRegion.RegionName, newRegion.RegionTicker)
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
	err := dbPool.QueryRow(r.Context(), "SELECT account_name, nation_permissions.region_name, cash_in_hand, cash_in_escrow FROM accounts, nation_permissions WHERE nation_name = $1 AND nation_permissions.nation_name = nation_permissions.region_name;", requedNat).Scan(&returnHello.NationName, &returnHello.Region, &returnHello.CashInHand, &returnHello.CashInEscrow)
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
	theFilter := firestore.OrFilter{
		Filters: []firestore.EntityFilter{
			firestore.PropertyFilter{
				Path:     "sender",
				Operator: "==",
				Value:    theNation,
			},
			firestore.PropertyFilter{
				Path:     "receiver",
				Operator: "==",
				Value:    theNation,
			},
		},
	}
	documents := fsClient.Collection(CASH_TRANSACT_COLL).WhereEntity(theFilter).OrderBy("timestamp", firestore.Desc).Limit(25).Documents(r.Context())
	for {
		docu, err := documents.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			log.Println("FS Err", err)
			return
		}
		var thisTransact transactionFormat
		err = docu.DataTo(&thisTransact)
		if err != nil {
			log.Println("Docu Err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		theReturn.Transactions = append(theReturn.Transactions, thisTransact)
	}
	encoder.Encode(theReturn)

}
