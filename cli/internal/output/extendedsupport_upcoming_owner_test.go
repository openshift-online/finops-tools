package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/awshealth"
)

func TestUpcomingOwnerOutputKeepsEventsAccountsAndResources(t *testing.T) {
	result := upcomingFixture()
	result.OwnersIncluded = true
	result.Events[0].Accounts[0].OwnerEmail = "jdoe@redhat.com"
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		t.Run(string(format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteUpcomingLifecycleResult(&buf, format, result); err != nil {
				t.Fatal(err)
			}
			text := buf.String()
			for _, want := range []string{"jdoe@redhat.com", "event-1", "2027-01-15T12:30:00Z", "Upgrade this database", "111111111111", "arn:aws:rds:us-east-1:111111111111:db:production"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
			if strings.Contains(text, "\x1b") {
				t.Fatal("ANSI in nonterminal output:", text)
			}
			switch format {
			case FormatPrettyPrint:
				if !strings.Contains(text, "OWNER") || strings.Contains(text, "STATUS") || strings.Contains(text, "PENDING") {
					t.Fatal(text)
				}
			case FormatJSON:
				var decoded awshealth.Result
				if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil || !decoded.OwnersIncluded || decoded.Events[0].Accounts[0].Resources[0].Status != "PENDING" {
					t.Fatal(decoded, err)
				}
			case FormatCSV:
				rows, err := csv.NewReader(&buf).ReadAll()
				if err != nil || len(rows) != 2 || rows[0][13] != "owner_email" || rows[1][13] != "jdoe@redhat.com" || rows[0][19] != "owner_error" || strings.Contains(text, "PENDING") {
					t.Fatal(rows, err)
				}
			}
		})
	}
}

func TestUpcomingOwnerOutputPreservesMissingOwnersAndEmptyAccounts(t *testing.T) {
	result := upcomingFixture()
	result.OwnersIncluded, result.Complete = true, false
	result.Events[0].Accounts[0].OwnerError = "owner tag missing"
	result.Events[0].Accounts = append(result.Events[0].Accounts, awshealth.AffectedAccount{
		AccountID: "222222222222", OwnerEmail: "other@redhat.com", ResourcesComplete: false,
	})
	result.Events = append(result.Events, awshealth.Event{ARN: "public-event", AccountsComplete: true, Accounts: []awshealth.AffectedAccount{}})
	result.Warnings = []awshealth.Warning{{PayerAccountID: "123456789012", EventARN: "event-1", AccountID: "111111111111", Operation: "account_owner", Message: "owner tag missing"}}
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		var buf bytes.Buffer
		if err := WriteUpcomingLifecycleResult(&buf, format, result); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"owner tag missing", "111111111111", "222222222222", "other@redhat.com", "public-event", "account_owner"} {
			if !strings.Contains(buf.String(), want) {
				t.Fatalf("format=%s missing %q: %s", format, want, buf.String())
			}
		}
		if format == FormatPrettyPrint && (!strings.Contains(buf.String(), "Unknown") || !strings.Contains(buf.String(), "Resource lookup incomplete")) {
			t.Fatal(buf.String())
		}
		if format == FormatCSV {
			rows, err := csv.NewReader(&buf).ReadAll()
			if err != nil || len(rows) != 5 || rows[1][13] != "" || rows[1][19] != "owner tag missing" || rows[2][13] != "other@redhat.com" || rows[3][13] != "" {
				t.Fatal(rows, err)
			}
		}
	}
}

func TestUpcomingOwnerOutputSanitizesOwnerFields(t *testing.T) {
	result := upcomingFixture()
	result.OwnersIncluded = true
	result.Events[0].Accounts[0].OwnerEmail = "\x1b[31mjdoe@redhat.com\x1b[0m"
	var buf bytes.Buffer
	if err := WriteUpcomingLifecycleResult(&buf, FormatPrettyPrint, result); err != nil || strings.Contains(buf.String(), "\x1b") {
		t.Fatal(buf.String(), err)
	}
	result.Events[0].Accounts[0].OwnerEmail = "=formula"
	result.Events[0].Accounts[0].OwnerError = "=error"
	buf.Reset()
	if err := WriteUpcomingLifecycleResult(&buf, FormatCSV, result); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || rows[1][13] != "'=formula" || rows[1][19] != "'=error" {
		t.Fatal(rows, err)
	}
}
