// Package accountreview builds per-account cost and inventory reports so owners
// can decide whether to keep or delete AWS accounts.
//
// Build gathers data for every selected account. Failures that affect a single
// account (tags, Cost Explorer, inventory) are recorded on that AccountReport
// and do not abort the rest of the batch. GroupReports then drops accounts with
// no resolvable owner email so callers can plan or send the remaining messages.
package accountreview

import (
	"fmt"
	"strings"
	"time"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/inventory"
)

// GroupBy controls how reports are grouped for email delivery.
type GroupBy string

const (
	// GroupByAccount sends one email per account.
	GroupByAccount GroupBy = "account"
	// GroupByOwner sends one email per owner covering all of that owner's accounts.
	GroupByOwner GroupBy = "owner"
)

// ParseGroupBy parses a --group-by flag value.
func ParseGroupBy(s string) (GroupBy, error) {
	switch GroupBy(strings.TrimSpace(s)) {
	case GroupByAccount, "":
		return GroupByAccount, nil
	case GroupByOwner:
		return GroupByOwner, nil
	default:
		return "", fmt.Errorf("unknown group-by %q (supported: account, owner)", strings.TrimSpace(s))
	}
}

// DeliveryStatus describes the outcome of preparing or sending an owner notification.
type DeliveryStatus string

const (
	// StatusPlanned is a dry-run outcome: the message would be sent but --send was not set.
	StatusPlanned DeliveryStatus = "planned"
	// StatusSent means Gmail accepted the message.
	StatusSent DeliveryStatus = "sent"
	// StatusSendFailed means Gmail rejected the message (other accounts still send).
	StatusSendFailed DeliveryStatus = "send_failed"
	// StatusOwnerNotFound means the Organizations owner tag is missing or empty.
	StatusOwnerNotFound DeliveryStatus = "owner_not_found"
	// StatusInvalidOwner means the owner tag is present but is not a usable email.
	StatusInvalidOwner DeliveryStatus = "invalid_owner"
	// StatusSkipped means the account was not reviewed (no matches, or assume-role failed).
	StatusSkipped DeliveryStatus = "skipped"
)

// AccountReport is the full review payload for one AWS account.
type AccountReport struct {
	AccountID    string            `json:"account_id"`
	AccountName  string            `json:"account_name"`
	DisplayAlias string            `json:"display_alias,omitempty"`
	OUPath       string            `json:"ou_path,omitempty"`
	Tags         []coreaccount.Tag `json:"tags,omitempty"`
	// OwnerEmail is the parsed mailbox (lowercase). Empty when OwnerError is set.
	OwnerEmail string `json:"owner_email,omitempty"`
	// OwnerError explains why OwnerEmail could not be resolved (missing tag, invalid address, ListTags failure).
	OwnerError   string                     `json:"owner_error,omitempty"`
	MonthlyCosts cost.AccountMonthlyCosts   `json:"monthly_costs"`
	Inventory    inventory.AccountInventory `json:"inventory"`
	// InventoryError is a human-readable join of regional skips and account-level inventory warnings.
	InventoryError string    `json:"inventory_error,omitempty"`
	GeneratedAt    time.Time `json:"generated_at"`
}

// DeliveryResult records the outcome for one account or owner group.
// AccountID is set for per-account failures; AccountIDs is set when one email covers several accounts.
type DeliveryResult struct {
	AccountID  string         `json:"account_id,omitempty"`
	AccountIDs []string       `json:"account_ids,omitempty"`
	OwnerEmail string         `json:"owner_email,omitempty"`
	Status     DeliveryStatus `json:"status"`
	Reason     string         `json:"reason,omitempty"`
}

// OwnerGroup bundles one or more account reports that share a delivery recipient.
type OwnerGroup struct {
	OwnerEmail string
	Reports    []AccountReport
}

// BuildResult is the output of a full account review build.
type BuildResult struct {
	Reports []AccountReport
}
