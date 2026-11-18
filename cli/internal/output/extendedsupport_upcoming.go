package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/openshift-online/finops-tools/core/awshealth"
)

func WriteUpcomingLifecycleResult(w io.Writer, format Format, result awshealth.Result) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case FormatCSV:
		return writeUpcomingLifecycleCSV(w, result)
	case FormatPrettyPrint:
		return writeUpcomingLifecyclePretty(w, result)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeUpcomingLifecycleCSV(w io.Writer, result awshealth.Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"row_type", "payer_account_id", "event_arn", "event_type_code", "service", "region", "start_time", "end_time", "event_status", "description", "account_id", "resource_id", "entity_arn", "resource_status", "last_updated_time", "complete", "accounts_complete", "resources_complete", "warning"}); err != nil {
		return err
	}
	writeRow := func(kind string, event awshealth.Event, account *awshealth.AffectedAccount, resource *awshealth.Resource, warning string) error {
		row := []string{kind, event.PayerAccountID, event.ARN, event.TypeCode, event.Service, event.Region, upcomingTime(&event.StartTime), upcomingTime(event.EndTime), event.Status, event.Description, "", "", "", "", "", strconv.FormatBool(result.Complete), strconv.FormatBool(event.AccountsComplete), "", warning}
		if account != nil {
			row[10] = account.AccountID
			row[17] = strconv.FormatBool(account.ResourcesComplete)
			if account.Description != "" {
				row[9] = account.Description
			}
		}
		if resource != nil {
			row[11], row[12], row[13], row[14] = resource.ResourceID, resource.EntityARN, resource.Status, upcomingTime(resource.LastUpdatedTime)
		}
		for i := range row {
			row[i] = sanitizeCSVField(row[i])
		}
		return cw.Write(row)
	}
	for _, event := range result.Events {
		if len(event.Accounts) == 0 {
			if err := writeRow("event", event, nil, nil, ""); err != nil {
				return err
			}
		}
		for _, account := range event.Accounts {
			if len(account.Resources) == 0 {
				if err := writeRow("account", event, &account, nil, ""); err != nil {
					return err
				}
			}
			for _, resource := range account.Resources {
				if err := writeRow("resource", event, &account, &resource, ""); err != nil {
					return err
				}
			}
		}
	}
	for _, warning := range result.Warnings {
		event := awshealth.Event{PayerAccountID: warning.PayerAccountID, ARN: warning.EventARN}
		account := awshealth.AffectedAccount{AccountID: warning.AccountID}
		if err := writeRow("warning", event, &account, nil, warning.Operation+": "+warning.Message); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func upcomingTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
