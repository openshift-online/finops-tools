package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/awshealth"
)

func upcomingFixture() awshealth.Result {
	start := time.Date(2027, 1, 15, 12, 30, 0, 0, time.UTC)
	return awshealth.Result{AsOf: start.AddDate(0, -1, 0), Complete: true, Events: []awshealth.Event{{
		PayerAccountID: "123456789012", ARN: "event-1", TypeCode: "AWS_RDS_PLANNED_LIFECYCLE_EVENT", StartTime: start,
		Service: "RDS", Region: "us-east-1", Status: "upcoming", Description: "Upgrade this database", AccountsComplete: true,
		Accounts: []awshealth.AffectedAccount{{AccountID: "111111111111", ResourcesComplete: true, Resources: []awshealth.Resource{{
			ResourceID: "arn:aws:rds:us-east-1:111111111111:db:production", EntityARN: "arn:aws:health:us-east-1:111111111111:entity/123", Status: "PENDING", LastUpdatedTime: &start,
		}}}},
	}}}
}

func TestUpcomingLifecycleOutputPreservesHierarchyAndResourceIdentifiers(t *testing.T) {
	result := upcomingFixture()
	result.LookaheadDays = 90
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		t.Run(string(format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteUpcomingLifecycleResult(&buf, format, result); err != nil {
				t.Fatal(err)
			}
			text := buf.String()
			eventType := "AWS_RDS_PLANNED_LIFECYCLE_EVENT"
			if format == FormatPrettyPrint {
				if !strings.Contains(text, "Lookahead: 90 days (through 2027-03-15T12:30:00Z)") {
					t.Fatal("missing lookahead window:", text)
				}
				if strings.Contains(text, eventType) {
					t.Fatal("pretty output retained the AWS_ event-name prefix")
				}
				eventType = "RDS_PLANNED_LIFECYCLE_EVENT"
			}
			for _, value := range []string{"2027-01-15T12:30:00Z", eventType, "111111111111", "arn:aws:rds:us-east-1:111111111111:db:production", "PENDING", "Upgrade this database"} {
				if !strings.Contains(text, value) {
					t.Fatalf("missing %q: %s", value, text)
				}
			}
			if strings.Contains(text, "\x1b[") {
				t.Fatal("ANSI in nonterminal output")
			}
			switch format {
			case FormatJSON:
				var decoded awshealth.Result
				if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil || decoded.Events[0].Accounts[0].Resources[0].EntityARN != result.Events[0].Accounts[0].Resources[0].EntityARN || decoded.LookaheadDays != 90 {
					t.Fatal(decoded, err)
				}
			case FormatCSV:
				rows, err := csv.NewReader(&buf).ReadAll()
				if err != nil || len(rows) != 2 || rows[1][0] != "resource" || rows[1][11] == rows[1][12] {
					t.Fatal(rows, err)
				}
			}
		})
	}
}

func TestUpcomingLifecycleIncompleteAndEmptyAccountsRemainVisibleInEveryFormat(t *testing.T) {
	result := upcomingFixture()
	result.Complete = false
	result.Events[0].Accounts = append(result.Events[0].Accounts, awshealth.AffectedAccount{AccountID: "222222222222", Resources: []awshealth.Resource{}})
	result.Events = append(result.Events, awshealth.Event{ARN: "no-accounts", TypeCode: "PLANNED_LIFECYCLE_EVENT", StartTime: result.Events[0].StartTime})
	result.Warnings = []awshealth.Warning{{PayerAccountID: "123456789012", EventARN: "unmatched-event", AccountID: "222222222222", Operation: "event_details", Message: "Access denied"}}
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		var buf bytes.Buffer
		if err := WriteUpcomingLifecycleResult(&buf, format, result); err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"222222222222", "no-accounts", "unmatched-event", "Access denied"} {
			if !strings.Contains(buf.String(), value) {
				t.Fatalf("format=%s missing %s: %s", format, value, buf.String())
			}
		}
		if format == FormatPrettyPrint && (!strings.Contains(buf.String(), "Results incomplete") || !strings.Contains(buf.String(), "Resource lookup incomplete") || !strings.Contains(buf.String(), "Affected account lookup incomplete")) {
			t.Fatal(buf.String())
		}
		if format == FormatCSV {
			rows, err := csv.NewReader(&buf).ReadAll()
			if err != nil || len(rows) != 5 || rows[4][0] != "warning" || rows[4][15] != "false" {
				t.Fatal(rows, err)
			}
		}
	}
}

func TestUpcomingLifecycleEmptyAndSanitizedOutput(t *testing.T) {
	var buf bytes.Buffer
	result := awshealth.Result{Complete: true, Events: []awshealth.Event{}}
	if err := WriteUpcomingLifecycleResult(&buf, FormatPrettyPrint, result); err != nil || !strings.Contains(buf.String(), "No matching upcoming") {
		t.Fatal(buf.String(), err)
	}
	result = upcomingFixture()
	result.Events[0].Description = "\x1b[31mUntrusted description\x1b[0m"
	result.Events[0].Accounts[0].Resources[0].ResourceID = "=HYPERLINK(\"example\")"
	buf.Reset()
	if err := WriteUpcomingLifecycleResult(&buf, FormatPrettyPrint, result); err != nil || strings.Contains(buf.String(), "\x1b") {
		t.Fatal(buf.String(), err)
	}
	buf.Reset()
	if err := WriteUpcomingLifecycleResult(&buf, FormatCSV, result); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || !strings.HasPrefix(rows[1][11], "'=HYPERLINK") {
		t.Fatal(rows, err)
	}
	if err := WriteUpcomingLifecycleResult(&buf, Format("bad"), result); err == nil {
		t.Fatal("invalid format accepted")
	}
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		if err := WriteUpcomingLifecycleResult(upcomingBrokenWriter{}, format, result); err == nil {
			t.Fatalf("write error hidden for %s", format)
		}
	}
}

type upcomingBrokenWriter struct{}

func (upcomingBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestUpcomingLifecyclePrettyPropagatesTableWriteErrors(t *testing.T) {
	result := upcomingFixture()
	var buf bytes.Buffer
	if err := WriteUpcomingLifecycleResult(&buf, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("write failed during table output")
	for _, section := range []struct {
		name   string
		marker string
	}{
		{name: "header", marker: "RESOURCE ID / ARN"},
		{name: "resource", marker: result.Events[0].Accounts[0].Resources[0].ResourceID},
	} {
		start := strings.Index(buf.String(), section.marker)
		if start < 0 {
			t.Fatalf("missing table %s in output", section.name)
		}
		for _, failure := range []struct {
			name string
			err  error
			want error
		}{
			{name: "error", err: writeErr, want: writeErr},
			{name: "short write", want: io.ErrShortWrite},
		} {
			t.Run(section.name+"/"+failure.name, func(t *testing.T) {
				w := &upcomingLimitedWriter{remaining: start + len(section.marker)/2, err: failure.err}
				if err := WriteUpcomingLifecycleResult(w, FormatPrettyPrint, result); !errors.Is(err, failure.want) {
					t.Fatalf("error = %v, want %v", err, failure.want)
				}
			})
		}
	}
}

type upcomingLimitedWriter struct {
	remaining int
	err       error
}

func (w *upcomingLimitedWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		n := w.remaining
		w.remaining = 0
		return n, w.err
	}
	w.remaining -= len(p)
	return len(p), nil
}
