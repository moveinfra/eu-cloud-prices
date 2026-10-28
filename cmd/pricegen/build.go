package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Provider describes one column of the index.
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	EUOwned  bool   `json:"euOwned"`
	Region   string `json:"region"`
	Location string `json:"location"`
	Currency string `json:"sourceCurrency"`
	Source   string `json:"serverSource"`
	Fetched  string `json:"serversFetched"`
	Note     string `json:"note,omitempty"`
}

var providerInfo = []Provider{
	{ID: "aws", Name: "AWS", Location: "Frankfurt"},
	{ID: "azure", Name: "Azure", Location: "Frankfurt (Germany West Central)"},
	{ID: "gcp", Name: "Google Cloud", Location: "Frankfurt"},
	{ID: "upcloud", Name: "UpCloud", EUOwned: true, Location: "Frankfurt"},
	{ID: "hetzner", Name: "Hetzner", EUOwned: true, Location: "Falkenstein"},
	{ID: "scaleway", Name: "Scaleway", EUOwned: true, Location: "Paris"},
	{ID: "ovhcloud", Name: "OVHcloud", EUOwned: true, Location: "Europe"},
}

// Unit is a single unit price in EUR with where it came from.
type Unit struct {
	Price   float64 `json:"price"`
	Product string  `json:"product,omitempty"`
	Source  string  `json:"source"`
	Note    string  `json:"note,omitempty"`
}

// Egress is internet data transfer out, in EUR. Tiers apply to the traffic above the free allowance.
type Egress struct {
	FreeGB    float64 `json:"freeGB"`
	PerServer bool    `json:"perServer"` // the free allowance comes with every server
	Tiers     []Tier  `json:"tiers"`
	Source    string  `json:"source"`
	Note      string  `json:"note,omitempty"`
}

type Tier struct {
	FromGB float64  `json:"fromGB"`
	ToGB   *float64 `json:"toGB"` // nil = no upper bound
	PerGB  float64  `json:"perGB"`
}

// Services holds the unit prices of one provider. Nil means not offered or not verified.
type Services struct {
	BlockStorageGBMonth  *Unit   `json:"blockStorageGBMonth"`
	ObjectStorageGBMonth *Unit   `json:"objectStorageGBMonth"`
	LoadBalancerMonthly  *Unit   `json:"loadBalancerMonthly"`
	KubernetesMonthly    *Unit   `json:"kubernetesMonthly"`
	PostgresMonthly      *Unit   `json:"postgres2vCPU8GBMonthly"`
	Egress               *Egress `json:"egress"`
}

// Price is one cell of a basket.
type Price struct {
	Monthly float64 `json:"monthly"`
	Detail  string  `json:"detail"`
}

type Basket struct {
	ID     string            `json:"id"`
	Label  string            `json:"label"`
	Prices map[string]*Price `json:"prices"` // nil = not offered or not verified
}

type Index struct {
	Generated string `json:"generated"`
	Currency  string `json:"currency"`
	License   string `json:"license"`
	FX        struct {
		Date      string  `json:"date"`
		USDPerEUR float64 `json:"usdPerEur"`
		Source    string  `json:"source"`
	} `json:"fx"`
	Method    []string             `json:"method"`
	Providers []Provider           `json:"providers"`
	Baskets   []Basket             `json:"baskets"`
	Services  map[string]*Services `json:"services"`
	Servers   map[string][]Server  `json:"servers"`
}

var method = []string{
	"Prices are list prices in EUR per month, excluding VAT, without discounts, commitments or free tiers unless stated.",
	"A month is 730 hours, or the provider's monthly cap when it bills less (UpCloud 672 hours).",
	"USD prices are converted at the ECB reference rate of the date shown.",
	"Server baskets pick the cheapest x86 server with at least the stated vCPU and memory from general-purpose, compute- or memory-optimised ranges. Shared-core and burstable servers are excluded; vCPUs are compared one to one.",
	"Server lists come from each provider's price API. Other unit prices come from data/manual.json, each with the page it was taken from.",
}

