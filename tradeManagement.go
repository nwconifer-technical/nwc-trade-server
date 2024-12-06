package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type tradeFormat struct {
	TradeId   string
	Ticker    string
	Sender    string
	Direction string
	Quantity  int
	PriceType string
	Price     float32
}

func (Env env) openTrade(w http.ResponseWriter, r *http.Request) {
	log.Println("Trade Entry Occurring")
	decoder := json.NewDecoder(r.Body)
	var sentThing tradeFormat
	var currentQuote Quote
	dbTx, err := Env.DBPool.Begin(r.Context())
	if err != nil {
		log.Println("Tx Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbTx.Rollback(r.Context())
	err = decoder.Decode(&sentThing)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("JSON Err", err)
		return
	}
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
	jsonEncoder := json.NewEncoder(w)
	err = dbTx.QueryRow(r.Context(), `SELECT share_price, total_share_volume FROM stocks WHERE ticker = $1`, sentThing.Ticker).Scan(&currentQuote.MarketPrice, &currentQuote.TotalVolume)
	if err != nil {
		if err == pgx.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("Quote Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var opposingDirect string
	if strings.EqualFold(sentThing.Direction, "buy") {
		opposingDirect = "sell"
	} else if strings.EqualFold(sentThing.Direction, "sell") {
		opposingDirect = "buy"
	} else {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var enteredTradeId int = 0
	var openOrders pgx.Rows
	if strings.EqualFold(sentThing.PriceType, "market") {
		sentThing.Price = currentQuote.MarketPrice
		if opposingDirect == "sell" {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = $2 AND (order_price <= $3 OR price_type = 'market');`, sentThing.Ticker, opposingDirect, currentQuote.MarketPrice)
		} else {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = $2 AND (order_price >= $3 OR price_type = 'market');`, sentThing.Ticker, opposingDirect, currentQuote.MarketPrice)
		}
	} else {
		if opposingDirect == "sell" {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = $2 AND order_price <= $3;`, sentThing.Ticker, opposingDirect, sentThing.Price)
		} else {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = $2 AND order_price >= $3;`, sentThing.Ticker, opposingDirect, sentThing.Price)
		}
	}
	if err != nil && err != pgx.ErrNoRows {
		log.Println("Order DB Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var oppOrders []tradeFormat
	for {
		newRow := openOrders.Next()
		if !newRow {
			if openOrders.Err() != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			break
		}
		var oppTrade tradeFormat
		err = openOrders.Scan(&oppTrade.TradeId, &oppTrade.Sender, &oppTrade.Quantity)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		oppOrders = append(oppOrders, oppTrade)
	}
	for i := 0; i < len(oppOrders); i++ {
		oppTrade := oppOrders[i]
		var cashValue float32
		var transferAmount int
		if sentThing.Quantity > oppTrade.Quantity {
			transferAmount = sentThing.Quantity - oppTrade.Quantity
			sentThing.Quantity -= oppTrade.Quantity
			oppTrade.Quantity = 0
		} else if sentThing.Quantity == oppTrade.Quantity {
			transferAmount = sentThing.Quantity
			oppTrade.Quantity = 0
			sentThing.Quantity = 0
		} else {
			transferAmount = oppTrade.Quantity - sentThing.Quantity
			sentThing.Quantity = 0
			oppTrade.Quantity -= sentThing.Quantity
		}
		cashValue = sentThing.Price * float32(transferAmount)
		var cashTransfer transactionFormat
		var shareTrans shareTransfer
		if strings.EqualFold(sentThing.Direction, "buy") {
			cashTransfer = transactionFormat{
				Sender:   sentThing.Sender,
				Receiver: oppTrade.Sender,
				Value:    cashValue,
				Message:  sentThing.Ticker + ` Trade`,
			}
			shareTrans = shareTransfer{
				Ticker:   sentThing.Ticker,
				Sender:   oppTrade.Sender,
				Receiver: sentThing.Sender,
				Quantity: transferAmount,
				AvgPrice: sentThing.Price,
			}
		} else {
			cashTransfer = transactionFormat{
				Receiver: sentThing.Sender,
				Sender:   oppTrade.Sender,
				Value:    cashValue,
				Message:  sentThing.Ticker + ` Trade`,
			}
			shareTrans = shareTransfer{
				Ticker:   sentThing.Ticker,
				Receiver: oppTrade.Sender,
				Sender:   sentThing.Sender,
				Quantity: transferAmount,
				AvgPrice: sentThing.Price,
			}
		}
		err = Env.handCashTransaction(&cashTransfer, r.Context(), dbTx)
		if err != nil {
			log.Println("Cash Err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = transferShares(r.Context(), dbTx, shareTrans)
		if err != nil {
			log.Println("Shares Err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		log.Println(oppTrade.TradeId, oppTrade.Quantity)
		if oppTrade.Quantity == 0 {
			err = dbTx.QueryRow(r.Context(), `DELETE FROM open_orders WHERE trade_id = $1`, oppTrade.TradeId).Scan()
		} else {
			err = dbTx.QueryRow(r.Context(), `UPDATE open_orders SET quant = $1 WHERE trade_id = $2`, oppTrade.Quantity, oppTrade.TradeId).Scan()
		}
		if err != nil && err != pgx.ErrNoRows {
			log.Println(`Opposite Update Err`, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if sentThing.Quantity > 0 {
		err = dbTx.QueryRow(r.Context(), `INSERT INTO open_orders (ticker, trader, quant, order_direction, price_type, order_price) VALUES ($1, $2, $3, $4, $5, $6) RETURNING trade_id`, sentThing.Ticker, sentThing.Sender, sentThing.Quantity, sentThing.Direction, sentThing.PriceType, sentThing.Price).Scan(&enteredTradeId)
		if err != nil {
			log.Println("FinalDB Err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	err = dbTx.Commit(r.Context())
	if err != nil {
		log.Println("Commit Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if enteredTradeId != 0 {
		jsonEncoder.Encode(struct {
			TradeId int
		}{
			TradeId: enteredTradeId,
		})
	}
	w.WriteHeader(http.StatusOK)
}

// func tradePriceUpdate(dbTx pgx.Tx, currentQuote Quote, price float32, quantity float32, direction string) error {

// }
