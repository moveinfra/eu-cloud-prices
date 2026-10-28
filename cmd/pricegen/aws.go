package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const awsBase = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws"

type awsProduct struct {
	ProductFamily string            `json:"productFamily"`
	Attributes    map[string]string `json:"attributes"`
}

type awsTerm struct {
	PriceDimensions map[string]struct {
		PricePerUnit map[string]string `json:"pricePerUnit"`
	} `json:"priceDimensions"`
}

// fetchAWS streams the EC2 bulk price file for Frankfurt (~400 MB) and keeps Linux on-demand instances.
func fetchAWS() (*Source, error) {
	url := fmt.Sprintf("%s/AmazonEC2/current/eu-central-1/index.json", awsBase)
	src := &Source{Currency: "USD", Source: url}
	specs := map[string]Server{} // sku -> server without price

	body, err := get(url, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	dec := json.NewDecoder(body)
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch tok {
		case "products":
			err = eachEntry(dec, func(sku string) error {
				var p awsProduct
				if err := dec.Decode(&p); err != nil {
					return err
				}
				a := p.Attributes
				if p.ProductFamily != "Compute Instance" || a["locationType"] != "AWS Region" || a["operatingSystem"] != "Linux" ||
					a["tenancy"] != "Shared" || a["preInstalledSw"] != "NA" || a["capacitystatus"] != "Used" || a["operation"] != "RunInstances" {
					return nil
				}
				vcpu, _ := strconv.ParseFloat(a["vcpu"], 64)
				mem, _ := strconv.ParseFloat(strings.TrimSuffix(strings.ReplaceAll(a["memory"], ",", ""), " GiB"), 64)
				specs[sku] = Server{Name: a["instanceType"], VCPU: vcpu, MemoryGiB: mem, Category: awsCategory(a), Arch: awsArch(a)}
				return nil
			})
		case "terms":
			err = eachEntry(dec, func(kind string) error {
				if kind != "OnDemand" {
					return skip(dec)
				}
				return eachEntry(dec, func(sku string) error {
					s, ok := specs[sku]
					if !ok {
						return skip(dec)
					}
					var terms map[string]awsTerm
					if err := dec.Decode(&terms); err != nil {
						return err
					}
					for _, t := range terms {
						for _, d := range t.PriceDimensions {
							if price, err := strconv.ParseFloat(d.PricePerUnit["USD"], 64); err == nil && price > 0 {
								s.Monthly = price * hoursPerMonth
								src.Servers = append(src.Servers, s)
							}
						}
					}
					return nil
				})
			})
		default:
			err = skip(dec)
		}
		if err != nil {
			return nil, err
		}
	}
	return src, nil
}

func awsCategory(a map[string]string) string {
	t := a["instanceType"]
	switch {
	case strings.HasPrefix(t, "t"):
		return "burstable"
	case a["instanceFamily"] == "General purpose":
		return "general"
	case a["instanceFamily"] == "Compute optimized":
		return "compute"
	case a["instanceFamily"] == "Memory optimized":
		return "memory"
	case a["instanceFamily"] == "Storage optimized":
		return "storage"
	}
	return "other" // GPU, FPGA, machine learning chips
}

func awsArch(a map[string]string) string {
	if strings.Contains(lower(a["physicalProcessor"]), "graviton") {
		return "arm"
	}
	return "x86"
}

// eachEntry walks a JSON object, calling fn with each key while the decoder sits on its value.
func eachEntry(dec *json.Decoder, fn func(key string) error) error {
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("unexpected token %v", tok)
		}
		if err := fn(key); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

// skip consumes the next value token by token, so huge sections (Reserved terms) are never held in memory.
func skip(dec *json.Decoder) error {
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}
