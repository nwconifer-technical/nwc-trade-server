package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/go-co-op/gocron/v2"
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

	cronSched, err := gocron.NewScheduler()
	if err != nil {
		log.Fatal(err)
	}
	cronSched.NewJob(
		gocron.CronJob(`*/30 * * * *`, false),
		gocron.NewTask(primaryEnv.logPrices, primCtx),
	)
	cronSched.NewJob(
		gocron.CronJob(`5 00 * * *`, false),
		gocron.NewTask(env.updateLoanValues, primCtx),
	)
	theMux := http.NewServeMux()
	theMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello!"))
	})
	theMux.HandleFunc("get /ping", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Pinged")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Pong"))
	})
	theMux.HandleFunc("POST /signup/nation", primaryEnv.signupFunc)
	theMux.HandleFunc("POST /signup/region", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.registerRegion)
	})
	theMux.HandleFunc("POST /verify/nation", primaryEnv.userVerification)
	theMux.HandleFunc("POST /cash/transaction", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.outerCashHandler)
	})
	theMux.HandleFunc("GET /cash/details/{natName}", primaryEnv.nationCashDetails)
	theMux.HandleFunc("GET /loans", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.getLoans)
	})
	theMux.HandleFunc("GET /loan/{loanId}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.getLoan)
	})
	theMux.HandleFunc("POST /loan/issue", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.manualLoanIssue)
	})
	theMux.HandleFunc("GET /nation/{natName}", primaryEnv.nationInfo)
	theMux.HandleFunc("GET /region/{region}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.regionInfo)
	})
	theMux.HandleFunc("GET /list/nations", func(w http.ResponseWriter, r *http.Request) {
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
	theMux.HandleFunc("GET /shares/quote/{ticker}", primaryEnv.marketQuote)
	theMux.HandleFunc("POST /shares/transfer", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.manualShareTransfer)
	})
	theMux.HandleFunc("POST /shares/trade", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.openTrade)
	})
	theMux.HandleFunc("GET /shares/trade/{id}", func(w http.ResponseWriter, r *http.Request) {
		tradeId := r.PathValue("id")
		var returnTrade tradeFormat
		anEncoder := json.NewEncoder(w)
		err := primaryEnv.DBPool.QueryRow(r.Context(), `SELECT ticker, trader, quant, order_direction, price_type, order_price FROM open_orders WHERE trade_id = $1`, tradeId).Scan(&returnTrade.Ticker, &returnTrade.Sender, &returnTrade.Quantity, &returnTrade.Direction, &returnTrade.PriceType, &returnTrade.Price)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("Trade Find err", err)
		}
		anEncoder.Encode(returnTrade)
	})
	theMux.HandleFunc("DELETE /shares/trade/{id}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, func(w http.ResponseWriter, r *http.Request) {
			tradeId := r.PathValue("id")
			err := primaryEnv.DBPool.QueryRow(r.Context(), `DELETE FROM open_orders WHERE trade_id = $1`, tradeId).Scan()
			if err != nil && err != pgx.ErrNoRows {
				w.WriteHeader(http.StatusInternalServerError)
				log.Println("Delete Trade Err", err)
			}
			w.WriteHeader(http.StatusOK)
		})

	})
	theMux.HandleFunc("GET /shares/quote", primaryEnv.getAllStocks)
	theMux.HandleFunc("GET /shares/book/{ticker}", primaryEnv.returnAssetBook)
	theMux.HandleFunc("GET /shares/portfolio", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.accountPortfolio)
	})
	theMux.HandleFunc("GET /shares/portfolio/{region}", func(w http.ResponseWriter, r *http.Request) {
		primaryEnv.securedWrapper(w, r, primaryEnv.accountPortfolio)
	})

	theServer := http.Server{
		Addr:        `:8080`,
		Handler:     theMux,
		ReadTimeout: 5 * time.Second,
	}
	cronSched.Start()
	log.Println("NWC Trade Server Started")
	err = theServer.ListenAndServe()
	defer theServer.Shutdown(primCtx)
	log.Panicln("The server broke", err)
}
