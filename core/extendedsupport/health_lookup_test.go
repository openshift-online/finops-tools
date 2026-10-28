package extendedsupport

import (
	"testing"
	"time"

	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
)

func TestHealthLifecycleLookupConcurrent(t *testing.T) {
	now := time.Date(2032, 6, 1, 0, 0, 0, 0, time.UTC)
	redis := healthVersionKey{service: ServiceElastiCache, engine: "redis", version: "5.0.6"}
	supported := healthVersionKey{service: ServiceElastiCache, engine: "redis", version: "5.0.7"}
	eol := healthVersionKey{service: ServiceElastiCache, engine: "redis", version: "6"}
	missing := healthVersionKey{service: ServiceElastiCache, engine: "valkey", version: "5.0.6"}
	search := healthVersionKey{service: ServiceOpenSearch, engine: "opensearch", version: "1.2"}
	events := []healthtypes.LifecycleEvent{
		healthEvent("standard_support_end", now.Add(-time.Hour), "all"),
		healthEvent("extended_support_end", now.Add(time.Hour), "all"),
		healthEvent("standard_support_end", now.Add(time.Minute), "us-west-2"),
	}
	catalog := &HealthLifecycleCatalog{entries: map[healthVersionKey][]healthtypes.LifecycleEvent{
		{service: ServiceElastiCache, engine: "redis", version: "5"}: events,
		supported: {healthEvent("supported", now, "all")},
		eol:       {healthEvent("end_of_life", now, "all")},
		search:    events,
	}}
	for _, tc := range []struct {
		name                   string
		key                    healthVersionKey
		region                 string
		at                     time.Time
		current, past, unknown bool
	}{
		{name: "fallback version in extended support", key: redis, region: "us-east-1", at: now, current: true},
		{name: "same version with regional dates", key: redis, region: "us-west-2", at: now},
		{name: "case insensitive region", key: redis, region: "US-EAST-1", at: now, current: true},
		{name: "different time zone same instant", key: redis, region: "us-east-1", at: now.In(time.FixedZone("offset", -5*60*60)), current: true},
		{name: "specific supported version", key: supported, region: "us-east-1", at: now},
		{name: "different engine unknown", key: missing, region: "us-east-1", at: now, unknown: true},
		{name: "opensearch version", key: search, region: "us-east-1", at: now, current: true},
		{name: "end of life not reached is unknown", key: eol, region: "us-east-1", at: now.Add(-time.Nanosecond), unknown: true},
		{name: "end of life reached is past support", key: eol, region: "us-east-1", at: now, past: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for range 32 {
				record, warning := classifyHealthSupport(catalog, ResourceWarning{
					Service: tc.key.service, Engine: tc.key.engine, EngineVersion: tc.key.version, Region: tc.region,
				}, tc.at)
				if (record != nil) != (tc.current || tc.past) || (warning != nil) != tc.unknown {
					t.Fatalf("record=%+v warning=%+v", record, warning)
				}
				if record != nil {
					result := buildResult([]Record{*record}, nil, nil, nil, tc.at)
					if (len(result.Records) == 1) != tc.current || (len(result.Summary.PastExtendedSupportResources) == 1) != tc.past {
						t.Fatalf("result=%+v", result)
					}
				}
			}
		})
	}
}

func BenchmarkHealthLifecycleLookup(b *testing.B) {
	now := time.Date(2032, 6, 1, 0, 0, 0, 0, time.UTC)
	catalog := &HealthLifecycleCatalog{entries: map[healthVersionKey][]healthtypes.LifecycleEvent{
		{service: ServiceElastiCache, engine: "redis", version: "5"}: {
			healthEvent("standard_support_end", now.AddDate(-1, 0, 0), "all"),
			healthEvent("extended_support_end", now.AddDate(1, 0, 0), "all"),
		},
	}}
	// Repeated resources share the same engine version, region, and scan time.
	if _, err := catalog.lookup(ServiceElastiCache, "redis", "5.0.6", "us-east-1", now); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := catalog.lookup(ServiceElastiCache, "redis", "5.0.6", "us-east-1", now); err != nil {
			b.Fatal(err)
		}
	}
}
