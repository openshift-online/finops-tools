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
			wantRecord: true, wantStandard: standardEnd.AddDate(-2, 0, 0),
		},
		{
			name:  "supported exact version takes precedence over broader schedule",
			exact: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}}}, minor: usable,
		},
		{
			name:  "unusable exact version falls back to supported minor",
			exact: ambiguous, minor: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}}}, major: usable,
		},
		{
			name:  "unusable exact and minor fall back to supported major",
			exact: ambiguous, minor: []healthtypes.LifecycleEvent{}, major: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}}},
		},
		{
			name:  "past exact version takes precedence over supported minor",
			exact: valid(standardEnd.AddDate(-2, 0, 0), standardEnd), minor: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}}},
			wantRecord: true, wantStandard: standardEnd.AddDate(-2, 0, 0),
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
			if record != nil {
				result := buildResult([]Record{*record}, nil, nil, nil, now)
				past := !now.Before(*record.ExtendedSupportEndDate)
				if (len(result.Summary.PastExtendedSupportResources) == 1) != past || result.Summary.TotalCount != len(result.Records) {
					t.Fatalf("classification = %+v", result)
				}
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

func TestHealthSupportedOnlyClassification(t *testing.T) {
	now := time.Date(2032, 6, 1, 12, 0, 0, 0, time.UTC)
	supported := healthtypes.LifecycleEvent{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}}
	standard := healthEvent("END_OF_STANDARD_SUPPORT", now.AddDate(-1, 0, 0), "all")
	for _, tc := range []struct {
		name    string
		events  []healthtypes.LifecycleEvent
		current bool
		past    bool
		unknown bool
	}{
		{name: "supported without a date", events: []healthtypes.LifecycleEvent{supported}},
		{name: "supported with a date and uppercase type", events: []healthtypes.LifecycleEvent{healthEvent("SUPPORTED", now.AddDate(-1, 0, 0), "all")}},
		{name: "duplicate supported events", events: []healthtypes.LifecycleEvent{supported, supported}},
		{name: "regional supported event", events: []healthtypes.LifecycleEvent{healthEvent("supported", now, "US-EAST-1")}},
		{name: "supported only in another region", events: []healthtypes.LifecycleEvent{healthEvent("supported", now, "us-west-2")}, unknown: true},
		{name: "supported without regions", events: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("supported")}}, unknown: true},
		{name: "other region milestones do not change supported status", events: []healthtypes.LifecycleEvent{supported, healthEvent("end_of_life", now, "us-west-2")}},
		{name: "unrecognized applicable event stays unknown", events: []healthtypes.LifecycleEvent{supported, healthEvent("unknown_event", now, "all")}, unknown: true},
		{name: "unrecognized event in another region is ignored", events: []healthtypes.LifecycleEvent{supported, healthEvent("unknown_event", now, "us-west-2")}},
		{name: "supported does not hide incomplete support milestones", events: []healthtypes.LifecycleEvent{supported, standard}, unknown: true},
		{name: "supported does not hide current extended support", events: []healthtypes.LifecycleEvent{supported, standard, healthEvent("END_OF_EXTENDED_SUPPORT", now.AddDate(1, 0, 0), "all")}, current: true},
		{name: "supported does not hide expired extended support", events: []healthtypes.LifecycleEvent{supported, standard, healthEvent("END_OF_EXTENDED_SUPPORT", now, "all")}, past: true},
		{name: "supported does not hide end of life", events: []healthtypes.LifecycleEvent{supported, healthEvent("end_of_life", now, "all")}, past: true},
		{name: "future end of life is not supported only", events: []healthtypes.LifecycleEvent{supported, healthEvent("end_of_life", now.AddDate(1, 0, 0), "all")}, unknown: true},
	} {
		for _, service := range []Service{ServiceElastiCache, ServiceOpenSearch} {
			t.Run(string(service)+"/"+tc.name, func(t *testing.T) {
				engine, version := "redis", "9.1"
				if service == ServiceOpenSearch {
					engine = "opensearch"
				}
				catalog := &HealthLifecycleCatalog{entries: map[healthVersionKey][]healthtypes.LifecycleEvent{
					{service: service, engine: engine, version: version}: tc.events,
				}}
				record, warning := classifyHealthSupport(catalog, ResourceWarning{
					Service: service, Engine: engine, EngineVersion: version, Region: "us-east-1",
				}, now)
				if (warning != nil) != tc.unknown || (record != nil) != (tc.current || tc.past) {
					t.Fatalf("record=%+v warning=%+v", record, warning)
				}
				if record != nil {
					result := buildResult([]Record{*record}, nil, nil, nil, now)
					if (len(result.Records) == 1) != tc.current || (len(result.Summary.PastExtendedSupportResources) == 1) != tc.past {
						t.Fatalf("classification=%+v", result)
					}
				}
			})
		}
	}
}

