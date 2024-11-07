package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transactionFormat struct {
	Timecode time.Time `firestore:"timestamp" json:"timecode,omitempty"`
	Sender   string    `firestore:"sender" json:"sender"`
	Receiver string    `firestore:"receiver" json:"receiver"`
	Value    float64   `firestore:"value" json:"value"`
	Message  string    `firestoe:"message,omitempty" json:"message"`
}

func outerCashHandler(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient *firestore.Client) {
	log.Println("Cash Transaction Occurring")
	decoder := json.NewDecoder(r.Body)
	var sentThing transactionFormat
	err := decoder.Decode(&sentThing)
	sentThing.Timecode = time.Now()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
	dbTx, err := dbPool.Begin(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err = handCashTransaction(&sentThing, r.Context(), dbTx, fsClient); err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Transact Err", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handCashTransaction(transaction *transactionFormat, ctx context.Context, dbTx pgx.Tx, fsClient *firestore.Client) error {
	var err error
	defer dbTx.Rollback(ctx)
	err = dbTx.QueryRow(ctx, `UPDATE accounts SET cash_in_hand = cash_in_hand - $1 WHERE account_name = $2`, transaction.Value, transaction.Sender).Scan()
	if err != pgx.ErrNoRows {
		return err
	}
	err = dbTx.QueryRow(ctx, `UPDATE accounts SET cash_in_hand = cash_in_hand + $1 WHERE account_name = $2`, transaction.Value, transaction.Receiver).Scan()
	if err != pgx.ErrNoRows {
		return err
	}
	err = dbTx.Commit(ctx)
	if err != nil {
		return err
	}
	_, _, err = fsClient.Collection(CASH_TRANSACT_COLL).Add(ctx, transaction)
	return err
}
