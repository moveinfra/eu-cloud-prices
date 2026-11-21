package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// fetchUpCloud reads plans and Frankfurt prices from the UpCloud API (token in UPCLOUD_TOKEN, read access is enough).
// Prices are in euro cents per hour; UpCloud caps a month at 672 billable hours.
func fetchUpCloud() (*Source, error) {
	const api = "https://api.upcloud.com/1.3"
	h := http.Header{"Authorization": {"Bearer " + os.Getenv("UPCLOUD_TOKEN")}, "Accept": {"application/json"}}
	var plans struct {
		Plans struct {
			Plan []struct {
				Name   string `json:"name"`
				Cores  int    `json:"core_number"`
				Memory int    `json:"memory_amount"` // MiB
				GPUs   int    `json:"gpu_amount"`
			} `json:"plan"`
		} `json:"plans"`
	}
	if err := getJSON(api+"/plan", h, &plans); err != nil {
		return nil, err
	}
	var prices struct {
		Prices struct {
			Zone []map[string]json.RawMessage `json:"zone"`
		} `json:"prices"`
	}
	if err := getJSON(api+"/price", h, &prices); err != nil {
		return nil, err
	}
	src := &Source{Currency: "EUR", Source: api + "/price", Note: "Monthly price is 672 hours, UpCloud's monthly cap."}
	for _, zone := range prices.Prices.Zone {
		var name string
		json.Unmarshal(zone["name"], &name)
		if name != "de-fra1" {
			continue
		}
		for _, p := range plans.Plans.Plan {
			var it struct {
				Price float64 `json:"price"`
			}
			raw, ok := zone["server_plan_"+p.Name]
			if p.GPUs > 0 || !ok || json.Unmarshal(raw, &it) != nil || it.Price == 0 {
				continue
			}
			src.Servers = append(src.Servers, Server{Name: p.Name, VCPU: float64(p.Cores), MemoryGiB: float64(p.Memory) / 1024,
				Monthly: it.Price / 100 * 672, Category: upcloudCategory(p.Name), Arch: "x86"})
		}
	}
	if len(src.Servers) == 0 {
		return nil, fmt.Errorf("no de-fra1 plans in the API response")
	}
	return src, nil
}

func upcloudCategory(name string) string {
	switch {
	case strings.HasPrefix(name, "DEV-"):
		return "shared"
	case strings.HasPrefix(name, "HICPU-"):
		return "compute"
	case strings.HasPrefix(name, "HIMEM-"):
		return "memory"
	case strings.HasPrefix(name, "CLOUDNATIVE-"):
		return "other"
	}
	return "general"
}

// fetchHetzner reads server types from the Hetzner Cloud API (token in HCLOUD_TOKEN, a read-only project token).
func fetchHetzner() (*Source, error) {
	h := http.Header{"Authorization": {"Bearer " + os.Getenv("HCLOUD_TOKEN")}}
	var resp struct {
		ServerTypes []struct {
			Name         string  `json:"name"`
			Cores        float64 `json:"cores"`
			Memory       float64 `json:"memory"` // GB
			CPUType      string  `json:"cpu_type"`
			Architecture string  `json:"architecture"`
			Deprecation  any     `json:"deprecation"`
			Prices       []struct {
				Location     string `json:"location"`
				PriceMonthly struct {
					Net string `json:"net"`
				} `json:"price_monthly"`
			} `json:"prices"`
		} `json:"server_types"`
	}
	const url = "https://api.hetzner.cloud/v1/server_types?per_page=50"
	if err := getJSON(url, h, &resp); err != nil {
		return nil, err
	}
	src := &Source{Currency: "EUR", Source: url, Note: "Net prices. A primary IPv4 address is billed separately."}
	for _, t := range resp.ServerTypes {
		if t.Deprecation != nil {
			continue
		}
		for _, p := range t.Prices {
			var monthly float64
			if p.Location != "fsn1" || parseFloat(p.PriceMonthly.Net, &monthly) != nil {
				continue
			}
			cat := "general"
			if t.CPUType == "shared" {
				cat = "shared"
			}
			arch := "x86"
			if t.Architecture == "arm" {
				arch = "arm"
			}
			src.Servers = append(src.Servers, Server{Name: strings.ToUpper(t.Name), VCPU: t.Cores, MemoryGiB: t.Memory,
				Monthly: monthly, Category: cat, Arch: arch})
		}
	}
	return src, nil
}

// Scaleway bills Instances by the hour; a month is 730 hours, the figure its own API used for monthly_price.
const scalewayHoursPerMonth = 730