func TestHealthEndOfLifeClassification(t *testing.T) {
	cutoff := time.Date(2032, 6, 1, 12, 0, 0, 0, time.UTC)
	eol := healthEvent("end_of_life", cutoff, "all")
	standard := healthEvent("END_OF_STANDARD_SUPPORT", cutoff.AddDate(-1, 0, 0), "all")
	for _, tc := range []struct {
		name    string
		events  []healthtypes.LifecycleEvent
		at      time.Time
		current bool
		past    bool
		unknown bool
		broader bool
	}{
		{name: "end of life alone at boundary", events: []healthtypes.LifecycleEvent{eol}, at: cutoff, past: true},
		{name: "end of life alone after boundary", events: []healthtypes.LifecycleEvent{eol}, at: cutoff.Add(time.Second), past: true},
		{name: "future end of life alone cannot establish current support", events: []healthtypes.LifecycleEvent{eol}, at: cutoff.Add(-time.Nanosecond), unknown: true},
		{name: "standard end and future end of life", events: []healthtypes.LifecycleEvent{standard, eol}, at: cutoff.Add(-time.Nanosecond), current: true},
		{name: "independent extended end and end of life", events: []healthtypes.LifecycleEvent{standard, healthEvent("END_OF_EXTENDED_SUPPORT", cutoff.Add(-time.Hour), "all"), eol}, at: cutoff, past: true},
		{name: "terminal end of life overrides later support dates", events: []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", cutoff.Add(time.Hour), "all"), eol}, at: cutoff, past: true},
		{name: "terminal end of life survives missing support date", events: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("END_OF_STANDARD_SUPPORT"), Regions: []string{"all"}}, eol}, at: cutoff, past: true},
		{name: "terminal end of life survives conflicting support dates", events: []healthtypes.LifecycleEvent{standard, healthEvent("END_OF_STANDARD_SUPPORT", cutoff.Add(-time.Hour), "all"), eol}, at: cutoff, past: true},
		{name: "terminal end of life survives unusable support regions", events: []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", cutoff.Add(-time.Hour)), eol}, at: cutoff, past: true},
		{name: "end of life date missing", events: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("end_of_life"), Regions: []string{"all"}}}, at: cutoff, unknown: true},
		{name: "end of life date zero", events: []healthtypes.LifecycleEvent{healthEvent("end_of_life", time.Time{}, "all")}, at: cutoff, unknown: true},
		{name: "end of life dates conflict", events: []healthtypes.LifecycleEvent{eol, healthEvent("end_of_life", cutoff.Add(-time.Hour), "all")}, at: cutoff, unknown: true},
		{name: "end of life regions missing", events: []healthtypes.LifecycleEvent{healthEvent("end_of_life", cutoff)}, at: cutoff, unknown: true},
		{name: "end of life applies elsewhere", events: []healthtypes.LifecycleEvent{healthEvent("end_of_life", cutoff, "us-west-2")}, at: cutoff, unknown: true},
		{name: "future regional event overrides reached global event", events: []healthtypes.LifecycleEvent{eol, healthEvent("end_of_life", cutoff.Add(time.Hour), "us-east-1")}, at: cutoff, unknown: true},
		{name: "reached regional event overrides future global event", events: []healthtypes.LifecycleEvent{healthEvent("end_of_life", cutoff.Add(time.Hour), "all"), healthEvent("end_of_life", cutoff, "us-east-1")}, at: cutoff, past: true},
		{name: "exact end of life takes precedence over broader support", events: []healthtypes.LifecycleEvent{eol}, at: cutoff, past: true, broader: true},
	} {
		for _, service := range []Service{ServiceElastiCache, ServiceOpenSearch} {
			t.Run(string(service)+"/"+tc.name, func(t *testing.T) {
				engine, version := "redis", "5.0.6"
				if service == ServiceOpenSearch {
					engine, version = "opensearch", "1.2"
				}
				catalog := &HealthLifecycleCatalog{entries: map[healthVersionKey][]healthtypes.LifecycleEvent{
					{service: service, engine: engine, version: version}: tc.events,
				}}
				if tc.broader {
					catalog.entries[healthVersionKey{service: service, engine: engine, version: "5"}] = []healthtypes.LifecycleEvent{
						standard, healthEvent("END_OF_EXTENDED_SUPPORT", cutoff.AddDate(1, 0, 0), "all"),
					}
				}
				var record *Record
				var warning *ResourceWarning
				if service == ServiceElastiCache {
					record, warning = classifyElastiCache(elastiCacheResource{id: "cache", engine: engine, version: version, resourceType: ResourceTypeElastiCacheCluster}, "111111111111", "us-east-1", tc.at, catalog)
				} else {
					record, warning = classifyOpenSearch("111111111111", "us-east-1", "domain", "OpenSearch_"+version, tc.at, catalog)
				}
				if (warning != nil) != tc.unknown || (record != nil) != (tc.current || tc.past) {
					t.Fatalf("record=%+v warning=%+v", record, warning)
				}
				if record == nil {
					return
				}
				if record.EndOfLifeDate == nil || !record.EndOfLifeDate.Equal(cutoff) {
					t.Fatalf("end of life date = %+v", record)
				}
				result := buildResult([]Record{*record}, nil, nil, nil, tc.at)
				if (len(result.Records) == 1) != tc.current || (len(result.Summary.PastExtendedSupportResources) == 1) != tc.past || result.Summary.TotalCount != len(result.Records) || len(result.Summary.UnclassifiedResources) != 0 {
					t.Fatalf("classification = %+v", result)
				}
			})
		}
	}
}
