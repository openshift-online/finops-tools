package extendedsupport

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

func dateOnlyUTC(value time.Time) time.Time {
	y, m, d := value.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Cost Explorer response buckets can use date-only strings or RFC3339
// timestamps. Preserve the actual boundary: daily/monthly buckets must fall on
// UTC midnight, so never silently truncate a partial day into a calendar date.
// This is separate from cost.ParseDate, which validates CLI date inputs.
func parseCostBucketDate(value string) (time.Time, error) {
	if date, err := cost.ParseDate(value); err == nil {
		return date, nil
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	instant = instant.UTC()
	if !instant.Equal(dateOnlyUTC(instant)) {
		return time.Time{}, fmt.Errorf("billing bucket boundary %q is not UTC midnight", value)
	}
	return instant, nil
}

func costBucketDates(interval *cetypes.DateInterval, requested cost.DateRange) (string, string, error) {
	if interval == nil {
		return "", "", fmt.Errorf("billing response has no time period")
	}
	start, startErr := parseCostBucketDate(aws.ToString(interval.Start))
	end, endErr := parseCostBucketDate(aws.ToString(interval.End))
	if startErr != nil || endErr != nil || !end.After(start) || start.Before(requested.Start) || end.After(requested.End) {
		return "", "", fmt.Errorf("billing response has an invalid or out-of-range time period: returned %q to %q; requested %s to %s (end exclusive)",
			aws.ToString(interval.Start), aws.ToString(interval.End), cost.FormatDate(requested.Start), cost.FormatDate(requested.End))
	}
	return cost.FormatDate(start), cost.FormatDate(end.AddDate(0, 0, -1)), nil
}
