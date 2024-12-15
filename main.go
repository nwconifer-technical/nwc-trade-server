package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgres://postgres:hellofrend@104.198.238.223:5432/nwc_am_db?pool_min_conns=1&pool_max_conns=10

var HashCost,
	DbString,
	ProjId,
	ExtraKeyString,
	FirestoreString string

type env struct {
	DBPool         *pgxpool.Pool
	FSClient       *firestore.Client
	CashCollection string
	HashCost       int
	KeyString      string
}

func main() {
	primCtx := context.Background()
	var primaryEnv env = env{
		CashCollection: "cashTransactions",
		KeyString:      ExtraKeyString,
	}
	var err error
	primaryEnv.HashCost, _ = strconv.Atoi(HashCost)
	primaryEnv.DBPool, err = pgxpool.New(primCtx, DbString)
	if err != nil {
		log.Fatal(err)
	}
	testConn, err := primaryEnv.DBPool.Acquire(primCtx)
	if err != nil {
		log.Fatal(err)
	}
	err = testConn.Ping(primCtx)
	if err != nil {
		log.Fatal(err)
	}
	testConn.Release()
	defer primaryEnv.DBPool.Close()
	primaryEnv.FSClient, err = firestore.NewClientWithDatabase(primCtx, ProjId, FirestoreString)
	if err != nil {
		log.Panic(err)
	}
	defer primaryEnv.FSClient.Close()
	// var primaryEnv = env{
	// 	CashCollection: os.Getenv("CASH_TRANSACT_COLL"),
	// 	HashCost:       hashCost,
	// 	KeyString:      os.Getenv("EXTRA_KEY_STRING"),
	// 	DBPool:         dbPool,
	// 	FSClient:       fsClient,
	// }

	theMux := http.NewServeMux()
	theMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello!"))
	})
	theMux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Pong"))
	})
	theMux.HandleFunc("/signup/nation", func(w http.ResponseWriter, r *http.Request) {
		openPostWrapper(w, r, primaryEnv.signupFunc)
	})
	theMux.HandleFunc("/signup/region", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedPostWrapper(w, r, primaryEnv.registerRegion)
	})
	theMux.HandleFunc("/verify/nation", func(w http.ResponseWriter, r *http.Request) {
		openPostWrapper(w, r, primaryEnv.userVerification)
	})
	theMux.HandleFunc("/cash/transaction", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedPostWrapper(w, r, primaryEnv.outerCashHandler)
	})
	theMux.HandleFunc("/cash/details/{natName}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.nationCashDetails(w, r)
	})
	theMux.HandleFunc("/loans", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedGetWrapper(w, r, primaryEnv.getLoans)
	})
	theMux.HandleFunc("/loan/{loanId}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedGetWrapper(w, r, primaryEnv.getLoan)
	})
	theMux.HandleFunc("/loan/issue", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedPostWrapper(w, r, primaryEnv.manualLoanIssue)
	})
	theMux.HandleFunc("/nation/{natName}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.nationInfo(w, r)
	})
	theMux.HandleFunc("/region/{region}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedGetWrapper(w, r, primaryEnv.regionInfo)
	})
	theMux.HandleFunc("/list/nations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		headEncoder := json.NewEncoder(w)
		dbRows, err := primaryEnv.DBPool.Query(r.Context(), `SELECT account_name FROM accounts WHERE account_type = 'nation' ORDER BY cash_in_hand DESC LIMIT 25;`)
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
		primaryEnv.marketQuote(w, r)
	})
	theMux.HandleFunc("/shares/transfer", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedPostWrapper(w, r, primaryEnv.manualShareTransfer)
	})
	theMux.HandleFunc("/shares/trade", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedPostWrapper(w, r, primaryEnv.openTrade)
	})
	theMux.HandleFunc("/shares/book/{ticker}", primaryEnv.returnAssetBook)
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
