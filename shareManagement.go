package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"net/http"

	"github.com/gosimple/slug"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	err = dbTx.QueryRow(ctx, `UPDATE stocks SET total_share_volume = total_share_volume + $1, share_price = $2`, numberofShares, market_cap/newVol).Scan()
	return err
}

func buildMarketCap(region string) (float32, error) {
	// Initial Market Cap Mix
	// Most nations - 255 - NWC 90.00, TNP 6227.00
	// Economic Output - 76 - NWC 670783000000000, TNP 486764000000000
	// Average Income - 74 - NWC 126798, TNP 168437
	// WA Endorsements - 66 - NWC 4.22, TNP 26.13
	// Pro-Market - 48 - NWC 3.90, TNP 24.88
	// Mean take of all and multiplied by the "TNP Market Cap"
	// The API Call
	// https://www.nationstates.net/cgi-bin/api.cgi?region={REGION_SLUGIFIED}&q=census&scale=255+76+74+66+48
	type Scale struct {
		Id    int     `xml:"id,attr"`
		Score float32 `xml:"SCORE"`
	}

	type Census struct {
		Region string  `xml:"id,attr"`
		Scores []Scale `xml:"CENSUS>SCALE"`
	}
	const TNPMarkCap = 500000000
	var TNPVals map[int]float32 = map[int]float32{
		255: 6227,
		76:  486764000000000,
		74:  168437,
		66:  26.13,
		48:  24.88,
	}
	client := &http.Client{}
	regionSlugified := slug.Substitute(region, map[string]string{
		" ": "_",
	})
	reqString := `https://www.nationstates.net/cgi-bin/api.cgi?region=` + regionSlugified + `&q=census&scale=255+76+74+66+48`
	log.Println(reqString)
	req, err := http.NewRequest(http.MethodGet, reqString, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "NWConifer Finance Application, by Gallaton")
	resp, err := client.Do(req)
	if resp.StatusCode != 200 {
		log.Println(resp.StatusCode)
		log.Println("Request Err", err)
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var output Census
	err = xml.Unmarshal(body, &output)
	if err != nil {
		log.Println("Unmarshal Err", err)
		return 0, err
	}

	var runingTotal float32
	for i := 0; i < len(output.Scores); i++ {
		currentOne := output.Scores[i]
		runingTotal += currentOne.Score / TNPVals[currentOne.Id]
	}
	return (runingTotal / 5) * TNPMarkCap, nil
}

type Quote struct {
	Ticker      string  `json:"ticker"`
	MarketPrice float32 `json:"marketPrice"`
}

func marketQuote(w http.ResponseWriter, r *http.Request, dbPool *pgxpool.Pool) {
	sendingQuote := Quote{
		Ticker: r.PathValue("ticker"),
	}
	theEncoder := json.NewEncoder(w)
	err := dbPool.QueryRow(r.Context(), `SELECT share_price FROM stocks WHERE ticker = $1`, sendingQuote.Ticker).Scan(&sendingQuote.MarketPrice)
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
