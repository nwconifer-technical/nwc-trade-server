package main

import (
	"encoding/json"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	err = ourConn.QueryRow(r.Context(), "INSERT INTO accounts (account_name, account_type, cash_in_hand) VALUES ($1, $2, $3);", newRegion.RegionName, "region", 1000000).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Print("DB Err 2", err)
		return
	}
	regionMarketCap, err := buildMarketCap(newRegion.RegionName)
	if err != nil {
		log.Println("Market Cap Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	err = ourConn.QueryRow(r.Context(), `INSERT INTO stocks (ticker, region, market_cap, total_share_volume, share_price) VALUES ($1, $2, $3, 0, 0);`, newRegion.RegionTicker, newRegion.RegionName, regionMarketCap).Scan()
	if err != nil && err != pgx.ErrNoRows {
		log.Println("DB Err 3", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	err = createShares(r.Context(), ourConn, newRegion.RegionName, 1000000)
	if err != nil && err != pgx.ErrNoRows {
		log.Println("Creation err", err)
		w.WriteHeader(http.StatusInternalServerError)
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
