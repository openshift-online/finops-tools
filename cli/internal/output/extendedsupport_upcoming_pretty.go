package output

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/openshift-online/finops-tools/core/awshealth"
)

// Require whitespace after sentence punctuation so version numbers stay intact.
var upcomingDescriptionSummaryRE = regexp.MustCompile(`^\[[^\]]*\]\s*(.+?[.!?]\s+.+?[.!?])(?:\s|$)`)

var upcomingDescriptionReferenceRE = regexp.MustCompile(`\s*\[[0-9]+\]`)

func upcomingDescriptionSummary(description string) string {
	description = SanitizeTerminal(strings.Join(strings.Fields(description), " "))
	// Remove references before matching so "sentence.[1]" is a sentence boundary.
	withoutReferences := upcomingDescriptionReferenceRE.ReplaceAllString(description, "")
	if match := upcomingDescriptionSummaryRE.FindStringSubmatch(withoutReferences); match != nil {
		return match[1]
	}
	return description
}

func writeUpcomingLifecyclePretty(w io.Writer, result awshealth.Result) error {
	s := newStyler(w)
	if _, err := fmt.Fprintf(w, "Upcoming planned lifecycle events: %d\nAs of: %s\n", len(result.Events), upcomingTime(&result.AsOf)); err != nil {
		return err
	}
	if len(result.Services) > 0 {
		if _, err := fmt.Fprintf(w, "Services: %s\n", SanitizeTerminal(strings.Join(result.Services, ", "))); err != nil {
			return err
		}
	}
	if len(result.Regions) > 0 {
		if _, err := fmt.Fprintf(w, "Event regions: %s\n", SanitizeTerminal(strings.Join(result.Regions, ", "))); err != nil {
			return err
		}
	}
	if err := writeExcludedRegions(w, result.ExcludedRegions); err != nil {
		return err
	}
	if !result.Complete {
		if _, err := fmt.Fprintln(w, "Results incomplete; review lookup warnings."); err != nil {
			return err
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(w, "Warning: payer %s event %s account %s (%s): %s\n", SanitizeTerminal(warning.PayerAccountID), SanitizeTerminal(warning.EventARN), SanitizeTerminal(warning.AccountID), SanitizeTerminal(warning.Operation), SanitizeTerminal(warning.Message)); err != nil {
			return err
		}
	}
	if len(result.Events) == 0 && result.Complete {
		_, err := fmt.Fprintln(w, "No matching upcoming planned lifecycle events.")
		return err
	}
	for _, event := range result.Events {
		title := upcomingTime(&event.StartTime) + "  " + strings.TrimPrefix(SanitizeTerminal(event.TypeCode), "AWS_")
		if s.enabled {
			title = s.bold(s.cyan(title))
		}
		if _, err := fmt.Fprintf(w, "\n%s\n  Payer: %s  Service: %s  Region: %s  Status: %s\n  Event: %s\n", title, SanitizeTerminal(event.PayerAccountID), SanitizeTerminal(event.Service), SanitizeTerminal(event.Region), SanitizeTerminal(event.Status), SanitizeTerminal(event.ARN)); err != nil {
			return err
		}
		if event.Description != "" {
			if _, err := fmt.Fprintf(w, "  %s\n", upcomingDescriptionSummary(event.Description)); err != nil {
				return err
			}
		}
		if len(event.Accounts) == 0 {
			message := "No affected accounts reported."
			if !event.AccountsComplete {
				message = "Affected account lookup incomplete."
			}
			if _, err := fmt.Fprintf(w, "  %s\n", message); err != nil {
				return err
			}
			continue
		}
		// tablewriter discards write errors, so render into memory before writing to w.
		var tableOutput bytes.Buffer
		table := newExtendedSupportTable(&tableOutput)
		table.SetHeader([]string{cell(s, s.bold, "ACCOUNT"), cell(s, s.bold, "RESOURCE ID / ARN"), cell(s, s.bold, "STATUS")})
		for _, account := range event.Accounts {
			if len(account.Resources) == 0 {
				message := "No resources reported"
				if !account.ResourcesComplete {
					message = "Resource lookup incomplete"
				}
				table.Append([]string{account.AccountID, message, ""})
			}
			for _, resource := range account.Resources {
				id := resource.ResourceID
				if id == "" {
					id = "Health entity: " + resource.EntityARN
				}
				status := resource.Status
				if status == "" {
					status = "Not reported"
				}
				table.Append([]string{SanitizeTerminal(account.AccountID), SanitizeTerminal(id), SanitizeTerminal(status)})
			}
		}
		table.Render()
		if _, err := tableOutput.WriteTo(w); err != nil {
			return err
		}
	}
	return nil
}
