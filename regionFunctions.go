package main

import (
	"encoding/json"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/api/iterator"
)

func regionInfo(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient firestore.Client) {
	returnObject := struct {
		HandValue     float32
		EscrowValue   float32
		CashTransacts []transactionFormat
		Loans         []loanFormat
	}{}
	encoder := json.NewEncoder(w)
	regionToRet := r.PathValue("region")
	requingNation := r.Header.Get("NationName")
	theConn, err := dbPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("DB Err", err)
		return
	}
	defer theConn.Release()
	err = theConn.QueryRow(r.Context(), `SELECT cash_in_hand, cash_in_escrow FROM accounts, nation_permissions WHERE account_name = $1 AND account_type = 'region' AND (nation_permissions.region_name = accounts.account_name AND nation_permissions.nation_name = $2);`, regionToRet, requingNation).Scan(&returnObject.HandValue, &returnObject.EscrowValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Query1 Err", err)
		return
	}
	documents := fsClient.Collection(CASH_TRANSACT_COLL).WhereEntity(firestore.OrFilter{
		Filters: []firestore.EntityFilter{
			firestore.PropertyFilter{
				Path:     "sender",
				Operator: "==",
				Value:    regionToRet,
			},
			firestore.PropertyFilter{
				Path:     "receiver",
				Operator: "==",
				Value:    regionToRet,
			},
		},
	}).OrderBy("timestamp", firestore.Desc).Limit(25).Documents(r.Context())
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
		returnObject.CashTransacts = append(returnObject.CashTransacts, thisTransact)
	}
	regLoans, err := getAccountLoans(r.Context(), theConn, regionToRet)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	returnObject.Loans = regLoans
	encoder.Encode(returnObject)
}
