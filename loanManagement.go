package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

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
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbTx.Rollback(r.Context())
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
	err := Env.DBPool.QueryRow(r.Context(), `SELECT lendee, lender, lent_value, rate, current_value FROM loans WHERE loan_id = $1`, loanId).Scan(&theLoan.Lendee, &theLoan.Lender, &theLoan.LentValue, &theLoan.LoanRate, &theLoan.CurrentValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Get loan Err", err)
		return
	}
	var possibleRegions []string
	var sameThing bool
	bothAccts, err := Env.DBPool.Query(r.Context(), `SELECT account_name, account_type FROM accounts WHERE account_name = $1 OR account_name = $2`, theLoan.Lendee, theLoan.Lender)
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer bothAccts.Close()
	for bothAccts.Next() {
		if bothAccts.Err() != nil {
			log.Println("getLoan perms err", err)
			continue
		}
		var thisAccountName, thisAccountType string
		err = bothAccts.Scan(&thisAccountName, &thisAccountType)
		if err != nil {
			log.Println("Some err", err)
			continue
		}
		if thisAccountName == reqNat {
			sameThing = true
			break
		}
		if thisAccountType == "region" {
			possibleRegions = append(possibleRegions, thisAccountName)
		}
	}
	var livePerms bool = false
	if !sameThing {
		if len(possibleRegions) > 0 {
			for _, region := range possibleRegions {
				var perm string
				err := Env.DBPool.QueryRow(r.Context(), `SELECT permission FROM nation_permissions WHERE nation_name = $1 AND region_name = $2`, reqNat, region).Scan(&perm)
				if err != nil {
					if err == pgx.ErrNoRows {
						continue
					}
					w.WriteHeader(http.StatusInternalServerError)
					log.Println("getLoan perm err", err)
					return
				}
				if perm != "citizen" {
					livePerms = true
					break
				}
			}
		} else {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	if (theLoan.Lendee != reqNat && theLoan.Lender != reqNat) && !livePerms {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	loanTransacts, err := Env.DBPool.Query(r.Context(), `SELECT timecode, sender, receiver ,transaction_value, transaction_message FROM cash_transactions WHERE transaction_message LIKE $1 ORDER BY timecode DESC`, ("%ID " + loanId))
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer loanTransacts.Close()
	var theTransacts []transactionFormat
	for loanTransacts.Next() {
		curTransact := transactionFormat{}
		err := loanTransacts.Scan(&curTransact.Timecode, &curTransact.Sender, &curTransact.Receiver, &curTransact.Value, &curTransact.Message)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		theTransacts = append(theTransacts, curTransact)
	}
	encoder.Encode(struct {
		TheLoan       loanFormat
		LoanTransacts []transactionFormat
	}{
		TheLoan:       theLoan,
		LoanTransacts: theTransacts,
	})
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
	defer retRows.Close()
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

func (Env env) payLoan(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var theLoan loanFormat
	sentData := struct {
		LoanId      string
		RepayAmount float32
	}{}
	err := decoder.Decode(&sentData)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("repay decode err", err)
		return
	}
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("payLoan DB Err", err)
		return
	}
	defer dbConn.Release()
	err = dbConn.QueryRow(r.Context(), `SELECT lendee, lender, current_value FROM loans WHERE loan_id = $1`, sentData.LoanId).Scan(&theLoan.Lendee, &theLoan.Lender, &theLoan.CurrentValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("payLoan getloan Err", err)
		return
	}
	var accType string
	err = dbConn.QueryRow(r.Context(), `SELECT account_type FROM accounts WHERE account_name = $1`, theLoan.Lendee).Scan(&accType)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("payLoan perm err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if strings.EqualFold(accType, "region") {
		var perm string
		err = dbConn.QueryRow(r.Context(), `SELECT permission FROM nation_permissions WHERE nation_name = $1 AND region_name = $2;`, r.Header.Get("NationName"), theLoan.Lendee).Scan(&perm)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			log.Println("payLoan perm err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if perm == "citizen" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	} else {
		if theLoan.Lendee != r.Header.Get("NationName") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	dbTx, err := dbConn.Begin(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("payLoan tx err", err)
		return
	}
	defer dbTx.Rollback(r.Context())
	if sentData.RepayAmount >= theLoan.CurrentValue {
		sentData.RepayAmount = theLoan.CurrentValue
		err = dbTx.QueryRow(r.Context(), `DELETE FROM loans WHERE loan_id = $1`, sentData.LoanId).Scan()
		if err != nil && err != pgx.ErrNoRows {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("payLoan DB Err", err)
			return
		}
	} else {
		err = dbTx.QueryRow(r.Context(), `UPDATE loans SET current_value = current_value - $1 WHERE loan_id = $2`, sentData.RepayAmount, sentData.LoanId).Scan()
		if err != nil && err != pgx.ErrNoRows {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("payLoan DB Err", err)
			return
		}
	}
	cashMessage := `Loan Repayment - ID ` + sentData.LoanId
	err = Env.handCashTransaction(&transactionFormat{Sender: theLoan.Lendee, Receiver: theLoan.Lender, Value: sentData.RepayAmount, Message: cashMessage}, r.Context(), dbTx)
	if err != nil {
		log.Println("loanRepay cashTransact err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	err = dbTx.Commit(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("payLoan commit err", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (Env env) writeOffLoan(w http.ResponseWriter, r *http.Request) {
	loanId := r.PathValue("loanId")
	var lender string
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("writeOff DB Err", err)
		return
	}
	err = dbConn.QueryRow(r.Context(), `SELECT lender FROM loans WHERE loan_id = $1`, loanId).Scan(&lender)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("writeOff Query Err", err)
		return
	}
	log.Println(lender)
	var accType string
	err = dbConn.QueryRow(r.Context(), `SELECT account_type FROM accounts WHERE account_name = $1`, lender).Scan(&accType)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("writeOff Query Err", err)
		return
	}
	if accType == "region" {
		var natPerm string
		err = dbConn.QueryRow(r.Context(), `SELECT permission FROM nation_permissions WHERE nation_name = $1 AND region_name = $2`, r.Header.Get("NationName"), lender).Scan(&natPerm)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("writeOff Query Err", err)
			return
		}
		log.Println(natPerm)
		if natPerm == "citizen" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	} else {
		if lender != r.Header.Get("NationName") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	err = dbConn.QueryRow(r.Context(), `DELETE FROM loans WHERE loan_id = $1`, loanId).Scan()
	if err != nil && err != pgx.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("writeOff Del Err", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (Env env) updateLoanValues(ctx context.Context) error {
	dbConn, err := Env.DBPool.Acquire(ctx)
	if err != nil {
		log.Println("Loan Update job err", err)
		return err
	}
	defer dbConn.Release()
	theLoans, err := dbConn.Query(ctx, `SELECT loan_id, rate, current_value FROM loans`)
	if err != nil {
		log.Println("Loan update job err", err)
		return err
	}
	loanBatch := pgx.Batch{}
	for theLoans.Next() {
		var loanId int
		var loanRate, curVal float32
		err := theLoans.Scan(&loanId, &loanRate, &curVal)
		if err != nil {
			log.Println("Loan update err", err)
			return err
		}
		loanBatch.Queue(`UPDATE loans SET current_value = current_value * (1+($1/100)) WHERE loan_id = $2`, loanRate, loanRate)
	}
	err = dbConn.SendBatch(ctx, &loanBatch).Close()
	if err != nil && err != pgx.ErrNoRows {
		log.Println("Loan update job err", err)
		return err
	}
	return nil
}
