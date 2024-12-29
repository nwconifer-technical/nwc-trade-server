package main

import (
	"context"
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

func (theTrade tradeFormat) copy() tradeFormat {
	var newTrade tradeFormat
	newTrade.TradeId = theTrade.TradeId
	newTrade.Direction = theTrade.Direction
	newTrade.Ticker = theTrade.Ticker
	newTrade.Price = theTrade.Price
	newTrade.PriceType = theTrade.PriceType
	newTrade.Sender = theTrade.Sender
	newTrade.Quantity = theTrade.Quantity
	return newTrade
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
	var traderCash float32
	err = dbTx.QueryRow(r.Context(), `SELECT account_type, cash_in_hand FROM accounts WHERE account_name = $1`, sentThing.Sender).Scan(&account_type, &traderCash)
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
	err = dbTx.QueryRow(r.Context(), `SELECT share_price, total_share_volume, market_cap FROM stocks WHERE ticker = $1`, sentThing.Ticker).Scan(&currentQuote.MarketPrice, &currentQuote.TotalVolume, &currentQuote.MarketCapitalisation)
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
	}
	if strings.EqualFold(sentThing.Direction, "buy") {
		if strings.EqualFold(sentThing.PriceType, "market") {
			if (sentThing.Price*1.5)*float32(sentThing.Quantity) > float32(traderCash) {
				w.WriteHeader(http.StatusUnauthorized)
				log.Println("Risk unauthed")
				return
			}
		} else {
			if (sentThing.Price)*float32(sentThing.Quantity) > float32(traderCash) {
				w.WriteHeader(http.StatusUnauthorized)
				log.Println("Risk unauthed")
				return
			}
		}
	}

	err = tradePriceUpdate(r.Context(), dbTx, currentQuote, sentThing)
	if err != nil {
		log.Println("Update DB Err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if strings.EqualFold(sentThing.PriceType, "market") {
		if opposingDirect == "sell" {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = 'sell' AND (order_price <= $2 OR price_type = 'market');`, sentThing.Ticker, currentQuote.MarketPrice)
		} else {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = 'buy' AND (order_price >= $2 OR price_type = 'market');`, sentThing.Ticker, currentQuote.MarketPrice)
		}
	} else {
		if opposingDirect == "sell" {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = 'sell' AND order_price <= $2;`, sentThing.Ticker, sentThing.Price)
		} else {
			openOrders, err = dbTx.Query(r.Context(), `SELECT trade_id, trader, quant FROM open_orders WHERE ticker = $1 AND order_direction = 'buy' AND order_price >= $2;`, sentThing.Ticker, sentThing.Price)
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
	var updSentThing tradeFormat
	matchTrades := 0
	for i := 0; i < len(oppOrders); i++ {
		oppTrade := oppOrders[i]
		matchTrades += 1
		// ignore-not-used
		var transferAmount int
		var updOppTrade tradeFormat
		transferAmount, updOppTrade, updSentThing = updateTradeObjs(oppTrade, sentThing)
		cashValue := sentThing.Price * float32(transferAmount)
		log.Println(oppTrade)
		var cashTransfer transactionFormat
		var shareTrans shareTransfer
		if strings.EqualFold(sentThing.Direction, "buy") {
			cashTransfer = transactionFormat{
				Sender:   sentThing.Sender,
				Receiver: updOppTrade.Sender,
				Value:    cashValue,
				Message:  sentThing.Ticker + ` Trade`,
			}
			shareTrans = shareTransfer{
				Ticker:   sentThing.Ticker,
				Sender:   updOppTrade.Sender,
				Receiver: sentThing.Sender,
				Quantity: transferAmount,
				AvgPrice: sentThing.Price,
			}
		} else {
			cashTransfer = transactionFormat{
				Receiver: sentThing.Sender,
				Sender:   updOppTrade.Sender,
				Value:    cashValue,
				Message:  sentThing.Ticker + ` Trade`,
			}
			shareTrans = shareTransfer{
				Ticker:   sentThing.Ticker,
				Receiver: updOppTrade.Sender,
				Sender:   sentThing.Sender,
				Quantity: transferAmount,
				AvgPrice: sentThing.Price,
			}
		}
		err = Env.handCashTransaction(&cashTransfer, r.Context(), dbTx)
		if err != nil {
			log.Println("Cash Err", err)
			continue
		}
		err = transferShares(r.Context(), dbTx, shareTrans)
		if err != nil {
			log.Println("Shares Err", err)
			continue
		}
		if updOppTrade.Quantity == 0 {
			err = dbTx.QueryRow(r.Context(), `DELETE FROM open_orders WHERE trade_id = $1`, oppTrade.TradeId).Scan()
		} else {
			err = dbTx.QueryRow(r.Context(), `UPDATE open_orders SET quant = $1 WHERE trade_id = $2`, updOppTrade.Quantity, oppTrade.TradeId).Scan()
		}
		if err != nil && err != pgx.ErrNoRows {
			log.Println(`Opposite Update Err`, err)
			continue
		}
	}
	log.Println(updSentThing.Quantity)
	if updSentThing.Ticker == "" {
		updSentThing = sentThing.copy()
	}
	log.Println(updSentThing.Quantity)
	if updSentThing.Quantity > 0 {
		err = dbTx.QueryRow(r.Context(), `INSERT INTO open_orders (ticker, trader, quant, order_direction, price_type, order_price) VALUES ($1, $2, $3, $4, $5, $6) RETURNING trade_id`, sentThing.Ticker, sentThing.Sender, sentThing.Quantity, strings.ToLower(sentThing.Direction), sentThing.PriceType, sentThing.Price).Scan(&enteredTradeId)
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
		w.WriteHeader(http.StatusCreated)
		jsonEncoder.Encode(struct {
			TradeId int
		}{
			TradeId: enteredTradeId,
		})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func tradePriceUpdate(ctx context.Context, dbTx pgx.Tx, currentQuote Quote, theTrade tradeFormat) error {
	movementPercent := float32(theTrade.Quantity) / float32(currentQuote.TotalVolume)
	var priceDiffPercent float32 = 0.0
	var newMarketCap float32 = 0.0
	if strings.EqualFold(theTrade.Direction, "buy") {
		priceDiffPercent = (theTrade.Price / currentQuote.MarketPrice)
		newMarketCap = currentQuote.MarketCapitalisation * (1 + (movementPercent * priceDiffPercent))
	} else if strings.EqualFold(theTrade.Direction, "sell") {
		priceDiffPercent = (currentQuote.MarketPrice / theTrade.Price)
		newMarketCap = currentQuote.MarketCapitalisation * (1 - (movementPercent * priceDiffPercent))
	}
	log.Println(newMarketCap)
	err := dbTx.QueryRow(ctx, `UPDATE stocks SET market_cap = $1, share_price = $2 WHERE ticker = $3`, newMarketCap, (newMarketCap / float32(currentQuote.TotalVolume)), theTrade.Ticker).Scan()
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	return nil
}

func updateTradeObjs(oppTrade tradeFormat, sentThing tradeFormat) (int, tradeFormat, tradeFormat) {
	var transferAmount int
	newOppTrade := oppTrade.copy()
	newSentThing := sentThing.copy()
	if sentThing.Quantity > oppTrade.Quantity {
		transferAmount = newSentThing.Quantity - oppTrade.Quantity
		newSentThing.Quantity -= oppTrade.Quantity
		newOppTrade.Quantity = 0
	} else if sentThing.Quantity == oppTrade.Quantity {
		transferAmount = newSentThing.Quantity
		newOppTrade.Quantity = 0
		newSentThing.Quantity = 0
	} else if sentThing.Quantity < oppTrade.Quantity {
		transferAmount = newSentThing.Quantity
		newSentThing.Quantity = 0
		newOppTrade.Quantity -= sentThing.Quantity
	}
	return transferAmount, newOppTrade, newSentThing
}
