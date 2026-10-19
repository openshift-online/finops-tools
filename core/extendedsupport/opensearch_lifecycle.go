package extendedsupport

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OpenSearch control-plane APIs expose engine versions, but no lifecycle dates.
// This immutable table follows AWS's schedule, including the August 2026 extensions:
// https://docs.aws.amazon.com/opensearch-service/latest/developerguide/what-is.html#standard-and-extended-support
// https://aws.amazon.com/blogs/big-data/amazon-opensearch-service-extends-version-lifecycle-support-timelines/
// Reviewed 2026-10-19. Update when AWS publishes new dates. Unknown versions warn.
// The published end dates are inclusive UTC calendar dates. The AWS FAQ states
// that Extended Support charges start the day after standard support ends:
// https://aws.amazon.com/opensearch-service/faqs/
type openSearchLifecycle struct{ standardEnd, extendedEnd time.Time }

var openSearchLifecycles = buildOpenSearchLifecycles()

func buildOpenSearchLifecycles() map[string]openSearchLifecycle {
	versions := make(map[string]openSearchLifecycle)
	add := func(standardYear, extendedYear int, names ...string) {
		for _, name := range names {
			versions[name] = openSearchLifecycle{
				standardEnd: time.Date(standardYear, 11, 7, 0, 0, 0, 0, time.UTC),
				extendedEnd: time.Date(extendedYear, 11, 7, 0, 0, 0, 0, time.UTC),
			}
		}
	}
	add(2025, 2027,
		"Elasticsearch_1.5", "Elasticsearch_2.3", "Elasticsearch_5.1", "Elasticsearch_5.3", "Elasticsearch_5.5",
		"Elasticsearch_6.0", "Elasticsearch_6.2", "Elasticsearch_6.3", "Elasticsearch_6.4", "Elasticsearch_6.5", "Elasticsearch_6.7",
		"Elasticsearch_7.1", "Elasticsearch_7.4", "Elasticsearch_7.7", "Elasticsearch_7.8",
		"OpenSearch_1.0", "OpenSearch_1.1", "OpenSearch_1.2", "OpenSearch_2.3", "OpenSearch_2.5", "OpenSearch_2.7", "OpenSearch_2.9")
	add(2025, 2028, "Elasticsearch_5.6")
	add(2027, 2030, "Elasticsearch_6.8", "Elasticsearch_7.10", "OpenSearch_1.3", "OpenSearch_2.19")
	add(2027, 2028, "Elasticsearch_7.9", "OpenSearch_2.11", "OpenSearch_2.13", "OpenSearch_2.15", "OpenSearch_2.17")
	// These published versions have no announced end of standard support.
	for _, name := range []string{"OpenSearch_3.1", "OpenSearch_3.3", "OpenSearch_3.5"} {
		versions[name] = openSearchLifecycle{}
	}
	return versions
}

func classifyOpenSearch(accountID, region, name, engineVersion string, now time.Time) (*Record, *ResourceWarning) {
	warning := &ResourceWarning{AccountID: accountID, Region: region, Service: ServiceOpenSearch, ResourceType: ResourceTypeOpenSearchDomain, ResourceID: name, EngineVersion: strings.TrimSpace(engineVersion)}
	if warning.EngineVersion == "" {
		warning.Message = "OpenSearch domain engine version is missing"
		return nil, warning
	}
	prefix, version, ok := strings.Cut(warning.EngineVersion, "_")
	engine := strings.ToLower(prefix)
	if !ok || (engine != "opensearch" && engine != "elasticsearch") {
		warning.Message = "OpenSearch domain engine version is invalid: " + warning.EngineVersion
		return nil, warning
	}
	warning.Engine, warning.EngineVersion = engine, version
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		warning.Message = "OpenSearch domain engine version is invalid: " + engineVersion
		return nil, warning
	}
	for _, part := range parts {
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil || strconv.FormatUint(value, 10) != part {
			warning.Message = "OpenSearch domain engine version is invalid: " + engineVersion
			return nil, warning
		}
	}
	canonical := "OpenSearch_" + version
	if engine == "elasticsearch" {
		canonical = "Elasticsearch_" + version
	}
	lifecycle, known := openSearchLifecycles[canonical]
	if !known {
		warning.Message = fmt.Sprintf("OpenSearch lifecycle information for %s %s is missing", engine, version)
		return nil, warning
	}
	if lifecycle.standardEnd.IsZero() || now.Before(lifecycle.standardEnd.AddDate(0, 0, 1)) {
		return nil, nil
	}
	if !now.Before(lifecycle.extendedEnd.AddDate(0, 0, 1)) {
		warning.Message = "OpenSearch domain engine version is past extended support"
		return nil, warning
	}
	return &Record{
		AccountID: accountID, Region: region, Service: ServiceOpenSearch, ResourceType: ResourceTypeOpenSearchDomain,
		ResourceID: name, Engine: engine, EngineVersion: version,
		// OpenSearch support schedules apply to major.minor versions, as with EKS.
		MajorEngineVersion: version, StandardSupportEndDate: lifecycle.standardEnd, ExtendedSupportEndDate: &lifecycle.extendedEnd,
	}, nil
}
