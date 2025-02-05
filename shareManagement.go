package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (Env env) logPrices(ctx context.Context) error {
	theTime := time.Now()
	newConn, err := Env.DBPool.Acquire(ctx)
	if err != nil {
		log.Println("Share Price Logging Err", err)
		return err
	}
	defer newConn.Release()
	allStocks, err := newConn.Query(ctx, `SELECT ticker, share_price FROM stocks`)
	if err != nil {
		log.Println("Share Price Logging Err", err)
		return err
	}
	defer allStocks.Close()
	bigBatch := pgx.Batch{}
	for allStocks.Next() {
		var ticker string
		var price float64
		err := allStocks.Scan(&ticker, &price)
		if err != nil {
			log.Println("Share Price Logging ", ticker, "Err", err)
			return err
		}
		bigBatch.Queue(`INSERT INTO stock_prices (timecode, ticker, log_market_price) VALUES ($1,$2,$3)`, theTime.Format(`2006-01-02 15:04:05 MST`), ticker, price)
		bigBatch.Queue(`UPDATE open_orders SET order_price = $1 WHERE ticker = $2 AND price_type = 'market'`, price, ticker)
	}
	if allStocks.Err() != nil {
		log.Println("Share Price Logging Err", err)
		return err
	}
	err = newConn.SendBatch(ctx, &bigBatch).Close()
	if err != nil {
		log.Println("Share Price Logging Err", err)
		return err
	}

	return nil
}

func createShares(ctx context.Context, dbTx pgx.Tx, region string, numberofShares int) error {
	var ticker string
	var market_cap float32
	var existingVolume int
	err := dbTx.QueryRow(ctx, `SELECT ticker, market_cap, total_share_volume FROM stocks WHERE region = $1`, region).Scan(&ticker, &market_cap, &existingVolume)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	newVol := float32(existingVolume + numberofShares)
	err = dbTx.QueryRow(ctx, `INSERT INTO stock_holdings (ticker, account_name, share_quant, avg_price) VALUES ($1, $2, $3, 0) ON CONFLICT (ticker, account_name) DO UPDATE SET share_quant = stock_holdings.share_quant + EXCLUDED.share_quant`, ticker, region, numberofShares).Scan()
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	err = dbTx.QueryRow(ctx, `UPDATE stocks SET total_share_volume = total_share_volume + $1, share_price = $2 WHERE ticker = $3`, numberofShares, market_cap/newVol, ticker).Scan()
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	err = dbTx.QueryRow(ctx, `UPDATE open_orders SET open_price = $1 WHERE ticker = $2 AND price_type = 'market';`, market_cap/newVol, ticker).Scan()
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	return nil
}

type createSend struct {
	Region   string `json:"region"`
	Quantity int    `json:"quantity"`
}

func (Env env) manualCreateShares(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	acct := r.Header.Get("NationName")
	var sendingData createSend
	err := decoder.Decode(&sendingData)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	dbConn, err := Env.DBPool.Begin(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("TX Create Err", err)
		return
	}
	defer dbConn.Rollback(r.Context())
	var natPerm string
	err = dbConn.QueryRow(r.Context(), `SELECT permission FROM accounts WHERE nation_name = $1 AND region_name = $2`, acct, sendingData.Region).Scan(&natPerm)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("AccType Query Err", err)
		return
	}
	if natPerm == "citizen" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	err = createShares(r.Context(), dbConn, sendingData.Region, sendingData.Quantity)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	err = dbConn.Commit(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	return
}

type Quote struct {
	Ticker               string  `json:"ticker"`
	Region               string  `json:"region"`
	MarketPrice          float32 `json:"marketPrice"`
	MarketCapitalisation float32 `json:"marketCap"`
	TotalVolume          int     `json:"totalVolume"`
}

