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
	"google.golang.org/api/iterator"
)

type transactionFormat struct {
	Timecode time.Time `firestore:"timestamp" json:"timecode,omitempty"`
	Sender   string    `firestore:"sender" json:"sender"`
	Receiver string    `firestore:"receiver" json:"receiver"`
	Value    float32   `firestore:"value" json:"value"`
	Message  string    `firestoe:"message,omitempty" json:"message"`
}

func outerCashHandler(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool, fsClient *firestore.Client) {
	log.Println("Cash Transaction Occurring")
	decoder := json.NewDecoder(r.Body)
	var sentThing *transactionFormat
	err := decoder.Decode(&sentThing)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
	dbTx, err := dbPool.Begin(r.Context())
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
	if err = handCashTransaction(sentThing, r.Context(), dbTx, fsClient); err != nil {
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

func handCashTransaction(transaction *transactionFormat, ctx context.Context, dbTx pgx.Tx, fsClient *firestore.Client) error {
	transaction.Timecode = time.Now()
	var err error
	err = dbTx.QueryRow(ctx, `UPDATE accounts SET cash_in_hand = cash_in_hand - $1 WHERE account_name = $2`, transaction.Value, transaction.Sender).Scan()
	if err != pgx.ErrNoRows {
		return err
	}
	err = dbTx.QueryRow(ctx, `UPDATE accounts SET cash_in_hand = cash_in_hand + $1 WHERE account_name = $2`, transaction.Value, transaction.Receiver).Scan()
	if err != pgx.ErrNoRows {
		return err
	}
	_, _, err = fsClient.Collection(CASH_TRANSACT_COLL).Add(ctx, transaction)
	return err
}

func getUserCashTransactions(ctx context.Context, fsClient firestore.Client, user string) ([]transactionFormat, error) {
	var cashTransacts []transactionFormat
	documents := fsClient.Collection(CASH_TRANSACT_COLL).WhereEntity(firestore.OrFilter{
		Filters: []firestore.EntityFilter{
			firestore.PropertyFilter{
				Path:     "sender",
				Operator: "==",
				Value:    user,
			},
			firestore.PropertyFilter{
				Path:     "receiver",
				Operator: "==",
				Value:    user,
			},
		},
	}).OrderBy("timestamp", firestore.Desc).Limit(25).Documents(ctx)
	for {
		docu, err := documents.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			log.Println("FS Err", err)
			return nil, err
		}
		var thisTransact transactionFormat
		err = docu.DataTo(&thisTransact)
		if err != nil {
			log.Println("Docu Err", err)
			return nil, err
		}
		cashTransacts = append(cashTransacts, thisTransact)
	}
	return cashTransacts, nil
}
