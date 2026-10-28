package main

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const azureAPI = "https://prices.azure.com/api/retail/prices"

type azureItem struct {
	ArmSkuName    string  `json:"armSkuName"`
	SkuName       string  `json:"skuName"`
	ProductName   string  `json:"productName"`
	UnitPrice     float64 `json:"unitPrice"`
	UnitOfMeasure string  `json:"unitOfMeasure"`
}

// fetchAzure reads Linux pay-as-you-go VM prices in EUR from the Azure Retail Prices API.
// The API has no vCPU or memory figures, so they are derived from the size name (see azureSpecs).
func fetchAzure() (*Source, error) {
	filter := "serviceName eq 'Virtual Machines' and armRegionName eq 'germanywestcentral' and priceType eq 'Consumption'"
	next := azureAPI + "?" + url.Values{"currencyCode": {"EUR"}, "$filter": {filter}}.Encode()
	src := &Source{Currency: "EUR", Source: azureAPI,
		Note: "Memory is derived from the VM size name and Azure's published ratio per series; check sizes marked estimated."}
	best := map[string]float64{}
	for next != "" {
		var page struct {
			Items        []azureItem `json:"Items"`
			NextPageLink string      `json:"NextPageLink"`
		}
		if err := getJSON(next, nil, &page); err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			name := lower(it.SkuName + " " + it.ProductName)
			if it.UnitOfMeasure != "1 Hour" || it.UnitPrice <= 0 || strings.Contains(name, "spot") ||
				strings.Contains(name, "low priority") || strings.Contains(name, "windows") {
				continue
			}
			if old, ok := best[it.ArmSkuName]; !ok || it.UnitPrice < old {
				best[it.ArmSkuName] = it.UnitPrice
			}
		}
		next = page.NextPageLink
	}
	for name, hourly := range best {
		s, ok := azureSpecs(name)
		if !ok {
			continue
		}
		s.Monthly = hourly * hoursPerMonth
		src.Servers = append(src.Servers, s)
	}
	return src, nil
}

// Standard_D4s_v5, Standard_E8-4ds_v5 (constrained vCPU), Standard_B2ms, Standard_A2m_v2, Standard_NC24ads_A100_v4
var azureSize = regexp.MustCompile(`^Standard_([A-Z]+)(\d+)(?:-(\d+))?([a-z]*)(?:_[A-Z0-9]+)?(?:_v(\d+))?$`)

// azureSpecs derives vCPU and memory from a VM size name. Sizes it cannot read are skipped.
func azureSpecs(name string) (Server, bool) {
	m := azureSize.FindStringSubmatch(name)
	if m == nil {
		return Server{}, false
	}
	series, suffix := m[1], m[4]
	vcpu, _ := strconv.ParseFloat(m[2], 64)
	if m[3] != "" { // constrained-core sizes keep the memory of the full size but expose fewer vCPUs
		vcpu, _ = strconv.ParseFloat(m[3], 64)
	}
	full, _ := strconv.ParseFloat(m[2], 64)
	gen, _ := strconv.Atoi(m[5])
	s := Server{Name: name, VCPU: vcpu, Arch: "x86", Estimated: true}
	if strings.Contains(suffix, "p") {
		s.Arch = "arm"
	}
	if series == "D" && gen <= 2 && full >= 11 && full <= 15 { // D11-D15 (v1, v2) are memory-optimised with their own sizes
		sizes := map[float64][2]float64{11: {2, 14}, 12: {4, 28}, 13: {8, 56}, 14: {16, 112}, 15: {20, 140}}
		s.VCPU, s.MemoryGiB, s.Category = sizes[full][0], sizes[full][1], "memory"
		return s, true
	}
	perCPU := 0.0
	switch series {
	case "D", "DC":
		s.Category, perCPU = "general", 4
		if series == "D" && gen <= 2 {
			perCPU = 3.5 // Dv2 and the original D series
		}
		if strings.Contains(suffix, "l") {
			perCPU = 2
		}
	case "E", "EC":
		s.Category, perCPU = "memory", 8
	case "F":
		s.Category, perCPU = "compute", 2
	case "A":
		if gen < 2 {
			s.Category = "other" // A v1: irregular sizes, retired
			break
		}
		s.Category, perCPU = "general", 2
		if strings.Contains(suffix, "m") {
			perCPU = 8
		}
	case "L":
		s.Category, perCPU = "storage", 8
	case "B":
		s.Category = "burstable"
		s.MemoryGiB = azureBurstable(full, suffix, gen)
	default: // M, H, N (GPU) and other special series: vCPU only
		s.Category = "other"
	}
	if perCPU > 0 {
		s.MemoryGiB = full * perCPU
	}
	return s, true
}

// azureBurstable covers the irregular B-series memory sizes.
func azureBurstable(cpu float64, suffix string, gen int) float64 {
	if gen >= 2 {
		switch {
		case strings.Contains(suffix, "t"):
			return cpu / 2
		case strings.Contains(suffix, "l"):
			return cpu * 2
		}
		return cpu * 4
	}
	switch {
	case strings.Contains(suffix, "ls"):
		return 0.5
	case strings.Contains(suffix, "ms"):
		return cpu * 4
	case cpu == 1:
		return 1
	}
	return cpu * 2
}