// fetchScaleway reads Paris 1 Instances from two public endpoints: the Instance product list for the sizes, and the
// public product catalogue for the prices. Since November 2026 the product list returns 0 for every price.
func fetchScaleway() (*Source, error) {
	const zone = "fr-par-1"
	const base = "https://api.scaleway.com/instance/v1/zones/" + zone + "/products/servers"
	const catalog = "https://api.scaleway.com/product-catalog/v2alpha1/public-catalog/products"

	hourly := map[string]float64{} // server name -> EUR per hour
	for page := 1; ; page++ {
		var resp struct {
			Products []scalewayProduct `json:"products"`
		}
		if err := getJSON(fmt.Sprintf("%s?product_types=instance&page_size=100&page=%d", catalog, page), nil, &resp); err != nil {
			return nil, err
		}
		for _, p := range resp.Products {
			if h, ok := p.hourlyIn(zone); ok {
				hourly[p.Product] = h
			}
		}
		if len(resp.Products) < 100 {
			break
		}
	}

	src := &Source{Currency: "EUR", Source: catalog + "?product_types=instance"}
	for page := 1; ; page++ {
		var resp struct {
			Servers map[string]struct {
				NCPUs        float64 `json:"ncpus"`
				RAM          float64 `json:"ram"` // bytes
				GPU          int     `json:"gpu"`
				Arch         string  `json:"arch"`
				EndOfService bool    `json:"end_of_service"`
			} `json:"servers"`
		}
		if err := getJSON(fmt.Sprintf("%s?per_page=100&page=%d", base, page), nil, &resp); err != nil {
			return nil, err
		}
		for name, s := range resp.Servers {
			h := hourly[name]
			if s.GPU > 0 || s.EndOfService || h <= 0 {
				continue
			}
			arch := "x86"
			if s.Arch == "arm64" {
				arch = "arm"
			}
			src.Servers = append(src.Servers, Server{Name: name, VCPU: s.NCPUs, MemoryGiB: s.RAM / (1 << 30),
				Monthly: round(h*scalewayHoursPerMonth, 2), Category: scalewayCategory(name), Arch: arch})
		}
		if len(resp.Servers) < 100 {
			break
		}
	}
	if len(src.Servers) == 0 {
		return nil, fmt.Errorf("scaleway: no priced servers in %s (%d prices in the catalogue): the API changed again", zone, len(hourly))
	}
	return src, nil
}

type scalewayProduct struct {
	SKU      string `json:"sku"`
	Product  string `json:"product"`
	Status   string `json:"status"`
	Locality struct {
		Zone string `json:"zone"`
	} `json:"locality"`
	Price struct {
		Retail struct {
			Units any     `json:"units"` // number or string, depending on the API version
			Nanos float64 `json:"nanos"`
		} `json:"retail_price"`
	} `json:"price"`
	Unit struct {
		Unit string `json:"unit"`
	} `json:"unit_of_measure"`
}

// hourlyIn returns the EUR price per hour of an Instance that is on sale in the zone.
func (p scalewayProduct) hourlyIn(zone string) (float64, bool) {
	if p.Locality.Zone != zone || p.Status == "retired" || !strings.HasPrefix(p.SKU, "/instance/server/") {
		return 0, false
	}
	var units float64
	fmt.Sscan(fmt.Sprint(p.Price.Retail.Units), &units)
	price := units + p.Price.Retail.Nanos/1e9
	switch p.Unit.Unit {
	case "hour":
	case "minute":
		price *= 60
	default:
		return 0, false
	}
	return price, price > 0
}

func scalewayCategory(name string) string {
	switch {
	case strings.HasPrefix(name, "DEV1") || strings.HasPrefix(name, "PLAY2") || strings.HasPrefix(name, "STARDUST") ||
		strings.HasPrefix(name, "START1") || strings.HasPrefix(name, "VC1") || strings.HasPrefix(name, "BASIC"):
		return "shared"
	case strings.Contains(name, "-HC-") || strings.HasPrefix(name, "COMPUTE"):
		return "compute"
	case strings.Contains(name, "-HM-") || strings.HasPrefix(name, "MEMORY"):
		return "memory"
	case strings.HasPrefix(name, "X64") || strings.HasPrefix(name, "ARM64") || strings.Contains(name, "-HN-"):
		return "other"
	}
	return "general"
}

// fetchOVH reads the public Public Cloud catalogue (German subsidiary, EUR, net prices) and keeps hourly-billed
// Linux instances of the current ranges. OVHcloud prices most instances the same in all European regions.
func fetchOVH() (*Source, error) {
	const url = "https://eu.api.ovh.com/v1/order/catalog/public/cloud?ovhSubsidiary=DE"
	var cat struct {
		Addons []struct {
			PlanCode string `json:"planCode"`
			Product  string `json:"product"`
			Pricings []struct {
				Price float64 `json:"price"` // 1e-8 EUR
			} `json:"pricings"`
			Blobs struct {
				Commercial struct {
					BrickSubtype string `json:"brickSubtype"`
					Name         string `json:"name"`
				} `json:"commercial"`
				Technical struct {
					CPU struct {
						Cores float64 `json:"cores"`
					} `json:"cpu"`
					Memory struct {
						Size float64 `json:"size"`
					} `json:"memory"`
				} `json:"technical"`
			} `json:"blobs"`
		} `json:"addons"`
	}
	if err := getJSON(url, nil, &cat); err != nil {
		return nil, err
	}
	src := &Source{Currency: "EUR", Source: url, Note: "Hourly price x 730. OVHcloud also offers cheaper monthly billing for some ranges."}
	cats := map[string]string{"general-purpose": "general", "cpu": "compute", "ram": "memory", "iops": "storage", "discovery": "shared"}
	for _, a := range cat.Addons {
		c, ok := cats[a.Blobs.Commercial.BrickSubtype]
		if a.Product != "publiccloud-instance" || !ok || !strings.HasSuffix(a.PlanCode, ".consumption") ||
			strings.HasPrefix(a.PlanCode, "win-") || len(a.Pricings) == 0 || a.Blobs.Technical.CPU.Cores == 0 {
			continue
		}
		src.Servers = append(src.Servers, Server{Name: a.Blobs.Commercial.Name, VCPU: a.Blobs.Technical.CPU.Cores,
			MemoryGiB: a.Blobs.Technical.Memory.Size, Monthly: a.Pricings[0].Price / 1e8 * hoursPerMonth, Category: c, Arch: "x86"})
	}
	return src, nil
}

func parseFloat(s string, f *float64) error {
	_, err := fmt.Sscan(s, f)
	return err
}
