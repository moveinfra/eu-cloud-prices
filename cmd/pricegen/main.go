// Command pricegen builds the EU Cloud Price Index.
//
//	go run ./cmd/pricegen fetch aws        # one provider's server list -> data/sources/aws.json
//	go run ./cmd/pricegen fetch all        # every provider that can be fetched with the credentials present
//	go run ./cmd/pricegen build            # data/sources + data/manual.json -> data/eu-cloud-prices.json, CSV, README tables
//
// Server lists come from the providers' own price APIs. Unit prices that have no public API (egress, object storage,
// load balancers, ...) live in data/manual.json, each with the page it was taken from.
// Run from the repo root. CI runs it monthly and opens a pull request with the changes.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	dataDir       = "data"
	hoursPerMonth = 730.0 // 365 * 24 / 12, the month hyperscalers bill by
)

// Source is one provider's server list in its own currency, as fetched.
type Source struct {
	Provider  string   `json:"provider"`
	Region    string   `json:"region"`
	Currency  string   `json:"currency"`
	Generated string   `json:"generated"`
	Source    string   `json:"source"`
	Note      string   `json:"note,omitempty"`
	Servers   []Server `json:"servers"`
}

// Server is one on-demand Linux server type.
type Server struct {
	Name      string  `json:"name"`
	VCPU      float64 `json:"vcpu"`
	MemoryGiB float64 `json:"memoryGiB"`           // 0 when unknown
	Monthly   float64 `json:"monthly"`             // price for a full month
	Category  string  `json:"category"`            // general, compute, memory, shared, burstable, storage, other
	Arch      string  `json:"arch,omitempty"`      // x86 or arm
	Estimated bool    `json:"estimated,omitempty"` // memory derived from the size name, not from the API
}

type fetcher struct {
	region string
	run    func() (*Source, error)
	needs  string // environment variable the fetcher needs, if any
}

var fetchers = map[string]fetcher{
	"aws":      {"eu-central-1", fetchAWS, ""},
	"azure":    {"germanywestcentral", fetchAzure, ""},
	"gcp":      {"europe-west3", fetchGCP, ""},
	"upcloud":  {"de-fra1", fetchUpCloud, "UPCLOUD_TOKEN"},
	"hetzner":  {"fsn1", fetchHetzner, "HCLOUD_TOKEN"},
	"scaleway": {"fr-par-1", fetchScaleway, ""},
	"ovhcloud": {"DE", fetchOVH, ""},
}

var client = &http.Client{Timeout: 30 * time.Minute}

func main() {
	var err error
	switch {
	case len(os.Args) == 3 && os.Args[1] == "fetch":
		err = fetch(os.Args[2])
	case len(os.Args) == 2 && os.Args[1] == "fx":
		err = fetchFX()
	case len(os.Args) == 2 && os.Args[1] == "build":
		err = build()
	default:
		err = fmt.Errorf("usage: pricegen fetch <provider|all> | fx | build")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pricegen:", err)
		os.Exit(1)
	}
}

func fetch(name string) error {
	if name == "all" {
		names := make([]string, 0, len(fetchers))
		for n := range fetchers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if env := fetchers[n].needs; env != "" && os.Getenv(env) == "" {
				fmt.Printf("%s: skipped, %s is not set (the previous data/sources/%s.json is kept)\n", n, env, n)
				continue
			}
			if err := fetch(n); err != nil {
				return err
			}
		}
		return fetchFX()
	}
	f, ok := fetchers[name]
	if !ok {
		return fmt.Errorf("unknown provider %q", name)
	}
	src, err := f.run()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	src.Provider, src.Region, src.Generated = name, f.region, today()
	sort.Slice(src.Servers, func(i, j int) bool {
		a, b := src.Servers[i], src.Servers[j]
		if a.Monthly != b.Monthly {
			return a.Monthly < b.Monthly
		}
		return a.Name < b.Name
	})
	for i := range src.Servers {
		src.Servers[i].Monthly = round(src.Servers[i].Monthly, 2)
	}
	fmt.Printf("%s %s: %d server types\n", name, f.region, len(src.Servers))
	return writeJSON(filepath.Join(dataDir, "sources", name+".json"), src)
}

func get(url string, header http.Header) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, body)
	}
	return resp.Body, nil
}

func getJSON(url string, h http.Header, v any) error {
	body, err := get(url, h)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(body).Decode(v)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func round(f float64, digits int) float64 {
	p := 1.0
	for range digits {
		p *= 10
	}
	if f < 0 {
		return -float64(int64(-f*p+0.5)) / p
	}
	return float64(int64(f*p+0.5)) / p
}

func lower(s string) string { return strings.ToLower(s) }
