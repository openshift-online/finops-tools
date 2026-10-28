package execsummary

// PayerID identifies one of the report's tabs (spec section 7): the AWS
// payer tabs plus the aggregated "All Payers" tab. The HCP Cost Efficiency
// and Cluster Growth tabs are global (not per-payer) and have no PayerID.
type PayerID string

// Canonical payer tab IDs, in display order (spec section 7).
const (
	PayerAll         PayerID = "all"
	PayerProduction  PayerID = "production"
	PayerStaging     PayerID = "staging"
	PayerIntegration PayerID = "integration"
)
