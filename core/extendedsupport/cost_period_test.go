package extendedsupport

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestCostBucketDatesAcceptCalendarDatesAndUTCMidnightTimestamps(t *testing.T) {
	requested := cost.DateRange{
		Start: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC),
	}
	for _, tc := range []struct {
		name, start, end, wantStart, wantEnd string
	}{
		{"date only", "2026-10-27", "2026-10-28", "2026-10-27", "2026-10-27"},
		{"reported AWS timestamp format", "2026-10-27T00:00:00Z", "2026-10-28T00:00:00Z", "2026-10-27", "2026-10-27"},
		{"mixed formats", "2026-10-27", "2026-10-28T00:00:00Z", "2026-10-27", "2026-10-27"},
		{"zero fractional seconds", "2026-10-27T00:00:00.000Z", "2026-10-28T00:00:00.000000000Z", "2026-10-27", "2026-10-27"},
		{"equivalent UTC boundaries", "2026-10-26T20:00:00-04:00", "2026-10-28T00:00:00+00:00", "2026-10-27", "2026-10-27"},
		{"whole requested interval", "2026-10-27T00:00:00Z", "2026-11-06T00:00:00Z", "2026-10-27", "2026-11-05"},
		{"last included day", "2026-11-05T00:00:00Z", "2026-11-06T00:00:00Z", "2026-11-05", "2026-11-05"},
		{"before requested range", "2026-10-26T00:00:00Z", "2026-10-28T00:00:00Z", "", ""},
		{"after requested range", "2026-11-05T00:00:00Z", "2026-11-07T00:00:00Z", "", ""},
		{"partial start day", "2026-10-27T01:00:00Z", "2026-10-28T00:00:00Z", "", ""},
		{"partial end day", "2026-10-27T00:00:00Z", "2026-10-28T00:00:00.000000001Z", "", ""},
		{"local midnight is not UTC midnight", "2026-10-27T00:00:00-04:00", "2026-10-28T00:00:00Z", "", ""},
		{"empty interval", "2026-10-27T00:00:00Z", "2026-10-27T00:00:00Z", "", ""},
		{"reversed interval", "2026-10-28", "2026-10-27T00:00:00Z", "", ""},
		{"missing start", "", "2026-10-28T00:00:00Z", "", ""},
		{"invalid date", "2026-10-32T00:00:00Z", "2026-11-01", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := costBucketDates(&cetypes.DateInterval{Start: aws.String(tc.start), End: aws.String(tc.end)}, requested)
			if tc.wantStart == "" {
				if err == nil {
					t.Fatalf("invalid bucket accepted: %s to %s", start, end)
				}
				return
			}
			if err != nil || start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("bucket=%s to %s err=%v, want %s to %s", start, end, err, tc.wantStart, tc.wantEnd)
			}
		})
	}
	if _, _, err := costBucketDates(nil, requested); err == nil {
		t.Fatal("missing billing interval was accepted")
	}
}

func TestSupportCostGranularityDefaultsMonthly(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"", "monthly"}, {"monthly", "monthly"}, {"DAILY", "daily"}, {" daily ", "daily"}} {
		got, err := ParseCostGranularity(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("granularity(%q)=%q err=%v", tc.input, got, err)
		}
	}
}