func build() error {
	var fx FX
	if err := readJSON(filepath.Join(dataDir, "sources", "fx.json"), &fx); err != nil {
		return err
	}
	var manual struct {
		Checked   string                    `json:"checked"`
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := readJSON(filepath.Join(dataDir, "manual.json"), &manual); err != nil {
		return err
	}
	idx := &Index{Generated: today(), Currency: "EUR", License: "CC BY 4.0, MoveInfra (https://moveinfra.eu/)", Method: method,
		Services: map[string]*Services{}, Servers: map[string][]Server{}}
	idx.FX.Date, idx.FX.USDPerEUR, idx.FX.Source = fx.Date, fx.Rates["USD"], fx.Source

	toEUR := func(v float64, currency string) float64 {
		if currency == "" || currency == "EUR" {
			return v
		}
		return v / fx.Rates[currency]
	}

	for _, p := range providerInfo {
		var src Source
		if err := readJSON(filepath.Join(dataDir, "sources", p.ID+".json"), &src); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			src = hetznerFromManual(manual.Providers[p.ID])
			if len(src.Servers) == 0 {
				return fmt.Errorf("no server list for %s: run pricegen fetch %s", p.ID, p.ID)
			}
		}
		p.Region, p.Currency, p.Source, p.Fetched, p.Note = src.Region, src.Currency, src.Source, src.Generated, src.Note
		idx.Providers = append(idx.Providers, p)
		servers := make([]Server, 0, len(src.Servers))
		for _, s := range src.Servers {
			s.Monthly = round(toEUR(s.Monthly, src.Currency), 2)
			servers = append(servers, s)
		}
		idx.Servers[p.ID] = servers
		idx.Services[p.ID] = services(manual.Providers[p.ID], toEUR)
	}
	idx.Baskets = baskets(idx)

	if err := writeJSON(filepath.Join(dataDir, "eu-cloud-prices.json"), idx); err != nil {
		return err
	}
	if err := writeCSV(idx); err != nil {
		return err
	}
	return writeReadme(idx, manual.Checked)
}

// hetznerFromManual is the fallback when no HCLOUD_TOKEN was available: the plan list checked by hand.
func hetznerFromManual(m map[string]any) Source {
	src := Source{Region: "fsn1", Currency: "EUR", Source: str(m, "hetznerServers", "source"), Generated: "manual",
		Note: "Plan list checked by hand on the Hetzner website (no API token in this run). A primary IPv4 address is billed separately."}
	list, _ := dig(m, "hetznerServers", "plans").([]any)
	if list == nil {
		list, _ = dig(m, "hetznerServers").([]any)
	}
	for _, it := range list {
		e, _ := it.(map[string]any)
		name := strings.ToUpper(fmt.Sprint(e["name"]))
		cat := "shared"
		if strings.HasPrefix(name, "CCX") {
			cat = "general"
		}
		src.Servers = append(src.Servers, Server{Name: name, VCPU: num(e["vcpu"]), MemoryGiB: num(e["ramGB"], e["memoryGB"]),
			Monthly: num(e["monthly"], e["monthlyEUR"], e["priceMonthly"]), Category: cat, Arch: "x86"})
	}
	return src
}

func services(m map[string]any, toEUR func(float64, string) float64) *Services {
	unit := func(key, field string) *Unit {
		obj, _ := m[key].(map[string]any)
		v, ok := obj[field].(float64)
		if !ok {
			return nil
		}
		return &Unit{Price: round(toEUR(v, str(obj, "currency")), 4), Source: str(obj, "source"), Note: str(obj, "note"),
			Product: firstNonEmpty(str(obj, "product"), str(obj, "plan"), str(obj, "tier"))}
	}
	s := &Services{
		BlockStorageGBMonth:  unit("blockStorage", "perGBMonth"),
		ObjectStorageGBMonth: unit("objectStorage", "perGBMonth"),
		LoadBalancerMonthly:  unit("loadBalancer", "monthly"),
		KubernetesMonthly:    unit("kubernetesControlPlane", "monthly"),
		PostgresMonthly:      unit("managedPostgres", "monthly"),
	}
	if e, ok := m["egress"].(map[string]any); ok {
		cur := str(e, "currency")
		eg := &Egress{FreeGB: num(e["freeGBPerMonth"]), Source: str(e, "source"), Note: str(e, "note"),
			PerServer: strings.Contains(lower(str(e, "freeScope")), "server")}
		tiers, _ := e["tiers"].([]any)
		for _, t := range tiers {
			tm, _ := t.(map[string]any)
			tier := Tier{FromGB: num(tm["fromGB"]), PerGB: round(toEUR(num(tm["perGB"]), cur), 4)}
			if to, ok := tm["toGB"].(float64); ok {
				tier.ToGB = &to
			}
			eg.Tiers = append(eg.Tiers, tier)
		}
		s.Egress = eg
	}
	return s
}

