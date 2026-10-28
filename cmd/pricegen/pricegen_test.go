package main

import "testing"

func TestAzureSpecs(t *testing.T) {
	cases := []struct {
		name      string
		vcpu, mem float64
		category  string
	}{
		{"Standard_D4s_v5", 4, 16, "general"},
		{"Standard_D2_v2", 2, 7, "general"},
		{"Standard_D4ls_v5", 4, 8, "general"},
		{"Standard_E8-4ds_v5", 4, 64, "memory"},
		{"Standard_F4s_v2", 4, 8, "compute"},
		{"Standard_B2ms", 2, 8, "burstable"},
		{"Standard_B1s", 1, 1, "burstable"},
		{"Standard_B2ts_v2", 2, 1, "burstable"},
		{"Standard_A2m_v2", 2, 16, "general"},
		{"Standard_NC24ads_A100_v4", 24, 0, "other"},
		{"Standard_D11_v2", 2, 14, "memory"},
		{"Standard_D13", 8, 56, "memory"},
	}
	for _, c := range cases {
		s, ok := azureSpecs(c.name)
		if !ok || s.VCPU != c.vcpu || s.MemoryGiB != c.mem || s.Category != c.category {
			t.Errorf("%s: got %+v ok=%v, want %v vCPU %v GiB %s", c.name, s, ok, c.vcpu, c.mem, c.category)
		}
	}
	if s, _ := azureSpecs("Standard_D4ps_v5"); s.Arch != "arm" {
		t.Errorf("D4ps_v5 should be arm, got %s", s.Arch)
	}
}

func TestEgressCost(t *testing.T) {
	ten := 10240.0
	aws := &Egress{FreeGB: 100, Tiers: []Tier{{0, &ten, 0.09}, {ten, nil, 0.085}}}
	if c, _ := egressCost(aws, 1100, 1); round(c, 2) != 90 {
		t.Errorf("1100 GB with 100 free at 0.09: got %v, want 90", c)
	}
	if c, _ := egressCost(aws, 100+ten+1000, 1); round(c, 2) != round(ten*0.09+1000*0.085, 2) {
		t.Errorf("second tier: got %v", c)
	}
	hetzner := &Egress{FreeGB: 20000, PerServer: true, Tiers: []Tier{{0, nil, 0.001}}}
	if c, _ := egressCost(hetzner, 50000, 3); c != 0 {
		t.Errorf("3 servers include 60 TB: got %v, want 0", c)
	}
	if c, _ := egressCost(hetzner, 25000, 1); round(c, 2) != 5 {
		t.Errorf("5 TB over the allowance at 1 EUR/TB: got %v, want 5", c)
	}
	if _, ok := egressCost(nil, 1, 1); ok {
		t.Error("nil egress must report not priced")
	}
}

func TestCheapestServer(t *testing.T) {
	servers := []Server{
		{Name: "burst", VCPU: 4, MemoryGiB: 16, Monthly: 10, Category: "burstable", Arch: "x86"},
		{Name: "arm", VCPU: 4, MemoryGiB: 16, Monthly: 20, Category: "general", Arch: "arm"},
		{Name: "small", VCPU: 4, MemoryGiB: 8, Monthly: 30, Category: "general", Arch: "x86"},
		{Name: "fit", VCPU: 4, MemoryGiB: 16, Monthly: 60, Category: "general", Arch: "x86"},
		{Name: "big", VCPU: 8, MemoryGiB: 32, Monthly: 50, Category: "compute", Arch: "x86"},
	}
	if s := cheapestServer(servers, 4, 16); s == nil || s.Name != "big" {
		t.Errorf("got %+v, want big (cheapest x86 non-burstable that fits)", s)
	}
	if s := cheapestServer(servers, 64, 256); s != nil {
		t.Errorf("nothing fits, got %+v", s)
	}
}
