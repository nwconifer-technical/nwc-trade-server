package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type loanFormat struct {
	LoanId       string  `json:"id,omitempty"`
	Lender       string  `json:"lender"`                 // The person issuing the loan
	Lendee       string  `json:"lendee"`                 // The person receiving the loan
	LentValue    float32 `json:"lentValue"`              // The value lent out
	LoanRate     float32 `json:"loanRate"`               // The loan interest rate
	CurrentValue float32 `json:"currentValue,omitempty"` // The current value of the loan, basically LentValue + interest - repayments
}

func (Env env) manualLoanIssue(w http.ResponseWriter, r *http.Request) {
	log.Println("Manual Loan Issuance")
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)
	var theLoan loanFormat
	err := decoder.Decode(&theLoan)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
	theLoan.CurrentValue = theLoan.LentValue
	dbTx, err := Env.DBPool.Begin(r.Context())
	defer dbTx.Rollback(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	theLoan.LoanId, err = Env.loanIssue(r.Context(), &theLoan, dbTx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Loan Err", err)
		return
	}
	err = dbTx.Commit(r.Context())
	if err != nil {
		log.Println("Tx Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	encoder.Encode(struct {
		LoanId string `json:"loanId"`
	}{
		LoanId: theLoan.LoanId,
	})
}

func (Env env) loanIssue(ctx context.Context, theLoan *loanFormat, dbTx pgx.Tx) (string, error) {
	log.Println("Loan Issuance")
	var theId string
	err := dbTx.QueryRow(ctx, `INSERT INTO loans (lendee, lender, lent_value, rate, current_value) VALUES ($1, $2, $3, $4, $5) RETURNING loan_id;`, theLoan.Lendee, theLoan.Lender, theLoan.LentValue, theLoan.LoanRate, theLoan.LentValue).Scan(&theId)
	if err != nil {
		return "", err
	}
	cashMessage := `Loan Issue - ID ` + theId
	err = Env.handCashTransaction(&transactionFormat{
		Sender:   theLoan.Lender,
		Receiver: theLoan.Lendee,
		Value:    theLoan.LentValue,
		Message:  cashMessage,
	}, ctx, dbTx)
	return theId, err
}

func (Env env) getLoan(w http.ResponseWriter, r *http.Request) {
	encoder := json.NewEncoder(w)
	loanId := r.PathValue("loanId")
	var theLoan loanFormat
	theLoan.LoanId = loanId
	reqNat := r.Header.Get("NationName")
	err := Env.DBPool.QueryRow(r.Context(), `SELECT lendee, lender, lent_value, rate, current_value FROM loans WHERE loan_id = $2`, loanId, loanId).Scan(&theLoan.Lendee, &theLoan.Lender, &theLoan.LentValue, &theLoan.LoanRate, &theLoan.CurrentValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if theLoan.Lendee != reqNat && theLoan.Lender != reqNat {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	encoder.Encode(theLoan)
}

func (Env env) getLoans(w http.ResponseWriter, r *http.Request) {
	log.Println("Loans Get")
	requedNat := r.Header.Get("NationName")
	encoder := json.NewEncoder(w)
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbConn.Release()
	theLoans, err := getAccountLoans(r.Context(), dbConn, requedNat)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	encoder.Encode(struct {
		YourLoans []loanFormat `json:"yourLoans"`
	}{
		YourLoans: theLoans,
	})
}

func getAccountLoans(ctx context.Context, dbConn *pgxpool.Conn, accountName string) ([]loanFormat, error) {
	retRows, err := dbConn.Query(ctx, `SELECT loan_id, lendee, lender, lent_value, rate, current_value FROM loans WHERE lendee = $1 OR lender = $1`, accountName)
	if err != nil {
		return nil, err
	}
	var returnArray []loanFormat
	for {
		if !retRows.Next() {
			break
		}
		var thisLoan loanFormat
		rowError := retRows.Scan(&thisLoan.LoanId, &thisLoan.Lendee, &thisLoan.Lender, &thisLoan.LentValue, &thisLoan.LoanRate, &thisLoan.CurrentValue)
		if rowError != nil {
			return nil, rowError
		}
		returnArray = append(returnArray, thisLoan)
	}
	return returnArray, nil
}
