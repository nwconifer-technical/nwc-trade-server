package main

import (
	"context"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gosimple/slug"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Scale struct {
	Id    int     `xml:"id,attr"`
	Score float32 `xml:"SCORE"`
}

type Census struct {
	Region string  `xml:"id,attr"`
	Scores []Scale `xml:"CENSUS>SCALE"`
}

func (Env env) runRealign(ctx context.Context) {
	conn, err := Env.DBPool.Acquire(ctx)
	if err != nil {
		return
	}
	err = realignPricesWithNS(conn, ctx)
	log.Println(err)
}

func realignPricesWithNS(dbConn *pgxpool.Conn, ctx context.Context) error {
	allStocks, err := dbConn.Query(ctx, `SELECT region, ticker, market_cap, share_stat1, share_stat2, share_stat3, share_stat4, share_stat5 FROM stocks`)
	if err != nil {
		return err
	}
	allShareUpdates := pgx.Batch{}
	client := &http.Client{}
	for allStocks.Next() {
		time.Sleep(time.Second * 1)
		var region, ticker string
		var share_stat1, share_stat2, share_stat3, share_stat4, share_stat5, curMarketCap float32
		err = allStocks.Scan(&region, &ticker, &curMarketCap, &share_stat1, &share_stat2, &share_stat3, &share_stat4, &share_stat5)
		if err != nil {
			return err
		}
		log.Println("Testing", region)
		req, err := http.NewRequest(http.MethodGet, `https://www.nationstates.net/cgi-bin/api.cgi?region=`+slug.Substitute(region, map[string]string{
			" ": "_",
		})+`&q=census&scale=255+76+74+66+48`, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "NWConifer Finance Application, by Gallaton")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			log.Println(resp.StatusCode)
			return err
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		var output Census
		err = xml.Unmarshal(body, &output)
		if err != nil {
			return err
		}
		var existingVals map[int]float32 = map[int]float32{
			255: share_stat1,
			76:  share_stat2,
			74:  share_stat3,
			66:  share_stat4,
			48:  share_stat5,
		}
		var percentMove float32
		var updatedVals []float32
		for i := 0; i < len(output.Scores); i++ {
			currentOne := output.Scores[i]
			log.Println(currentOne)
			percDiff := (currentOne.Score - existingVals[currentOne.Id]) / existingVals[currentOne.Id]
			percentMove += percDiff
			updatedVals = append(updatedVals, currentOne.Score)
		}

		allShareUpdates.Queue(`UPDATE stocks SET market_cap = $1, share_price = $2, share_stat1=$3, share_stat2=$4, share_stat3=$5, share_stat4=$6, share_stat5=$7 WHERE ticker = $8`, (curMarketCap * (1 + percentMove)), (curMarketCap*(1+percentMove))/1000000, updatedVals[0], updatedVals[1], updatedVals[2], updatedVals[3], updatedVals[4], ticker)
	}
	return dbConn.SendBatch(ctx, &allShareUpdates).Close()
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
