package format

import "testing"

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		amount float64
		want   string
	}{
		{0, "0.00"},
		{1234.5, "1,234.50"},
		{12345678.9, "12,345,678.90"},
		{-99.1, "-99.10"},
	}
	for _, tc := range tests {
		if got := FormatAmount(tc.amount); got != tc.want {
			t.Errorf("FormatAmount(%v) = %q, want %q", tc.amount, got, tc.want)
		}
	}
}

func TestFormatAmountWhole(t *testing.T) {
	if got := FormatAmountWhole(125000); got != "125,000" {
		t.Errorf("FormatAmountWhole = %q, want 125,000", got)
	}
}

func TestFormatMoney(t *testing.T) {
	if got := FormatMoney(1000, "USD"); got != "USD 1,000.00" {
		t.Errorf("FormatMoney = %q", got)
	}
	if got := FormatMoney(10, ""); got != "USD 10.00" {
		t.Errorf("FormatMoney default currency = %q", got)
	}
}

func TestFormatChartMoney(t *testing.T) {
	if got := FormatChartMoney(7927, "USD"); got != "USD 7,927" {
		t.Errorf("large chart label = %q", got)
	}
	if got := FormatChartMoney(99.5, "USD"); got != "USD 99.50" {
		t.Errorf("small chart label = %q", got)
	}
}

func TestFormatDollarCompact(t *testing.T) {
	tests := []struct {
		amount float64
		want   string
	}{
		{0, "$0"},
		{999, "$999"},
		{1500, "$1,500"}, // non-short mode: full comma format even in the thousands
		{45000, "$45,000"},
		{999999, "$999,999"},
		{1_000_000, "$1.00M"},
		{2_500_000, "$2.50M"},
		{-1500, "-$1,500"},
		{-2_500_000, "-$2.50M"},
	}
	for _, tc := range tests {
		if got := FormatDollarCompact(tc.amount); got != tc.want {
			t.Errorf("FormatDollarCompact(%v) = %q, want %q", tc.amount, got, tc.want)
		}
	}
}

func TestFormatDollarCompactShort(t *testing.T) {
	tests := []struct {
		amount float64
		want   string
	}{
		{0, "$0"},
		{999, "$999"},
		{1500, "$1.5K"},
		{45000, "$45.0K"},
		{1_000_000, "$1.00M"},
		{2_500_000, "$2.50M"},
		{-1500, "-$1.5K"},
		{-45000, "-$45.0K"},
	}
	for _, tc := range tests {
		if got := FormatDollarCompactShort(tc.amount); got != tc.want {
			t.Errorf("FormatDollarCompactShort(%v) = %q, want %q", tc.amount, got, tc.want)
		}
	}
}
