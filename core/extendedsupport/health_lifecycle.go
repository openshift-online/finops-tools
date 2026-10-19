package extendedsupport

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
)

type healthLifecycle struct{ standardEnd, extendedStart, extendedEnd *time.Time }

func normalizeLifecycleName(value string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(value)))
}

func (c *HealthLifecycleCatalog) lookup(service Service, engine, version, region string) (healthLifecycle, error) {
	if c == nil {
		return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle catalog was not loaded")
	}
	c.mu.Lock()
	entries, catalogErr := c.entries, c.err
	c.mu.Unlock()
	if entries == nil {
		if catalogErr != nil {
			return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle catalog unavailable: %w", catalogErr)
		}
		return healthLifecycle{}, fmt.Errorf("AWS Health lifecycle catalog was not loaded")
	}
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
	for _, candidate := range versions {
		if events, exists := entries[healthVersionKey{service: service, engine: engine, version: candidate}]; exists {
			return parseHealthLifecycle(events, region)
		}
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

func parseHealthLifecycle(events []healthtypes.LifecycleEvent, region string) (healthLifecycle, error) {
	var standard, start, extended, generic, billedSupportEnd healthMilestone
	for _, event := range events {
		kind := normalizeLifecycleName(aws.ToString(event.LifecycleEventType))
		var milestone *healthMilestone
		switch kind {
		case "END_OF_STANDARD_SUPPORT", "STANDARD_SUPPORT_END", "STANDARD_SUPPORT_END_DATE":
			milestone = &standard
		case "START_OF_EXTENDED_SUPPORT", "EXTENDED_SUPPORT_START":
			milestone = &start
		case "END_OF_EXTENDED_SUPPORT", "EXTENDED_SUPPORT_END", "EXTENDED_SUPPORT_END_DATE", "END_OF_LIFE":
			milestone = &extended
		case "END_OF_SUPPORT":
			milestone = &generic
			for _, risk := range event.ImpactRisks {
				if normalizeLifecycleName(risk) == "BILLING" {
					milestone = &billedSupportEnd
				}
			}
		default:
			continue
		}
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
		if len(event.Regions) == 0 {
			return healthLifecycle{}, fmt.Errorf("AWS Health support milestone %s has no region information", kind)
		}
		if rank == 0 {
			continue
		}
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
		if standard.rank == 0 && extended.rank > 0 {
			standard = generic
		} else if extended.rank == 0 && standard.rank > 0 {
			extended = generic
		}
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
	return healthLifecycle{standardEnd: standard.date, extendedStart: start.date, extendedEnd: extended.date}, nil
}

func classifyHealthSupport(catalog *HealthLifecycleCatalog, warning ResourceWarning, now time.Time) (*Record, *ResourceWarning) {
	lifecycle, err := catalog.lookup(warning.Service, warning.Engine, warning.EngineVersion, warning.Region)
	if err != nil {
		warning.Message = warningMessage(err)
		return nil, &warning
	}
	if now.Before(*lifecycle.standardEnd) {
		return nil, nil
	}
	if lifecycle.extendedStart != nil && now.Before(*lifecycle.extendedStart) {
		return nil, nil
	}
	if lifecycle.extendedEnd == nil {
		warning.Message = "AWS Health extended support end date is missing"
		return nil, &warning
	}
	if !now.Before(*lifecycle.extendedEnd) {
		warning.Message = "engine version is past extended support according to AWS Health"
		return nil, &warning
	}
	major := warning.EngineVersion
	if warning.Service == ServiceElastiCache {
		major = strings.Split(major, ".")[0]
	}
	return &Record{
		AccountID: warning.AccountID, Region: warning.Region, Service: warning.Service,
		ResourceType: warning.ResourceType, ResourceID: warning.ResourceID, Engine: warning.Engine,
		EngineVersion: warning.EngineVersion, MajorEngineVersion: major,
		StandardSupportEndDate: lifecycle.standardEnd.UTC(), ExtendedSupportEndDate: lifecycle.extendedEnd,
	}, nil
}
