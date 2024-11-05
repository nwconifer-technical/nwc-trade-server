package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"cloud.google.com/go/firestore"
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
	theMux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Pinged")
		w.WriteHeader(http.StatusOK)
	})
	theMux.HandleFunc("/signup/nation", func(w http.ResponseWriter, r *http.Request) {
		openPostWrapper(w, r, dbPool, signupFunc)
	})
	theMux.HandleFunc("/signup/region", func(w http.ResponseWriter, r *http.Request) {
		openPostWrapper(w, r, dbPool, registerRegion)
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
	theMux.HandleFunc("/nation/{natName}", func(w http.ResponseWriter, r *http.Request) {
		nationInfo(w, r, dbPool)
	})

	theServer := http.Server{
		Addr:    `:http`,
		Handler: theMux,
		// WriteTimeout: 5 * time.Second,
	}
	log.Println("NWC Trade Server Started")
	err = theServer.ListenAndServe()
	defer theServer.Shutdown(primCtx)
	log.Panicln("The server broke", err)
}
