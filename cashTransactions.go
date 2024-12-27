package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type transactionFormat struct {
	Timecode time.Time `firestore:"timestamp" json:"timecode,omitempty"`
	Sender   string    `firestore:"sender" json:"sender"`
	Receiver string    `firestore:"receiver" json:"receiver"`
	Value    float32   `firestore:"value" json:"value"`
	Message  string    `firestoe:"message,omitempty" json:"message"`
}

func (Env env) outerCashHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Cash Transaction Occurring")
	decoder := json.NewDecoder(r.Body)
	var sentThing *transactionFormat
	err := decoder.Decode(&sentThing)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
	dbTx, err := Env.DBPool.Begin(r.Context())
	defer dbTx.Rollback(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var accountType string
	err = dbTx.QueryRow(r.Context(), `SELECT account_type FROM accounts WHERE account_name = $1`, sentThing.Sender).Scan(&accountType)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("DB 0 Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if accountType == "nation" && sentThing.Sender != r.Header.Get("NationName") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if accountType == "region" {
		var permLevel string
		err = dbTx.QueryRow(r.Context(), `SELECT permission FROM nation_permissions WHERE region_name = $1 AND nation_name = $2`, sentThing.Sender, r.Header.Get("NationName")).Scan(&permLevel)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("DB Error", err)
			return
		}
		if permLevel != "trader" && permLevel != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	if err = Env.handCashTransaction(sentThing, r.Context(), dbTx); err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Transact Err", err)
		return
	}
	err = dbTx.Commit(r.Context())
	if err != nil {
		log.Println("Commit Error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (Env env) handCashTransaction(transaction *transactionFormat, ctx context.Context, dbTx pgx.Tx) error {
	transaction.Timecode = time.Now()
	var err error
	err = dbTx.QueryRow(ctx, `UPDATE accounts SET cash_in_hand = cash_in_hand - $1 WHERE account_name = $2`, transaction.Value, transaction.Sender).Scan()
	if err != pgx.ErrNoRows && err != nil {
		return err
	}
	err = dbTx.QueryRow(ctx, `UPDATE accounts SET cash_in_hand = cash_in_hand + $1 WHERE account_name = $2`, transaction.Value, transaction.Receiver).Scan()
	if err != pgx.ErrNoRows && err != nil {
		return err
	}
	err = dbTx.QueryRow(ctx, `INSERT INTO cash_transactions (timecode, sender, receiver, transaction_value, transaction_message) VALUES ($1,$2,$3,$4,$5)`, transaction.Timecode.Format(`2006-01-02 15:04:05 MST`), transaction.Sender, transaction.Receiver, transaction.Value, transaction.Message).Scan()
	if err != pgx.ErrNoRows && err != nil {
		return err
	}
	return nil
}

func (Env env) getUserCashTransactions(ctx context.Context, user string) ([]transactionFormat, error) {
	var cashTransacts []transactionFormat
	dbConn, err := Env.DBPool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer dbConn.Release()
	cashRows, err := dbConn.Query(ctx, `SELECT timecode, sender, receiver, transaction_value, transaction_message FROM cash_transactions WHERE sender = $1 OR receiver = $1 ORDER BY timecode DESC LIMIT 25;`, user)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	for cashRows.Next() {
		curTransact := transactionFormat{}
		err := cashRows.Scan(&curTransact.Timecode, &curTransact.Sender, &curTransact.Receiver, &curTransact.Value, &curTransact.Message)
		if err != nil {
			return nil, err
		}
		cashTransacts = append(cashTransacts, curTransact)
	}
	return cashTransacts, nil
}
