package execsummary

// Section 6 constants from golang-executive-summary-spec.md.
const (
	// ExternalVCPURatePerHour is the dollar rate per vCPU-hour billed to
	// external (revenue-generating) customers only. Internal clusters
	// always generate zero revenue regardless of vCPU-hours (spec 6.1).
	ExternalVCPURatePerHour = 0.04275

	// ExternalVCPURatePer1KHour equals ExternalVCPURatePerHour * 1000 and
	// is used by CalculateUnitCost's $/1K-vCPU-hour result (spec 6.6).
	ExternalVCPURatePer1KHour = 42.75
)

// externalClassification is the only OCM_MAPPING.CLASSIFICATION value that
// counts as revenue-generating (spec 6's EXTERNAL_CLASSIFICATIONS = {"external"}).
const externalClassification = "external"

// IsExternal reports whether a classification counts as revenue-generating
// per spec 6.1's EXTERNAL_CLASSIFICATIONS set.
func IsExternal(classification string) bool {
	return classification == externalClassification
}
