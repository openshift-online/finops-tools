package execsummary

import "testing"

func TestMoMPct(t *testing.T) {
	cases := []struct {
		name             string
		last, prev, want float64
	}{
		{"increase", 110, 100, 10},
		{"decrease", 90, 100, -10},
		{"zero prev", 100, 0, 0},
		{"negative prev", 100, -5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MoMPct(c.last, c.prev); got != c.want {
				t.Errorf("MoMPct(%v,%v) = %v, want %v", c.last, c.prev, got, c.want)
			}
		})
	}
}

func TestMoMClass(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{6, "bad"},
		{5.1, "bad"},
		{5, "warn"}, // exactly 5 is NOT > 5, but IS > 0
		{1, "warn"},
		{0.1, "warn"},
		{0, "good"},
		{-5, "good"},
	}
	for _, c := range cases {
		if got := MoMClass(c.pct); got != c.want {
			t.Errorf("MoMClass(%v) = %q, want %q", c.pct, got, c.want)
		}
	}
}

func TestSPCoverageClass(t *testing.T) {
	if got := SPCoverageClass(70); got != "good" {
		t.Errorf("SPCoverageClass(70) = %q, want good", got)
	}
	if got := SPCoverageClass(69.9); got != "warn" {
		t.Errorf("SPCoverageClass(69.9) = %q, want warn", got)
	}
}

func TestSPUtilizationClass(t *testing.T) {
	if got := SPUtilizationClass(90); got != "good" {
		t.Errorf("SPUtilizationClass(90) = %q, want good", got)
	}
	if got := SPUtilizationClass(89.9); got != "warn" {
		t.Errorf("SPUtilizationClass(89.9) = %q, want warn", got)
	}
}

func TestSavingsRate(t *testing.T) {
	got := SavingsRate(20, 100)
	if got == nil || *got != 20 {
		t.Errorf("SavingsRate(20,100) = %v, want 20", got)
	}
	if got := SavingsRate(20, 0); got != nil {
		t.Errorf("SavingsRate(20,0) = %v, want nil", *got)
	}
}

func TestSavingsRateClass(t *testing.T) {
	if got := SavingsRateClass(20); got != "good" {
		t.Errorf("SavingsRateClass(20) = %q, want good", got)
	}
	if got := SavingsRateClass(19.9); got != "warn" {
		t.Errorf("SavingsRateClass(19.9) = %q, want warn", got)
	}
}

func TestAnomalyClass(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{6, "bad"},
		{1, "warn"},
		{0, "good"},
	}
	for _, c := range cases {
		if got := AnomalyClass(c.count); got != c.want {
			t.Errorf("AnomalyClass(%d) = %q, want %q", c.count, got, c.want)
		}
	}
}

func TestComputePayerKPIs_FullAssembly(t *testing.T) {
	hcpAccounts := map[string]bool{"111111111111": true}
	in := PayerKPIInputs{
		PayerLabel: "All Payers",
		CostRecords: []CostRecord{
			{Month: "2026-05", AccountID: "111111111111", Cost: 1000},
			{Month: "2026-05", AccountID: "222222222222", Cost: 500},
			{Month: "2026-04", AccountID: "111111111111", Cost: 800},
			{Month: "2026-04", AccountID: "222222222222", Cost: 700},
		},
		LastMonth:                "2026-05",
		PrevMonth:                "2026-04",
		HCPAccountIDs:            hcpAccounts,
		ClusterCumulativeByMonth: map[string]int{"2026-04": 90, "2026-05": 100},
		SPCoverageAvg:            floatPtr(80),
		SPUtilizationAvg:         floatPtr(95),
		TotalNetSavings:          300,
		TotalOnDemandEquiv:       1000,
		AnomalyCount:             2,
	}

	got := ComputePayerKPIs(in)

	if got.TotalCostLastMonth != 1500 || got.TotalCostPrevMonth != 1500 {
		t.Errorf("totals = %v/%v, want 1500/1500", got.TotalCostLastMonth, got.TotalCostPrevMonth)
	}
	if got.MoMPct != 0 {
		t.Errorf("MoMPct = %v, want 0 (flat)", got.MoMPct)
	}
	if got.MoMClass != "good" {
		t.Errorf("MoMClass = %q, want good", got.MoMClass)
	}
	if got.ClusterCount != 100 {
		t.Errorf("ClusterCount = %d, want 100", got.ClusterCount)
	}
	if got.ClusterDelta == nil || *got.ClusterDelta != 10 {
		t.Errorf("ClusterDelta = %v, want 10", got.ClusterDelta)
	}
	if got.HCPCost != 1000 {
		t.Errorf("HCPCost = %v, want 1000 (only account 111111111111)", got.HCPCost)
	}
	if got.HCPUnitCost == nil || *got.HCPUnitCost != 10 {
		t.Errorf("HCPUnitCost = %v, want 10 (1000/100)", got.HCPUnitCost)
	}
	if got.SPCoverageClass != "good" {
		t.Errorf("SPCoverageClass = %q, want good (80 >= 70)", got.SPCoverageClass)
	}
	if got.SPUtilizationClass != "good" {
		t.Errorf("SPUtilizationClass = %q, want good (95 >= 90)", got.SPUtilizationClass)
	}
	if got.SavingsRate == nil || *got.SavingsRate != 30 {
		t.Errorf("SavingsRate = %v, want 30 (300/1000*100)", got.SavingsRate)
	}
	if got.SavingsRateClass != "good" {
		t.Errorf("SavingsRateClass = %q, want good (30 >= 20)", got.SavingsRateClass)
	}
	if got.AnomalyClass != "warn" {
		t.Errorf("AnomalyClass = %q, want warn", got.AnomalyClass)
	}
}

func TestComputePayerKPIs_ZeroClusterCountLeavesUnitCostNil(t *testing.T) {
	in := PayerKPIInputs{
		CostRecords:              []CostRecord{{Month: "2026-05", AccountID: "111111111111", Cost: 100}},
		LastMonth:                "2026-05",
		PrevMonth:                "2026-04",
		HCPAccountIDs:            map[string]bool{"111111111111": true},
		ClusterCumulativeByMonth: map[string]int{},
	}
	got := ComputePayerKPIs(in)
	if got.HCPUnitCost != nil {
		t.Errorf("HCPUnitCost = %v, want nil when ClusterCount is 0", *got.HCPUnitCost)
	}
	if got.ClusterDelta != nil {
		t.Errorf("ClusterDelta = %v, want nil when no prior snapshot", *got.ClusterDelta)
	}
}

func TestComputePayerKPIs_NilSPAveragesLeaveClassesEmpty(t *testing.T) {
	in := PayerKPIInputs{
		LastMonth:                "2026-05",
		PrevMonth:                "2026-04",
		ClusterCumulativeByMonth: map[string]int{},
	}
	got := ComputePayerKPIs(in)
	if got.SPCoverageClass != "" {
		t.Errorf("SPCoverageClass = %q, want empty when SPCoverageAvg is nil", got.SPCoverageClass)
	}
	if got.SPUtilizationClass != "" {
		t.Errorf("SPUtilizationClass = %q, want empty when SPUtilizationAvg is nil", got.SPUtilizationClass)
	}
	if got.SavingsRateClass != "" {
		t.Errorf("SavingsRateClass = %q, want empty when SavingsRate is nil (TotalOnDemandEquiv 0)", got.SavingsRateClass)
	}
}

func floatPtr(v float64) *float64 { return &v }