func (Env env) marketQuote(w http.ResponseWriter, r *http.Request) {
	sendingQuote := Quote{
		Ticker: r.PathValue("ticker"),
	}
	theEncoder := json.NewEncoder(w)
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbConn.Release()
	err = dbConn.QueryRow(r.Context(), `SELECT region, share_price, total_share_volume, market_cap FROM stocks WHERE ticker = $1`, sendingQuote.Ticker).Scan(&sendingQuote.Region, &sendingQuote.MarketPrice, &sendingQuote.TotalVolume, &sendingQuote.MarketCapitalisation)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	theEncoder.Encode(sendingQuote)
}

type shareTransfer struct {
	Ticker   string  `json:"ticker"`
	Sender   string  `json:"sender"`
	Receiver string  `json:"receiver"`
	Quantity int     `json:"quantity"`
	AvgPrice float32 `json:"avgprice"`
}

func (Env env) manualShareTransfer(w http.ResponseWriter, r *http.Request) {
	log.Println("Share Transfer Occurring")
	decoder := json.NewDecoder(r.Body)
	var sentThing shareTransfer
	err := decoder.Decode(&sentThing)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
	dbTx, err := Env.DBPool.Begin(r.Context())
	if err != nil {
		log.Println("Tx Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbTx.Rollback(r.Context())
	var account_type string
	err = dbTx.QueryRow(r.Context(), `SELECT account_type FROM accounts WHERE account_name = $1`, sentThing.Sender).Scan(&account_type)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("Auth Err 1", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if account_type == "region" {
		var permission string
		err = dbTx.QueryRow(r.Context(), `SELECT permission FROM nation_permissions WHERE region_name = $1 AND nation_name = $2`, sentThing.Sender, r.Header.Get("NationName")).Scan(&permission)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			log.Println("Auth Err 2", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if permission != "trader" && permission != "admin" && r.Header.Get("NationName") != "Gallaton" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	} else if account_type == "nation" {
		if sentThing.Sender != r.Header.Get("NationName") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	if err = transferShares(r.Context(), dbTx, sentThing); err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("DB Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	err = dbTx.Commit(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func transferShares(ctx context.Context, dbTx pgx.Tx, transfer shareTransfer) error {
	var currentSenderQuantity int
	err := dbTx.QueryRow(ctx, `SELECT share_quant FROM stock_holdings WHERE ticker = $1 AND account_name = $2`, transfer.Ticker, transfer.Sender).Scan(&currentSenderQuantity)
	log.Println(currentSenderQuantity)
	if err != nil {
		log.Println("Weird Issue", err)
		return err
	}
	if currentSenderQuantity-transfer.Quantity < 0 {
		log.Println("Unprocessable")
		return pgx.ErrNoRows
	}
	err = dbTx.QueryRow(ctx,
		`INSERT INTO stock_holdings (ticker, account_name, share_quant, avg_price) VALUES ($1, $2, $3, $4) ON CONFLICT (ticker, account_name) DO UPDATE SET share_quant = stock_holdings.share_quant + EXCLUDED.share_quant, avg_price = stock_holdings.avg_price + ((EXCLUDED.avg_price - stock_holdings.avg_price) * (stock_holdings.share_quant / EXCLUDED.share_quant));`,
		transfer.Ticker, transfer.Receiver, transfer.Quantity, transfer.AvgPrice).Scan()
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	err = dbTx.QueryRow(ctx, `UPDATE stock_holdings SET share_quant = stock_holdings.share_quant - $3 WHERE ticker = $1 AND account_name = $2;`, transfer.Ticker, transfer.Sender, transfer.Quantity).Scan()
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	return nil
}

type bookReturn struct {
	CurrentQuote Quote
	BookDepth    int
	Buys         []tradeFormat
	Sells        []tradeFormat
}

func (Env env) returnAssetBook(w http.ResponseWriter, r *http.Request) {
	log.Println("Book Get Request")
	var theBook = bookReturn{}
	theEncoder := json.NewEncoder(w)
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbConn.Release()
	err = dbConn.QueryRow(r.Context(), `SELECT region, share_price, total_share_volume, market_cap FROM stocks WHERE ticker = $1`, r.PathValue("ticker")).Scan(&theBook.CurrentQuote.Region, &theBook.CurrentQuote.MarketPrice, &theBook.CurrentQuote.TotalVolume, &theBook.CurrentQuote.MarketCapitalisation)
	theBook.CurrentQuote.Ticker = r.PathValue("ticker")
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("Get Quote Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	theTrades, err := dbConn.Query(r.Context(), `SELECT trade_id, trader, quant, order_direction, price_type, order_price FROM open_orders WHERE ticker = $1 ORDER BY order_price ASC`, r.PathValue("ticker"))
	if err == pgx.ErrNoRows {
		theBook.BookDepth = 0
		theEncoder.Encode(theBook)
		return
	} else if err != nil {
		log.Println("DB Trades Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer theTrades.Close()
	var currentMaxPrice float32 = 0
	for {
		newTrade := theTrades.Next()
		if !newTrade {
			theTrades.Close()
			break
		}
		thisTrade := tradeFormat{
			Ticker: r.PathValue("ticker"),
		}
		err = theTrades.Scan(&thisTrade.TradeId, &thisTrade.Sender, &thisTrade.Quantity, &thisTrade.Direction, &thisTrade.PriceType, &thisTrade.Price)
		if err != nil {
			log.Println("Proccing Trades Err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if thisTrade.Price > currentMaxPrice {
			theBook.BookDepth += 1
			currentMaxPrice = thisTrade.Price
		}
		if thisTrade.Direction == "buy" {
			theBook.Buys = append(theBook.Buys, thisTrade)
		} else if thisTrade.Direction == "sell" {
			theBook.Sells = append(theBook.Sells, thisTrade)
		}
	}
	err = theEncoder.Encode(theBook)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

type holdingFormat struct {
	Ticker        string
	ShareQuantity int
	AvgPrice      float32
}

type portfolioFormat struct {
	Account    string
	Holdings   []holdingFormat
	OpenOrders []tradeFormat
}

func getAcctOpenOrders(ctx context.Context, dbConn *pgxpool.Conn, acct string) ([]tradeFormat, error) {
	var trades []tradeFormat
	tradeReader, err := dbConn.Query(ctx, `SELECT trade_id, ticker, quant, order_direction, price_type, order_price FROM open_orders WHERE trader = $1 ORDER BY ticker;`, acct)
	if err != nil {
		return nil, err
	}
	defer tradeReader.Close()
	for tradeReader.Next() {
		var currentTrade tradeFormat
		err := tradeReader.Scan(&currentTrade.TradeId, &currentTrade.Ticker, &currentTrade.Quantity, &currentTrade.Direction, &currentTrade.PriceType, &currentTrade.Price)
		if err != nil {
			return nil, err
		}
		currentTrade.Sender = acct
		trades = append(trades, currentTrade)
	}
	return trades, tradeReader.Err()
}

func getHoldings(ctx context.Context, dbConn *pgxpool.Conn, acct string) ([]holdingFormat, error) {
	var holdings []holdingFormat
	holdingsReader, err := dbConn.Query(ctx, `SELECT ticker, share_quant, avg_price FROM stock_holdings WHERE account_name = $1`, acct)
	if err != nil {
		return nil, err
	}
	defer holdingsReader.Close()
	for holdingsReader.Next() {
		var currentHolding holdingFormat
		err := holdingsReader.Scan(&currentHolding.Ticker, &currentHolding.ShareQuantity, &currentHolding.AvgPrice)
		if err != nil {
			return nil, err
		}
		holdings = append(holdings, currentHolding)
	}
	return holdings, holdingsReader.Err()
}

func (Env env) accountPortfolio(w http.ResponseWriter, r *http.Request) {
	acctName := r.PathValue("region")
	theEncoder := json.NewEncoder(w)
	if acctName == "" {
		acctName = r.Header.Get("NationName")
	}
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Conn Acq Err", err)
		return
	}
	defer dbConn.Release()
	if acctName != r.Header.Get("NationName") {
		var accType string
		err := dbConn.QueryRow(r.Context(), `SELECT account_type FROM accounts WHERE account_name = $1`, acctName).Scan(&accType)
		if err != nil {
			if err == pgx.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("AccType Query Err", err)
			return
		}
		if accType == "region" {
			var accPerms string
			err := dbConn.QueryRow(r.Context(), `SELECT permission FROM nation_permissions WHERE region_name = $1 AND nation_name = $2;`, acctName, r.Header.Get("NationName")).Scan(&accPerms)
			if err != nil {
				if err == pgx.ErrNoRows {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				log.Println("Perms Check Query Err", err)
				return
			}
		}
	}
	theHoldings, err := getHoldings(r.Context(), dbConn, acctName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("getHoldings Err", err)
		return
	}
	acctOpens, err := getAcctOpenOrders(r.Context(), dbConn, acctName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("getAcctOpenOrders Err", err)
		return
	}
	returnObj := portfolioFormat{
		Account:    acctName,
		Holdings:   theHoldings,
		OpenOrders: acctOpens,
	}
	err = theEncoder.Encode(returnObj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Encode Err", err)
		return
	}
}

func (Env env) getAllStocks(w http.ResponseWriter, r *http.Request) {
	anEncoder := json.NewEncoder(w)
	w.Header().Set("Content-Type", "application/json")
	var allEquities []Quote
	allStocks, err := Env.DBPool.Query(r.Context(), `SELECT ticker, region, market_cap, total_share_volume, share_price FROM stocks ORDER BY market_cap DESC`)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Query Err", err)
		return
	}
	defer allStocks.Close()
	for allStocks.Next() {
		var thisQuote Quote
		err := allStocks.Scan(&thisQuote.Ticker, &thisQuote.Region, &thisQuote.MarketCapitalisation, &thisQuote.TotalVolume, &thisQuote.MarketPrice)
		if err != nil {
			log.Println("Scanning Err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		allEquities = append(allEquities, thisQuote)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Scan2 Err", err)
		return
	}
	toSend := struct {
		AllStocks []Quote `json:"allStocks"`
	}{
		AllStocks: allEquities,
	}
	w.WriteHeader(http.StatusOK)
	err = anEncoder.Encode(toSend)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("Encoder Error", err)
	}
}

type TimeQuote struct {
	Timecode time.Time
	LogPrice float32
}

func (Env env) getRecentPriceHistory(w http.ResponseWriter, r *http.Request) {
	var thePrices []TimeQuote
	ticker := r.PathValue(`ticker`)
	curTime := time.Now().AddDate(0, 0, -7)
	encod := json.NewEncoder(w)
	dbConn, err := Env.DBPool.Acquire(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("recentPrices err", err)
		return
	}
	defer dbConn.Release()
	thePastTimes, err := dbConn.Query(r.Context(), `SELECT timecode, log_market_price FROM stock_prices WHERE ticker = $1 AND timecode >= $2 ORDER BY timecode ASC;`, ticker, curTime)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("recentPrices err", err)
		return
	}
	for thePastTimes.Next() {
		var thisPrice TimeQuote
		if thePastTimes.Err() != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("recentPrices reader err", thePastTimes.Err())
			return
		}
		err := thePastTimes.Scan(&thisPrice.Timecode, &thisPrice.LogPrice)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println("recentPrices reader err", thePastTimes.Err())
			return
		}
		thePrices = append(thePrices, thisPrice)
	}
	if len(thePrices) < 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	encod.Encode(struct {
		RecentPrice []TimeQuote
	}{
		RecentPrice: thePrices,
	})
}
