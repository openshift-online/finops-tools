package extendedsupport

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
)

func TestElastiCacheFallsBackToUsableHealthLifecycle(t *testing.T) {
	now := time.Date(2032, 6, 1, 0, 0, 0, 0, time.UTC)
	standardEnd, extendedEnd := now.AddDate(-1, 0, 0), now.AddDate(1, 0, 0)
	valid := func(standard, extended time.Time) []healthtypes.LifecycleEvent {
		return []healthtypes.LifecycleEvent{
			healthEvent("END_OF_STANDARD_SUPPORT", standard, "all"),
			healthEvent("END_OF_EXTENDED_SUPPORT", extended, "all"),
		}
	}
	usable := valid(standardEnd, extendedEnd)
	ambiguous := []healthtypes.LifecycleEvent{healthEvent("END_OF_SUPPORT", standardEnd, "all")}
	for _, tc := range []struct {
		name                string
		exact, minor, major []healthtypes.LifecycleEvent
		wantRecord          bool
		wantStandard        time.Time
		wantWarning         []string
	}{
		{
			name: "usable exact version takes precedence", exact: valid(standardEnd.AddDate(0, -1, 0), extendedEnd), minor: usable, major: usable,
			wantRecord: true, wantStandard: standardEnd.AddDate(0, -1, 0),
		},
		{
			name: "absent exact version uses minor", minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "empty exact entry uses minor", exact: []healthtypes.LifecycleEvent{}, minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name:  "events for another region use minor",
			exact: []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", standardEnd, "us-west-2"), healthEvent("END_OF_EXTENDED_SUPPORT", extendedEnd, "us-west-2")}, minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "ambiguous exact entry uses minor", exact: ambiguous, minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "unrecognized milestone uses minor", exact: []healthtypes.LifecycleEvent{healthEvent("UNKNOWN_MILESTONE", standardEnd, "all")}, minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "missing standard date uses minor", exact: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("END_OF_STANDARD_SUPPORT"), Regions: []string{"all"}}}, minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "missing extended end uses minor", exact: []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", standardEnd, "all")}, minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "inconsistent dates use minor", exact: valid(extendedEnd, standardEnd), minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name:  "conflicting milestones use minor",
			exact: append(valid(standardEnd, extendedEnd), healthEvent("END_OF_STANDARD_SUPPORT", standardEnd.AddDate(0, 1, 0), "all")), minor: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "unusable exact and minor entries use major", exact: ambiguous, minor: []healthtypes.LifecycleEvent{}, major: usable,
			wantRecord: true, wantStandard: standardEnd,
		},
		{
			name: "all entries unusable retain failure details", exact: ambiguous, minor: valid(extendedEnd, standardEnd), major: []healthtypes.LifecycleEvent{},
			wantWarning: []string{"5.0.6", "5.0", "version 5 (", "missing or ambiguous", "inconsistent"},
		},
		{
			name: "missing entries stay unknown", wantWarning: []string{"lifecycle information", "5.0.6", "missing"},
		},
		{
			name: "standard support needs no extended end", exact: []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", extendedEnd, "all")}, minor: usable,
		},
		{
			name:  "before explicit extended start needs no extended end",
			exact: []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", standardEnd, "all"), healthEvent("START_OF_EXTENDED_SUPPORT", extendedEnd, "all")}, minor: usable,
		},
		{
			name: "past extended support does not use broader schedule", exact: valid(standardEnd.AddDate(-2, 0, 0), standardEnd), minor: usable,
			wantWarning: []string{"past extended support"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &HealthLifecycleCatalog{entries: make(map[healthVersionKey][]healthtypes.LifecycleEvent)}
			for version, events := range map[string][]healthtypes.LifecycleEvent{"5.0.6": tc.exact, "5.0": tc.minor, "5": tc.major} {
				if events != nil {
					catalog.entries[healthVersionKey{service: ServiceElastiCache, engine: "redis", version: version}] = events
				}
			}
			record, warning := classifyElastiCache(elastiCacheResource{
				id: "cache", engine: "redis", version: "5.0.6", resourceType: ResourceTypeElastiCacheCluster,
			}, "111111111111", "us-east-1", now, catalog)
			if (record != nil) != tc.wantRecord {
				t.Fatalf("record = %+v, warning = %+v; want record = %v", record, warning, tc.wantRecord)
			}
			if record != nil && (!record.StandardSupportEndDate.Equal(tc.wantStandard) || record.EngineVersion != "5.0.6") {
				t.Fatalf("record = %+v; want support end = %s and original engine version", record, tc.wantStandard)
			}
			if len(tc.wantWarning) == 0 {
				if warning != nil {
					t.Fatal(warning)
				}
				return
			}
			if warning == nil {
				t.Fatalf("expected warning containing %q", tc.wantWarning)
			}
			for _, want := range tc.wantWarning {
				if !strings.Contains(warning.Message, want) {
					t.Errorf("warning = %q; want %q", warning.Message, want)
				}
			}
		})
	}
}
