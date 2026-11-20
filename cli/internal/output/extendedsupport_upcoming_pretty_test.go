package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/awshealth"
)

const upcomingDescriptionNotice = "[AWS Health may periodically trigger reminder notifications about this communication if resources remain unresolved.]"
const upcomingDescriptionReferencedBrief = "You are receiving this message because you have one or more Amazon Aurora PostgreSQL DB instances running major version 14 that require your attention. Aurora PostgreSQL 14 will be deprecated by the community in November 2026 [1], and will not receive any bug fixes or security patches after that."
const upcomingDescriptionBrief = "You are receiving this message because you have one or more Amazon Aurora PostgreSQL DB instances running major version 14 that require your attention. Aurora PostgreSQL 14 will be deprecated by the community in November 2026, and will not receive any bug fixes or security patches after that."
const upcomingDescriptionExample = upcomingDescriptionNotice + " " + upcomingDescriptionReferencedBrief + " More text about Extended Support."

func TestUpcomingLifecyclePrettySummarizesDescription(t *testing.T) {
	for _, tc := range []struct {
		name        string
		description string
		want        string
	}{
		{name: "leading notice", description: upcomingDescriptionExample, want: upcomingDescriptionBrief},
		{name: "no references", description: upcomingDescriptionNotice + " " + upcomingDescriptionBrief + " More text.", want: upcomingDescriptionBrief},
		{name: "multiline", description: upcomingDescriptionNotice + "\nFirst sentence.\n Second\t sentence. Third sentence.", want: "First sentence. Second sentence."},
		{name: "versions and references", description: upcomingDescriptionNotice + " Version 14.3 requires action [1]. Version 15.2 remains supported [2]. More text.", want: "Version 14.3 requires action. Version 15.2 remains supported."},
		{name: "references before punctuation", description: upcomingDescriptionNotice + " First sentence [1]. Second sentence [2], with details [12]. More text.", want: "First sentence. Second sentence, with details."},
		{name: "references after punctuation", description: upcomingDescriptionNotice + " First sentence.[1] Second sentence.[2] More text.", want: "First sentence. Second sentence."},
		{name: "references between words", description: upcomingDescriptionNotice + " First [1] sentence. Second [2] sentence.", want: "First sentence. Second sentence."},
		{name: "consecutive references", description: upcomingDescriptionNotice + " First sentence [1][2]. Second sentence [3] [12]. More text.", want: "First sentence. Second sentence."},
		{name: "meaningful brackets", description: upcomingDescriptionNotice + " Instances [Preview] require attention [1]. Check [PostgreSQL 14] settings [2]. More text.", want: "Instances [Preview] require attention. Check [PostgreSQL 14] settings."},
		{name: "question and exclamation", description: upcomingDescriptionNotice + " Is an upgrade needed? Yes, upgrade now! More text.", want: "Is an upgrade needed? Yes, upgrade now!"},
		{name: "no leading notice", description: "Support ends in November [1]. Upgrade soon. More text.", want: "Support ends in November [1]. Upgrade soon. More text."},
		{name: "fewer than two sentences", description: upcomingDescriptionNotice + " Upgrade soon.", want: upcomingDescriptionNotice + " Upgrade soon."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := upcomingFixture()
			result.Events[0].Description = tc.description
			var buf bytes.Buffer
			if err := WriteUpcomingLifecycleResult(&buf, FormatPrettyPrint, result); err != nil {
				t.Fatal(err)
			}
			// The description follows the event ARN and precedes the account table.
			_, afterEvent, ok := strings.Cut(buf.String(), "  Event: event-1\n")
			if !ok {
				t.Fatal("missing event section:", buf.String())
			}
			description, _, _ := strings.Cut(afterEvent, "\n")
			if want := "  " + tc.want; description != want {
				t.Fatalf("description = %q, want %q", description, want)
			}
		})
	}
}

func TestUpcomingLifecyclePrettySummaryPreservesFullExportedDescriptions(t *testing.T) {
	result := upcomingFixture()
	result.Events[0].Description = upcomingDescriptionExample
	result.Events[0].Accounts[0].Description = upcomingDescriptionExample
	var pretty bytes.Buffer
	if err := WriteUpcomingLifecycleResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, format := range []Format{FormatJSON, FormatCSV} {
		t.Run(string(format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteUpcomingLifecycleResult(&buf, format, result); err != nil {
				t.Fatal(err)
			}
			if format == FormatJSON {
				var decoded awshealth.Result
				if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Events[0].Description != upcomingDescriptionExample || decoded.Events[0].Accounts[0].Description != upcomingDescriptionExample {
					t.Fatal("JSON lost the full descriptions")
				}
				return
			}
			rows, err := csv.NewReader(&buf).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || rows[1][9] != upcomingDescriptionExample {
				t.Fatal("CSV lost the full description:", rows)
			}
		})
	}
}
