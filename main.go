package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var CASH_TRANSACT_COLL = os.Getenv("CASH_TRANSACT_COLL")

// postgres://postgres:hellofrend@104.198.238.223:5432/nwc_am_db?pool_min_conns=1&pool_max_conns=10

func main() {
	primCtx := context.Background()
	dbPool, err := pgxpool.New(primCtx, os.Getenv("DB_CONNECTSTRING"))
	if err != nil {
		log.Fatal(err)
	}
	testConn, err := dbPool.Acquire(primCtx)
	if err != nil {
		log.Fatal(err)
	}
	err = testConn.Ping(primCtx)
	if err != nil {
		log.Fatal(err)
	}
	testConn.Release()
	defer dbPool.Close()
	fsClient, err := firestore.NewClientWithDatabase(primCtx, os.Getenv("PROJECT_ID"), os.Getenv("FIRESTORE_DB"))
	if err != nil {
		log.Panic(err)
	}
	defer fsClient.Close()

	theMux := http.NewServeMux()
	theMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello!"))
	})
	theMux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Pong"))
	})
	theMux.HandleFunc("/signup/nation", func(w http.ResponseWriter, r *http.Request) {
		openPostLTWrapper(w, r, dbPool, fsClient, signupFunc)
	})
	theMux.HandleFunc("/signup/region", func(w http.ResponseWriter, r *http.Request) {
		securedPostWrapper(w, r, dbPool, registerRegion)
	})
	theMux.HandleFunc("/verify/nation", func(w http.ResponseWriter, r *http.Request) {
		openPostWrapper(w, r, dbPool, userVerification)
	})
	theMux.HandleFunc("/cash/transaction", func(w http.ResponseWriter, r *http.Request) {
		securedPostLTWrapper(w, r, dbPool, fsClient, outerCashHandler)
	})
	theMux.HandleFunc("/cash/details/{natName}", func(w http.ResponseWriter, r *http.Request) {
		openGetLTWrapper(w, r, dbPool, fsClient, nationCashDetails)
	})
	theMux.HandleFunc("/loans", func(w http.ResponseWriter, r *http.Request) {
		securedGetWrapper(w, r, dbPool, getLoans)
	})
	theMux.HandleFunc("/loan/{loanId}", func(w http.ResponseWriter, r *http.Request) {
		securedGetWrapper(w, r, dbPool, getLoan)
	})
	theMux.HandleFunc("/loan/issue", func(w http.ResponseWriter, r *http.Request) {
		securedPostLTWrapper(w, r, dbPool, fsClient, manualLoanIssue)
	})
	theMux.HandleFunc("/nation/{natName}", func(w http.ResponseWriter, r *http.Request) {
		nationInfo(w, r, dbPool)
	})
	theMux.HandleFunc("/region/{region}", func(w http.ResponseWriter, r *http.Request) {
		securedGetLTWrapper(w, r, dbPool, *fsClient, regionInfo)
	})
	theMux.HandleFunc("/list/nations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		headEncoder := json.NewEncoder(w)
		dbRows, err := dbPool.Query(r.Context(), `SELECT account_name FROM accounts WHERE account_type = 'nation' ORDER BY cash_in_hand DESC LIMIT 25;`)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		objToRet := struct {
			Nations []string
		}{}
		for {
			newRow := dbRows.Next()
			if !newRow {
				break
			}
			var currNat string
			err = dbRows.Scan(&currNat)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			objToRet.Nations = append(objToRet.Nations, currNat)
		}
		headEncoder.Encode(objToRet)
	})
	theMux.HandleFunc("/shares/quote/{ticker}", func(w http.ResponseWriter, r *http.Request) {
		marketQuote(w, r, dbPool)
	})
	theServer := http.Server{
		Addr:        `:8080`,
		Handler:     theMux,
		ReadTimeout: 5 * time.Second,
	}
	log.Println("NWC Trade Server Started")
	err = theServer.ListenAndServe()
	defer theServer.Shutdown(primCtx)
	log.Panicln("The server broke", err)
}