// egressCost prices gb of internet egress for a setup with the given number of servers.
func egressCost(e *Egress, gb float64, servers int) (float64, bool) {
	if e == nil {
		return 0, false
	}
	free := e.FreeGB
	if e.PerServer {
		free *= float64(servers)
	}
	billable := math.Max(0, gb-free)
	cost := 0.0
	for _, t := range e.Tiers {
		hi := math.Inf(1)
		if t.ToGB != nil {
			hi = *t.ToGB
		}
		if billable > t.FromGB {
			cost += (math.Min(billable, hi) - t.FromGB) * t.PerGB
		}
	}
	return cost, true
}

func cheapestServer(servers []Server, vcpu, mem float64) *Server {
	var best *Server
	for i, s := range servers {
		if s.Arch != "x86" || s.VCPU < vcpu || s.MemoryGiB < mem ||
			(s.Category != "general" && s.Category != "compute" && s.Category != "memory") {
			continue
		}
		if best == nil || s.Monthly < best.Monthly {
			best = &servers[i]
		}
	}
	return best
}

func baskets(idx *Index) []Basket {
	var out []Basket
	add := func(id, label string, fn func(p string) *Price) {
		b := Basket{ID: id, Label: label, Prices: map[string]*Price{}}
		for _, p := range idx.Providers {
			b.Prices[p.ID] = fn(p.ID)
		}
		out = append(out, b)
	}
	for _, size := range [][2]float64{{2, 8}, {4, 16}, {8, 32}, {16, 64}} {
		add(fmt.Sprintf("server-%g-%g", size[0], size[1]), fmt.Sprintf("Server, %g vCPU, %g GB", size[0], size[1]), func(p string) *Price {
			if s := cheapestServer(idx.Servers[p], size[0], size[1]); s != nil {
				return &Price{Monthly: s.Monthly, Detail: s.Name}
			}
			return nil
		})
	}
	times := func(get func(*Services) *Unit, n float64) func(string) *Price {
		return func(p string) *Price {
			if u := get(idx.Services[p]); u != nil {
				return &Price{Monthly: round(u.Price*n, 2), Detail: u.Product}
			}
			return nil
		}
	}
	add("block-1tb", "Block storage, 1 TB SSD", times(func(s *Services) *Unit { return s.BlockStorageGBMonth }, 1000))
	add("object-1tb", "Object storage, 1 TB", times(func(s *Services) *Unit { return s.ObjectStorageGBMonth }, 1000))
	for _, tb := range []float64{1, 10} {
		add(fmt.Sprintf("egress-%gtb", tb), fmt.Sprintf("Internet egress, %g TB from one server", tb), func(p string) *Price {
			if c, ok := egressCost(idx.Services[p].Egress, tb*1000, 1); ok {
				return &Price{Monthly: round(c, 2), Detail: idx.Services[p].Egress.Note}
			}
			return nil
		})
	}
	add("load-balancer", "Managed load balancer", times(func(s *Services) *Unit { return s.LoadBalancerMonthly }, 1))
	add("kubernetes", "Managed Kubernetes control plane", times(func(s *Services) *Unit { return s.KubernetesMonthly }, 1))
	add("postgres-2-8", "Managed PostgreSQL, 2 vCPU, 8 GB", times(func(s *Services) *Unit { return s.PostgresMonthly }, 1))

	cell := func(id, p string) *Price {
		for _, b := range out {
			if b.ID == id {
				return b.Prices[p]
			}
		}
		return nil
	}
	// A small production setup, the kind most companies we talk to run.
	add("stack", "Example stack", func(p string) *Price {
		s := idx.Services[p]
		srv, obj, lb := cell("server-4-16", p), cell("object-1tb", p), cell("load-balancer", p)
		if srv == nil || s.BlockStorageGBMonth == nil || obj == nil || lb == nil {
			return nil
		}
		eg, ok := egressCost(s.Egress, 2000, 3)
		if !ok {
			return nil
		}
		total := 3*srv.Monthly + 500*s.BlockStorageGBMonth.Price + obj.Monthly + eg + lb.Monthly
		detail := "managed PostgreSQL"
		if pg := cell("postgres-2-8", p); pg != nil {
			total += pg.Monthly
		} else if vm := cell("server-2-8", p); vm != nil {
			total += vm.Monthly
			detail = "no managed PostgreSQL: self-run on a 2 vCPU 8 GB server"
		} else {
			return nil
		}
		return &Price{Monthly: round(total, 2), Detail: detail}
	})
	return out
}

