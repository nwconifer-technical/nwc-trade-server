package main

import (
	"encoding/json"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func regionInfo(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient firestore.Client) {
	returnObject := struct {
		RegionName    string
		HandValue     float32
		EscrowValue   float32
		CashTransacts []transactionFormat
		Loans         []loanFormat
	}{}
	encoder := json.NewEncoder(w)
	requingNation := r.Header.Get("NationName")
	regionToRet := r.PathValue("region")
	theConn, err := dbPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err", err)
		return
	}
	defer theConn.Release()
	err = theConn.QueryRow(r.Context(), `SELECT account_name, cash_in_hand, cash_in_escrow FROM accounts, nation_permissions WHERE account_type = 'region' AND (nation_permissions.region_name = accounts.account_name AND nation_permissions.nation_name = $1);`, requingNation).Scan(&returnObject.RegionName, &returnObject.HandValue, &returnObject.EscrowValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Query1 Err", err)
		return
	}
	returnObject.CashTransacts, err = getUserCashTransactions(r.Context(), fsClient, regionToRet)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	regLoans, err := getAccountLoans(r.Context(), theConn, regionToRet)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	returnObject.Loans = regLoans
	encoder.Encode(returnObject)
}
