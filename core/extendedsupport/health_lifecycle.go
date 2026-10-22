package extendedsupport

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
)

type healthLifecycle struct {
	standardEnd, extendedStart, extendedEnd, endOfLife *time.Time
	supportedOnly                                      bool
}

var lifecycleNameReplacer = strings.NewReplacer("-", "_", " ", "_")

func normalizeLifecycleName(value string) string {
	return strings.ToUpper(lifecycleNameReplacer.Replace(strings.TrimSpace(value)))
}

func (c *HealthLifecycleCatalog) lookup(service Service, engine, version, region string, now time.Time) (healthLifecycle, error) {
	if c == nil {
		return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle catalog was not loaded")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		// Loading failures remain retryable; only cache lookups against the
		// complete, immutable catalog once it has been published.
		if c.err != nil {
			return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle catalog unavailable: %w", c.err)
		}
		return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle catalog was not loaded")
	}
	key := healthLookupKey{
		version: healthVersionKey{service: service, engine: engine, version: version},
		region:  strings.ToLower(region), now: now.UTC(),
	}
	if cached, ok := c.lookups[key]; ok {
		return cached.lifecycle, cached.err
	}
	// The short, in-memory parse runs under the lock so concurrent accounts
	// resolve each key once. Network catalog loading runs outside this lock.
	lifecycle, err := lookupHealthLifecycle(c.entries, service, engine, version, region, now)
	if c.lookups == nil {
		c.lookups = make(map[healthLookupKey]healthLookupResult)
	}
	c.lookups[key] = healthLookupResult{lifecycle: lifecycle, err: err}
	return lifecycle, err
}

func lookupHealthLifecycle(entries map[healthVersionKey][]healthtypes.LifecycleEvent, service Service, engine, version, region string, now time.Time) (healthLifecycle, error) {
	versions := []string{version}
	if service == ServiceElastiCache {
		parts := strings.Split(version, ".")
		if len(parts) > 2 {
			versions = append(versions, strings.Join(parts[:2], "."))
		}
		if len(parts) > 1 {
			versions = append(versions, parts[0])
		}
	}
	var failures []string
	for _, candidate := range versions {
		if events, exists := entries[healthVersionKey{service: service, engine: engine, version: candidate}]; exists {
			lifecycle, err := parseHealthLifecycle(events, region, now)
			// A future standard-support end (or extended-support start) is enough
			// to classify a version as not yet in extended support. Once those
			// boundaries are reached, an extended-support end or end-of-life
			// boundary is required too.
			if err == nil && !lifecycle.supportedOnly && lifecycle.extendedEnd == nil && lifecycle.endOfLife == nil && !now.Before(*lifecycle.standardEnd) &&
				(lifecycle.extendedStart == nil || !now.Before(*lifecycle.extendedStart)) {
				err = fmt.Errorf("AWS Health extended support end date is missing")
			}
			if err == nil {
				return lifecycle, nil
			}
			// An existing but unusable entry must not hide a usable broader
			// version entry. Each candidate must supply its own valid schedule.
			failures = append(failures, fmt.Sprintf("version %s (%v)", candidate, err))
		}
	}
	if len(failures) > 0 {
		return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle information for %s %s %s is unusable in region %s; %s", service, engine, version, region, strings.Join(failures, "; "))
	}
	return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle information for %s %s %s is missing", service, engine, version)
}

type healthMilestone struct {
	date     *time.Time
	rank     int
	conflict bool
}

func (m *healthMilestone) add(date *time.Time, rank int) {
	if rank > m.rank {
		m.date, m.rank, m.conflict = date, rank, false
		return
	}
	if rank == m.rank && (m.date == nil || date == nil || !m.date.Equal(*date)) {
		m.conflict = true
	}
}

func (m healthMilestone) knownDate() *time.Time {
	if m.conflict || m.date == nil || m.date.IsZero() {
		return nil
	}
	return m.date
}