func writeCSV(idx *Index) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"basket", "label", "provider", "eu_owned", "monthly_eur", "detail"})
	for _, b := range idx.Baskets {
		for _, p := range idx.Providers {
			if pr := b.Prices[p.ID]; pr != nil {
				w.Write([]string{b.ID, b.Label, p.ID, fmt.Sprint(p.EUOwned), fmt.Sprintf("%.2f", pr.Monthly), pr.Detail})
			} else {
				w.Write([]string{b.ID, b.Label, p.ID, fmt.Sprint(p.EUOwned), "", "not offered or not verified"})
			}
		}
	}
	w.Flush()
	path := filepath.Join(dataDir, "eu-cloud-prices.csv")
	fmt.Println("wrote", path)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// writeReadme replaces the generated section of README.md.
func writeReadme(idx *Index, checked string) error {
	const start, end = "<!-- prices:start -->", "<!-- prices:end -->"
	raw, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	readme := string(raw)
	i, j := strings.Index(readme, start), strings.Index(readme, end)
	if i < 0 || j < i {
		return fmt.Errorf("README.md has no %s ... %s section", start, end)
	}
	var b strings.Builder
	b.WriteString(start + "\n")
	fmt.Fprintf(&b, "_EUR per month, excluding VAT. Server prices fetched %s, other prices checked %s, USD at %.4f per EUR (ECB, %s). Lowest in bold._\n\n",
		idx.Generated, checked, idx.FX.USDPerEUR, idx.FX.Date)
	b.WriteString("| | ")
	for _, p := range idx.Providers {
		name := p.Name
		if p.EUOwned {
			name += " 🇪🇺"
		}
		b.WriteString(name + " | ")
	}
	b.WriteString("\n|---|" + strings.Repeat("--:|", len(idx.Providers)) + "\n")
	for _, bk := range idx.Baskets {
		label := bk.Label
		if bk.ID == "stack" {
			label = "**Example stack**¹"
		}
		b.WriteString("| " + label + " | ")
		low := lowest(bk)
		for _, p := range idx.Providers {
			pr := bk.Prices[p.ID]
			switch {
			case pr == nil:
				b.WriteString("– | ")
			case pr.Monthly == 0:
				b.WriteString("**included** | ")
			case pr.Monthly == low:
				fmt.Fprintf(&b, "**%s** | ", money(pr.Monthly))
			default:
				b.WriteString(money(pr.Monthly) + " | ")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n¹ 3 servers with 4 vCPU and 16 GB, 500 GB block storage, 1 TB object storage, 2 TB internet egress, one load balancer and managed PostgreSQL with 2 vCPU and 8 GB. Where a provider has no managed PostgreSQL, the stack runs it on one more server.\n\n")
	b.WriteString("<details><summary>Which server each basket picked</summary>\n\n| | ")
	for _, p := range idx.Providers {
		b.WriteString(p.Name + " | ")
	}
	b.WriteString("\n|---|" + strings.Repeat("---|", len(idx.Providers)) + "\n")
	for _, bk := range idx.Baskets {
		if !strings.HasPrefix(bk.ID, "server-") {
			continue
		}
		b.WriteString("| " + bk.Label + " | ")
		for _, p := range idx.Providers {
			if pr := bk.Prices[p.ID]; pr != nil {
				b.WriteString("`" + pr.Detail + "` | ")
			} else {
				b.WriteString("– | ")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n</details>\n" + end)
	readme = readme[:i] + b.String() + readme[j+len(end):]
	fmt.Println("wrote README.md")
	return os.WriteFile("README.md", []byte(readme), 0o644)
}

func lowest(b Basket) float64 {
	low := math.Inf(1)
	for _, p := range b.Prices {
		if p != nil && p.Monthly < low {
			low = p.Monthly
		}
	}
	return low
}

func money(v float64) string {
	if v >= 100 {
		s := fmt.Sprintf("%.0f", v)
		for i := len(s) - 3; i > 0; i -= 3 {
			s = s[:i] + "," + s[i:]
		}
		return "€" + s
	}
	return fmt.Sprintf("€%.2f", v)
}

func dig(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func str(m map[string]any, keys ...string) string {
	if v, ok := dig(m, keys...).(string); ok {
		return v
	}
	return ""
}

func num(vs ...any) float64 {
	for _, v := range vs {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
