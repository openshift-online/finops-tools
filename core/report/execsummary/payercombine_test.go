package execsummary

import "testing"

func TestCombinePercentageAcrossPayers_SimpleMean(t *testing.T) {
	payer1 := []SPMonthlyMetric{{Month: "2026-05", Percentage: 60}}
	payer2 := []SPMonthlyMetric{{Month: "2026-05", Percentage: 80}}
	payer3 := []SPMonthlyMetric{{Month: "2026-05", Percentage: 100}}

	got := CombinePercentageAcrossPayers([][]SPMonthlyMetric{payer1, payer2, payer3})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Percentage != 80 {
		t.Errorf("Percentage = %v, want 80 (simple mean of 60/80/100)", got[0].Percentage)
	}
}

func TestCombinePercentageAcrossPayers_ExcludesMissingMonthFromAverage(t *testing.T) {
	payer1 := []SPMonthlyMetric{{Month: "2026-05", Percentage: 60}, {Month: "2026-06", Percentage: 50}}
	payer2 := []SPMonthlyMetric{{Month: "2026-05", Percentage: 80}} // no June data

	got := CombinePercentageAcrossPayers([][]SPMonthlyMetric{payer1, payer2})
	byMonth := map[string]float64{}
	for _, m := range got {
		byMonth[m.Month] = m.Percentage
	}
	if byMonth["2026-05"] != 70 {
		t.Errorf("2026-05 = %v, want 70 (mean of 60,80)", byMonth["2026-05"])
	}
	if byMonth["2026-06"] != 50 {
		t.Errorf("2026-06 = %v, want 50 (only payer1 has June data)", byMonth["2026-06"])
	}
}

func TestCombinePercentageAcrossPayers_SortedByMonth(t *testing.T) {
	payer1 := []SPMonthlyMetric{{Month: "2026-06", Percentage: 10}, {Month: "2026-05", Percentage: 20}}
	got := CombinePercentageAcrossPayers([][]SPMonthlyMetric{payer1})
	if len(got) != 2 || got[0].Month != "2026-05" || got[1].Month != "2026-06" {
		t.Errorf("got = %+v, want sorted ascending by month", got)
	}
}

func TestCombinePercentageAcrossPayers_Empty(t *testing.T) {
	got := CombinePercentageAcrossPayers(nil)
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty", got)
	}
}

func TestCombineSavingsAcrossPayers_SumsPerMonth(t *testing.T) {
	payer1 := []SPMonthlySavings{{Month: "2026-05", NetSavings: 100, OnDemandCostEquivalent: 500}}
	payer2 := []SPMonthlySavings{{Month: "2026-05", NetSavings: 50, OnDemandCostEquivalent: 200}}

	got := CombineSavingsAcrossPayers([][]SPMonthlySavings{payer1, payer2})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].NetSavings != 150 {
		t.Errorf("NetSavings = %v, want 150", got[0].NetSavings)
	}
	if got[0].OnDemandCostEquivalent != 700 {
		t.Errorf("OnDemandCostEquivalent = %v, want 700", got[0].OnDemandCostEquivalent)
	}
}

func TestCombineSavingsAcrossPayers_MissingMonthContributesZero(t *testing.T) {
	payer1 := []SPMonthlySavings{{Month: "2026-05", NetSavings: 100, OnDemandCostEquivalent: 500}}
	payer2 := []SPMonthlySavings{{Month: "2026-06", NetSavings: 10, OnDemandCostEquivalent: 20}}

	got := CombineSavingsAcrossPayers([][]SPMonthlySavings{payer1, payer2})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Month != "2026-05" || got[0].NetSavings != 100 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Month != "2026-06" || got[1].NetSavings != 10 {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestTotalSavings(t *testing.T) {
	savings := []SPMonthlySavings{
		{Month: "2026-04", NetSavings: 50, OnDemandCostEquivalent: 200},
		{Month: "2026-05", NetSavings: 30, OnDemandCostEquivalent: 100},
	}
	netSavings, onDemand := TotalSavings(savings)
	if netSavings != 80 || onDemand != 300 {
		t.Errorf("TotalSavings = %v/%v, want 80/300", netSavings, onDemand)
	}
}

func TestTotalSavings_Empty(t *testing.T) {
	netSavings, onDemand := TotalSavings(nil)
	if netSavings != 0 || onDemand != 0 {
		t.Errorf("TotalSavings(nil) = %v/%v, want 0/0", netSavings, onDemand)
	}
}

func TestAveragePercentage(t *testing.T) {
	metrics := []SPMonthlyMetric{{Percentage: 60}, {Percentage: 80}, {Percentage: 100}}
	got := AveragePercentage(metrics)
	if got == nil || *got != 80 {
		t.Errorf("AveragePercentage = %v, want 80", got)
	}
}

func TestAveragePercentage_Empty(t *testing.T) {
	if got := AveragePercentage(nil); got != nil {
		t.Errorf("AveragePercentage(nil) = %v, want nil", *got)
	}
}
