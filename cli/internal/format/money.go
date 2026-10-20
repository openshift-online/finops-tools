// Package format provides shared display formatting helpers for the CLI.
package format

import (
	"fmt"
	"strings"
)

// FormatAmount formats a monetary amount with thousands separators and two decimal places.
// Example: 12345678.9 -> "12,345,678.90"
func FormatAmount(amount float64) string {
	return formatFixed(amount, 2)
}

// FormatAmountWhole formats a monetary amount with thousands separators and no fractional part.
// Example: 125000 -> "125,000"
func FormatAmountWhole(amount float64) string {
	return formatFixed(amount, 0)
}

// FormatMoney formats currency code and amount for human-readable display.
// Example: FormatMoney(1234.5, "USD") -> "USD 1,234.50"
func FormatMoney(amount float64, currency string) string {
	cur := strings.TrimSpace(currency)
	if cur == "" {
		cur = "USD"
	}
	return fmt.Sprintf("%s %s", cur, FormatAmount(amount))
}

// FormatChartMoney formats currency and amount for chart axis labels (whole numbers when >= 1000).
func FormatChartMoney(amount float64, currency string) string {
	cur := strings.TrimSpace(currency)
	if cur == "" {
		cur = "USD"
	}
	if amount >= 1000 {
		return fmt.Sprintf("%s %s", cur, FormatAmountWhole(amount))
	}
	return fmt.Sprintf("%s %s", cur, FormatAmount(amount))
}

// FormatDollarCompact implements the HCP Executive Summary report's dollar
// formatting rule (golang-executive-summary-spec.md section 7.1's
// _fmt_dollar, in its default/non-"short" mode): values >= $1,000,000
// render as "$X.XXM"; otherwise "$X,XXX" (comma-grouped, no decimals).
func FormatDollarCompact(amount float64) string {
	return formatDollarSpec(amount, false)
}

// FormatDollarCompactShort implements the same spec 7.1 rule with the
// "short" flag set (used for compact sub-text deltas, e.g. KPI card
// sub-lines): values >= $1,000,000 render as "$X.XXM"; values >= $1,000
// render as "$X.XK" (one decimal); otherwise "$X,XXX".
func FormatDollarCompactShort(amount float64) string {
	return formatDollarSpec(amount, true)
}

func formatDollarSpec(amount float64, short bool) string {
	sign := ""
	abs := amount
	if amount < 0 {
		sign = "-"
		abs = -amount
	}
	switch {
	case abs >= 1_000_000:
		return fmt.Sprintf("%s$%.2fM", sign, abs/1_000_000)
	case short && abs >= 1_000:
		return fmt.Sprintf("%s$%.1fK", sign, abs/1_000)
	default:
		return fmt.Sprintf("%s$%s", sign, FormatAmountWhole(abs))
	}
}

func formatFixed(amount float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, amount)
	if decimals == 0 {
		return addThousandsSeparators(s)
	}
	parts := strings.SplitN(s, ".", 2)
	intPart := addThousandsSeparators(parts[0])
	if len(parts) < 2 {
		return intPart + ".00"
	}
	return intPart + "." + parts[1]
}

func addThousandsSeparators(intPart string) string {
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
