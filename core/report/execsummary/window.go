package execsummary

import (
	"fmt"
	"time"
)

// MonthLabel formats t as a YYYY-MM month key (e.g. "2026-05").
func MonthLabel(t time.Time) string {
	return t.Format("2006-01")
}

// MonthName formats t as an abbreviated month + year (e.g. "May 2026").
func MonthName(t time.Time) string {
	return t.Format("Jan 2006")
}

// MonthsInWindow returns the first day of each calendar month from start up to
// and including end. Both are normalised to the first day of their month.
func MonthsInWindow(start, end time.Time) []time.Time {
	cur := firstOfMonth(start)
	endNorm := firstOfMonth(end)

	var months []time.Time
	for !cur.After(endNorm) {
		months = append(months, cur)
		cur = nextMonth(cur)
	}
	return months
}

// ComputeWindow returns a TimeWindow of nMonths ending at the most recently
// completed month relative to today. Pass a non-nil today to override time.Now
// (useful in tests).
//
// Example: nMonths=3, today=2026-06-15 → window Jan–Mar 2026 is already past,
// but "most recently completed" means May 2026. The window is Mar–May 2026.
func ComputeWindow(nMonths int, today *time.Time) (TimeWindow, error) {
	if nMonths < 1 {
		return TimeWindow{}, fmt.Errorf("nMonths must be >= 1, got %d", nMonths)
	}

	ref := time.Now().UTC()
	if today != nil {
		ref = today.UTC()
	}

	// The most recently completed month is the month before the current month.
	end := firstOfMonth(ref).AddDate(0, -1, 0)
	// Start is (nMonths-1) months before end.
	start := end.AddDate(0, -(nMonths - 1), 0)

	months := MonthsInWindow(start, end)
	return TimeWindow{
		Start:  start,
		End:    end,
		Months: months,
	}, nil
}

// firstOfMonth returns midnight UTC on the first day of t's month.
func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// nextMonth returns the first day of the month following t.
func nextMonth(t time.Time) time.Time {
	return firstOfMonth(t).AddDate(0, 1, 0)
}
