package main

import (
	"encoding/xml"
	"path/filepath"
	"strconv"
)

const ecbDaily = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"

// FX holds ECB reference rates as units of currency per 1 EUR.
type FX struct {
	Date   string             `json:"date"`
	Source string             `json:"source"`
	Rates  map[string]float64 `json:"rates"`
}

func fetchFX() error {
	body, err := get(ecbDaily, nil)
	if err != nil {
		return err
	}
	defer body.Close()
	var doc struct {
		Cube struct {
			Cube struct {
				Time  string `xml:"time,attr"`
				Rates []struct {
					Currency string `xml:"currency,attr"`
					Rate     string `xml:"rate,attr"`
				} `xml:"Cube"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	}
	if err := xml.NewDecoder(body).Decode(&doc); err != nil {
		return err
	}
	fx := &FX{Date: doc.Cube.Cube.Time, Source: ecbDaily, Rates: map[string]float64{}}
	for _, r := range doc.Cube.Cube.Rates {
		if v, err := strconv.ParseFloat(r.Rate, 64); err == nil {
			fx.Rates[r.Currency] = v
		}
	}
	return writeJSON(filepath.Join(dataDir, "sources", "fx.json"), fx)
}
