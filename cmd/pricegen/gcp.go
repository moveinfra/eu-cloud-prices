package main

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Google's Cloud Billing Catalog API needs an API key and lists SKUs per vCPU and per GB, not per machine type.
// Nils Knieling's google-cloud-pricing-cost-calculator (Apache-2.0) publishes a per-machine-type price list generated
// from that API and refreshed regularly; we read it instead.
const gcpPricing = "https://raw.githubusercontent.com/Cyclenerd/google-cloud-pricing-cost-calculator/master/pricing.yml"

func fetchGCP() (*Source, error) {
	body, err := get(gcpPricing, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var doc struct {
		About struct {
			Generated string `yaml:"generated"`
		} `yaml:"about"`
		Compute struct {
			Instance map[string]struct {
				CPU  float64 `yaml:"cpu"`
				RAM  float64 `yaml:"ram"`
				Cost map[string]struct {
					Month float64 `yaml:"month"`
				} `yaml:"cost"`
			} `yaml:"instance"`
		} `yaml:"compute"`
	}
	if err := yaml.NewDecoder(body).Decode(&doc); err != nil {
		return nil, err
	}
	src := &Source{Currency: "USD", Source: gcpPricing,
		Note: "Generated from the Google Cloud Billing Catalog API by github.com/Cyclenerd/google-cloud-pricing-cost-calculator on " + doc.About.Generated + "."}
	for name, it := range doc.Compute.Instance {
		price := it.Cost["europe-west3"].Month
		if price <= 0 || it.CPU <= 0 {
			continue
		}
		src.Servers = append(src.Servers, Server{Name: name, VCPU: it.CPU, MemoryGiB: it.RAM, Monthly: price,
			Category: gcpCategory(name), Arch: gcpArch(name)})
	}
	return src, nil
}

func gcpCategory(name string) string {
	family, kind, _ := strings.Cut(name, "-")
	switch {
	case name == "e2-micro" || name == "e2-small" || name == "e2-medium" || name == "f1-micro" || name == "g1-small":
		return "shared"
	case strings.Contains(kind, "highmem") || strings.Contains(kind, "megamem") || strings.Contains(kind, "ultramem") ||
		strings.HasPrefix(family, "m"):
		return "memory"
	case strings.Contains(kind, "highcpu") || strings.HasPrefix(family, "c") || family == "h3":
		return "compute"
	case strings.HasPrefix(family, "a") || strings.HasPrefix(family, "g") || strings.HasPrefix(family, "ct"):
		return "other" // GPU and TPU machines
	case strings.HasPrefix(family, "z"):
		return "storage"
	}
	return "general"
}

func gcpArch(name string) string {
	family, _, _ := strings.Cut(name, "-")
	if family == "t2a" || family == "c4a" || family == "n4a" {
		return "arm"
	}
	return "x86"
}
