package execsummary

import (
	"testing"
	"time"
)

func TestMonthLabel(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2026-01"},
		{time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC), "2026-12"},
		{time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC), "2025-06"},
	}
	for _, c := range cases {
		if got := MonthLabel(c.in); got != c.want {
			t.Errorf("MonthLabel(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMonthName(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "Jan 2026"},
		{time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), "Dec 2026"},
		{time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC), "Jun 2025"},
	}
	for _, c := range cases {
		if got := MonthName(c.in); got != c.want {
			t.Errorf("MonthName(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMonthsInWindow(t *testing.T) {
	t.Run("three months", func(t *testing.T) {
		start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		months := MonthsInWindow(start, end)
		if len(months) != 3 {
			t.Fatalf("want 3 months, got %d: %v", len(months), months)
		}
		wantLabels := []string{"2026-03", "2026-04", "2026-05"}
		for i, m := range months {
			if got := MonthLabel(m); got != wantLabels[i] {
				t.Errorf("months[%d] = %q, want %q", i, got, wantLabels[i])
			}
		}
	})

	t.Run("single month", func(t *testing.T) {
		d := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		months := MonthsInWindow(d, d)
		if len(months) != 1 {
			t.Fatalf("want 1 month, got %d", len(months))
		}
		if got := MonthLabel(months[0]); got != "2026-06" {
			t.Errorf("got %q, want 2026-06", got)
		}
	})

	t.Run("normalises non-first-day inputs", func(t *testing.T) {
		start := time.Date(2026, 3, 15, 12, 30, 0, 0, time.UTC)
		end := time.Date(2026, 5, 31, 23, 59, 59, 0, time.UTC)
		months := MonthsInWindow(start, end)
		if len(months) != 3 {
			t.Fatalf("want 3 months, got %d", len(months))
		}
	})

	t.Run("year boundary", func(t *testing.T) {
		start := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		months := MonthsInWindow(start, end)
		if len(months) != 3 {
			t.Fatalf("want 3 months, got %d", len(months))
		}
		wantLabels := []string{"2025-11", "2025-12", "2026-01"}
		for i, m := range months {
			if got := MonthLabel(m); got != wantLabels[i] {
				t.Errorf("months[%d] = %q, want %q", i, got, wantLabels[i])
			}
		}
	})
}

func TestComputeWindow(t *testing.T) {
	t.Run("error on zero months", func(t *testing.T) {
		if _, err := ComputeWindow(0, nil); err == nil {
			t.Fatal("expected error for nMonths=0")
		}
	})

	t.Run("error on negative months", func(t *testing.T) {
		if _, err := ComputeWindow(-1, nil); err == nil {
			t.Fatal("expected error for nMonths=-1")
		}
	})

	t.Run("3 months ending at last completed month", func(t *testing.T) {
		// today = 2026-06-15; last completed month = May 2026
		// 3-month window: Mar, Apr, May 2026
		today := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		w, err := ComputeWindow(3, &today)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if MonthLabel(w.End) != "2026-05" {
			t.Errorf("End = %q, want 2026-05", MonthLabel(w.End))
		}
		if MonthLabel(w.Start) != "2026-03" {
			t.Errorf("Start = %q, want 2026-03", MonthLabel(w.Start))
		}
		if len(w.Months) != 3 {
			t.Errorf("len(Months) = %d, want 3", len(w.Months))
		}
	})

	t.Run("1 month window", func(t *testing.T) {
		today := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		w, err := ComputeWindow(1, &today)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(w.Months) != 1 {
			t.Fatalf("want 1 month, got %d", len(w.Months))
		}
		if MonthLabel(w.Start) != MonthLabel(w.End) {
			t.Errorf("start %q != end %q for 1-month window", MonthLabel(w.Start), MonthLabel(w.End))
		}
	})

	t.Run("year boundary window", func(t *testing.T) {
		// today = 2026-02-10; last completed = Jan 2026; 3 months = Nov, Dec 2025, Jan 2026
		today := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
		w, err := ComputeWindow(3, &today)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if MonthLabel(w.Start) != "2025-11" {
			t.Errorf("Start = %q, want 2025-11", MonthLabel(w.Start))
		}
		if MonthLabel(w.End) != "2026-01" {
			t.Errorf("End = %q, want 2026-01", MonthLabel(w.End))
		}
		if len(w.Months) != 3 {
			t.Errorf("len(Months) = %d, want 3", len(w.Months))
		}
	})
}