func parseHealthLifecycle(events []healthtypes.LifecycleEvent, region string, now time.Time) (healthLifecycle, error) {
	var standard, start, extended, endOfLife, generic, billedSupportEnd healthMilestone
	var regionErr error
	var supported, otherApplicableEvents bool
	for _, event := range events {
		kind := normalizeLifecycleName(aws.ToString(event.LifecycleEventType))
		rank := 0
		for _, applies := range event.Regions {
			if strings.EqualFold(applies, region) {
				rank = 2
				break
			}
			if strings.EqualFold(applies, "all") {
				rank = 1
			}
		}
		var milestone *healthMilestone
		switch kind {
		case "SUPPORTED":
			// This is an explicit support status; no cutoff date is required.
			supported = supported || rank > 0
		case "END_OF_STANDARD_SUPPORT", "STANDARD_SUPPORT_END", "STANDARD_SUPPORT_END_DATE":
			milestone = &standard
		case "START_OF_EXTENDED_SUPPORT", "EXTENDED_SUPPORT_START":
			milestone = &start
		case "END_OF_EXTENDED_SUPPORT", "EXTENDED_SUPPORT_END", "EXTENDED_SUPPORT_END_DATE":
			milestone = &extended
		case "END_OF_LIFE":
			milestone = &endOfLife
		case "END_OF_SUPPORT":
			milestone = &generic
			for _, risk := range event.ImpactRisks {
				if normalizeLifecycleName(risk) == "BILLING" {
					milestone = &billedSupportEnd
				}
			}
		default:
			// An unrecognized applicable event prevents treating this entry as
			// supported-only, since it could describe another support state.
			otherApplicableEvents = otherApplicableEvents || rank > 0
			continue
		}
		if len(event.Regions) == 0 {
			if regionErr == nil {
				regionErr = fmt.Errorf("AWS Health support milestone %s has no region information", kind)
			}
			continue
		}
		if rank == 0 {
			continue
		}
		if kind == "SUPPORTED" {
			continue
		}
		otherApplicableEvents = true
		milestone.add(event.Date, rank)
	}
	// Free-form event names are interpreted conservatively. A generic END_OF_SUPPORT
	// is usable only with a corresponding explicit boundary, or a BILLING impact.
	if standard.rank == 0 && billedSupportEnd.rank > 0 {
		standard = billedSupportEnd
	}
	if standard.rank == 0 && start.rank > 0 {
		standard = start
	}
	if generic.rank > 0 {
		if standard.rank == 0 && (extended.rank > 0 || endOfLife.rank > 0) {
			standard = generic
		} else if extended.rank == 0 && standard.rank > 0 {
			extended = generic
		}
	}
	if endOfLife.conflict {
		return healthLifecycle{}, fmt.Errorf("AWS Health end of life milestones have conflicting dates for region %s", region)
	}
	if endOfLife.rank > 0 && endOfLife.knownDate() == nil {
		return healthLifecycle{}, fmt.Errorf("AWS Health end of life date is missing")
	}
	if dateReached(now, endOfLife.date) {
		// A terminal event establishes that the version is stale independently
		// of missing or unusable support milestones. Keep only known dates.
		return healthLifecycle{
			standardEnd: standard.knownDate(), extendedStart: start.knownDate(),
			extendedEnd: extended.knownDate(), endOfLife: endOfLife.date,
		}, nil
	}
	if regionErr != nil {
		return healthLifecycle{}, regionErr
	}
	if supported && !otherApplicableEvents {
		return healthLifecycle{supportedOnly: true}, nil
	}
	if standard.rank == 0 {
		return healthLifecycle{}, fmt.Errorf("AWS Health standard support end milestone is missing or ambiguous for region %s", region)
	}
	if standard.conflict || start.conflict || extended.conflict {
		return healthLifecycle{}, fmt.Errorf("AWS Health support milestones have conflicting dates for region %s", region)
	}
	if standard.date == nil || standard.date.IsZero() {
		return healthLifecycle{}, fmt.Errorf("AWS Health standard support end date is missing")
	}
	if extended.rank > 0 && (extended.date == nil || extended.date.IsZero()) {
		return healthLifecycle{}, fmt.Errorf("AWS Health extended support end date is missing")
	}
	if start.rank > 0 && (start.date == nil || start.date.IsZero()) {
		return healthLifecycle{}, fmt.Errorf("AWS Health extended support start date is missing")
	}
	if extended.date != nil && !extended.date.After(*standard.date) {
		return healthLifecycle{}, fmt.Errorf("AWS Health support dates are inconsistent")
	}
	if start.date != nil && (start.date.Before(*standard.date) || (extended.date != nil && !extended.date.After(*start.date))) {
		return healthLifecycle{}, fmt.Errorf("AWS Health support dates are inconsistent")
	}
	if endOfLife.date != nil && (!endOfLife.date.After(*standard.date) ||
		(start.date != nil && !endOfLife.date.After(*start.date)) ||
		(extended.date != nil && endOfLife.date.Before(*extended.date))) {
		return healthLifecycle{}, fmt.Errorf("AWS Health support dates are inconsistent")
	}
	return healthLifecycle{standardEnd: standard.date, extendedStart: start.date, extendedEnd: extended.date, endOfLife: endOfLife.date}, nil
}

// classifyHealthSupport returns resources whose standard support or life has ended.
// buildResult separates current extended support from past extended support.
func classifyHealthSupport(catalog *HealthLifecycleCatalog, warning ResourceWarning, now time.Time) (*Record, *ResourceWarning) {
	lifecycle, err := catalog.lookup(warning.Service, warning.Engine, warning.EngineVersion, warning.Region, now)
	if err != nil {
		warning.Message = warningMessage(err)
		return nil, &warning
	}
	if lifecycle.supportedOnly {
		return nil, nil
	}
	if !dateReached(now, lifecycle.endOfLife) {
		if now.Before(*lifecycle.standardEnd) {
			return nil, nil
		}
		if lifecycle.extendedStart != nil && now.Before(*lifecycle.extendedStart) {
			return nil, nil
		}
	}
	major := warning.EngineVersion
	if warning.Service == ServiceElastiCache {
		major = strings.Split(major, ".")[0]
	}
	record := &Record{
		AccountID: warning.AccountID, Region: warning.Region, Service: warning.Service,
		ResourceType: warning.ResourceType, ResourceID: warning.ResourceID, Engine: warning.Engine,
		EngineVersion: warning.EngineVersion, MajorEngineVersion: major,
		ExtendedSupportEndDate: utcTimePtr(lifecycle.extendedEnd), EndOfLifeDate: utcTimePtr(lifecycle.endOfLife),
	}
	if lifecycle.standardEnd != nil {
		record.StandardSupportEndDate = lifecycle.standardEnd.UTC()
	}
	return record, nil
}
