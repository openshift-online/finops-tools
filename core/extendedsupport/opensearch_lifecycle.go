package extendedsupport

import (
	"strconv"
	"strings"
	"time"
)

func classifyOpenSearch(accountID, region, name, engineVersion string, now time.Time, catalog *HealthLifecycleCatalog) (*Record, *ResourceWarning) {
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
	return classifyHealthSupport(catalog, *warning, now)
}
