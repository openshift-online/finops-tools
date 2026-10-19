package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/openshift-online/finops-tools/core/cost"
)

func sampleAccountDetails() accountreview.AccountDetails {
	return accountreview.AccountDetails{
		AccountID:    "111111111111",
		AccountName:  "test-account",
		DisplayAlias: "my-linked",
		OUPath:       "Root / Sandbox",
		OwnerEmail:   "jdoe@redhat.com",
		Tags:         []coreaccount.Tag{{Key: "owner", Value: "jdoe"}},
		GeneratedAt:  time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		MonthlyCosts: accountreview.MonthlyCostsDetails{
			Currency: "USD",
			Months:   []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 1234.5}},
			Total:    1234.5,
			TopServices: []accountreview.ServiceCost{
				{Service: "AmazonEC2", Amount: 100},
			},
		},
		EC2Instances: []accountreview.EC2Detail{{
			InstanceID: "i-abc",
			Name:       "web",
			Type:       "t3.micro",
			State:      "running",
			Region:     "us-east-1",
		}},
		RDSClusters: []accountreview.RDSClusterDetail{{
			ClusterID: "cluster-1",
			Engine:    "aurora-postgresql",
			Status:    "available",
			Region:    "us-east-1",
		}},
		OpenShiftClusters: []accountreview.OpenShiftClusterDetail{{
			Environment:      "Production",
			Name:             "prod-cluster",
			ClusterID:        "ocm-prod-1",
			ProductType:      "ROSA Classic",
			State:            "ready",
			Region:           "us-east-1",
			OpenShiftVersion: "4.16.0",
		}},
		ResourceCounts: accountreview.ResourceCounts{
			UnattachedEBS: 2,
			VPCs:          1,
		},
		InventoryError: "us-west-2: denied",
	}
}

func TestWriteAccountDetailsPretty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAccountDetails(&buf, FormatPrettyPrint, []accountreview.AccountDetails{sampleAccountDetails()}); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	for _, want := range []string{
		"test-account (111111111111)",
		"Alias",
		"my-linked",
		"OU",
		"Root / Sandbox",
		"Owner",
		"jdoe@redhat.com",
		"Tags",
		"TAG",
		"VALUE",
		"owner",
		"Monthly costs",
		"MONTH",
		"AMOUNT",
		"2026-01",
		"USD 1,234.50",
		"Last listed month",
		"Period total",
		"AmazonEC2",
		"EC2 instances",
		"i-abc",
		"cluster-1",
		"Other resources",
		"Unattached EBS volumes: 2",
		"VPCs: 1",
		"OpenShift clusters",
		"prod-cluster",
		"ocm-prod-1",
		"Warnings",
		"Inventory: us-west-2: denied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty missing %q\n%s", want, out)
		}
	}
	for _, skip := range []string{"RDS instances (0)", "Route53 hosted zones (0)", "Load balancers:", "None found (scanned):"} {
		if strings.Contains(out, skip) {
			t.Errorf("pretty should not use empty tables, zero count rows, or none-found after a scan error for %q\n%s", skip, out)
		}
	}
	// Single-account output should not show a multi-account index prefix.
	if strings.Contains(out, "[1/1]") {
		t.Fatalf("unexpected index on single account:\n%s", out)
	}
}

func TestWriteAccountDetailsPrettyMultiAccountSeparators(t *testing.T) {
	var buf bytes.Buffer
	details := []accountreview.AccountDetails{
		{
			AccountID:   "111111111111",
			AccountName: "first",
			OwnerEmail:  "jdoe@redhat.com",
			MonthlyCosts: accountreview.MonthlyCostsDetails{
				Currency: "USD",
				Months:   []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 10}},
				Total:    10,
			},
		},
		{AccountID: "222222222222", AccountName: "second"},
	}
	if err := WriteAccountDetails(&buf, FormatPrettyPrint, details); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	for _, want := range []string{
		"Accounts in this review",
		"[1/2] first (111111111111)",
		"[2/2] second (222222222222)",
		"═",
		"─",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty missing %q\n%s", want, out)
		}
	}
	overview := strings.Index(out, "Accounts in this review")
	id1 := strings.Index(out, "111111111111")
	id2 := strings.Index(out, "222222222222")
	first := strings.Index(out, "[1/2]")
	second := strings.Index(out, "[2/2]")
	if overview < 0 || id1 < 0 || id2 < 0 || first < 0 || second < 0 {
		t.Fatalf("missing overview or account banners:\n%s", out)
	}
	if overview >= id1 || overview >= id2 || id1 >= first || id2 >= first || first >= second {
		t.Fatalf("overview should list both IDs before the first account banner: overview=%d id1=%d id2=%d first=%d second=%d\n%s", overview, id1, id2, first, second, out)
	}
	if !strings.Contains(out[first:second], "═") {
		t.Fatalf("expected separator between accounts:\n%s", out)
	}
}

func TestWriteAccountDetailsPrettyListsEmptyInventory(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{
		AccountID:      "111111111111",
		AccountName:    "test-account",
		ResourceCounts: accountreview.ResourceCounts{VPCs: 3},
	}
	if err := WriteAccountDetails(&buf, FormatPrettyPrint, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if strings.Contains(out, "EC2 instances (0)") {
		t.Fatalf("empty EC2 should not be a table heading:\n%s", out)
	}
	if !strings.Contains(out, "VPCs: 3") {
		t.Fatalf("nonzero count missing:\n%s", out)
	}
	noneIdx := strings.Index(out, "None found (scanned):")
	vpcIdx := strings.Index(out, "VPCs: 3")
	if noneIdx < 0 || vpcIdx < 0 || vpcIdx > noneIdx {
		t.Fatalf("none-found should follow found counts: vpc=%d none=%d\n%s", vpcIdx, noneIdx, out)
	}
	if !strings.Contains(out[noneIdx:], "EC2 instances") || !strings.Contains(out[noneIdx:], "Unattached EBS volumes") {
		t.Fatalf("none-found line missing scanned-empty types:\n%s", out)
	}
}

func TestWriteAccountDetailsPrettyEmptyResources(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{AccountID: "111111111111", AccountName: "test-account"}
	if err := WriteAccountDetails(&buf, FormatPrettyPrint, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "Resources") {
		t.Fatalf("resources section missing:\n%s", out)
	}
	if !strings.Contains(out, "None found (scanned):") {
		t.Fatalf("none-found line missing:\n%s", out)
	}
	if !strings.Contains(out, "EC2 instances") || !strings.Contains(out, "VPCs") {
		t.Fatalf("empty inventory should still name scanned types:\n%s", out)
	}
}

func TestWriteAccountDetailsTableColoredHeadersKeepSGR(t *testing.T) {
	var buf bytes.Buffer
	s := styler{enabled: true}
	err := writeAccountDetailsTable(&buf, s, "Tags", []string{"Tag", "Value"}, [][]string{
		{"app-code", "OSD-002"},
		{"service-phase", "dev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if strings.Contains(raw, "\033[1M") || strings.Contains(raw, "\033[0M") {
		t.Fatalf("AutoFormatHeaders mangled SGR into Delete Line CSI:\n%q", raw)
	}
	if !strings.Contains(raw, "\033[1mTAG\033[0m") {
		t.Fatalf("expected bold TAG header, got:\n%q", raw)
	}
	if !strings.Contains(raw, "\033[1mVALUE\033[0m") {
		t.Fatalf("expected bold VALUE header, got:\n%q", raw)
	}
	out := stripANSI(raw)
	if !strings.Contains(out, "TAG") || !strings.Contains(out, "VALUE") {
		t.Fatalf("headers missing after strip:\n%s", out)
	}
	if !strings.Contains(out, "app-code") || !strings.Contains(out, "OSD-002") {
		t.Fatalf("rows missing:\n%s", out)
	}
}

func TestWriteAccountDetailsPrettyEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAccountDetails(&buf, FormatPrettyPrint, nil); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "No accounts matched the selection.") {
		t.Fatalf("empty pretty = %q", out)
	}
}

func TestWriteAccountDetailsJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAccountDetails(&buf, FormatJSON, []accountreview.AccountDetails{sampleAccountDetails()}); err != nil {
		t.Fatal(err)
	}
	var got []accountreview.AccountDetails
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(got) != 1 || got[0].AccountID != "111111111111" {
		t.Fatalf("decoded = %+v", got)
	}
	if !strings.Contains(buf.String(), `"unattached_ebs": 2`) {
		t.Fatalf("json missing counts:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), `"load_balancers"`) || strings.Contains(buf.String(), `"unassociated_eips"`) {
		t.Fatalf("json should omit zero counts after an incomplete scan:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "1,234.50") {
		t.Fatalf("json should not pretty-format money:\n%s", buf.String())
	}
}

func TestWriteAccountDetailsJSONEmptyIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAccountDetails(&buf, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("empty json = %q", buf.String())
	}
}

func TestWriteAccountDetailsJSONCompleteScanIncludesZeroCounts(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{
		AccountID:      "111111111111",
		AccountName:    "test-account",
		ResourceCounts: accountreview.ResourceCounts{VPCs: 3},
	}
	if err := WriteAccountDetails(&buf, FormatJSON, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"vpcs": 3`) {
		t.Fatalf("nonzero count missing:\n%s", out)
	}
	if !strings.Contains(out, `"load_balancers": 0`) {
		t.Fatalf("complete scan should emit zero counts:\n%s", out)
	}
}

func TestWriteAccountDetailsCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAccountDetails(&buf, FormatCSV, []accountreview.AccountDetails{sampleAccountDetails()}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"account_id,account_name,owner_email,section,id,name,attr1,attr2,attr3,attr4,attr5,amount,currency",
		"111111111111,test-account,jdoe@redhat.com,account,111111111111,test-account,my-linked,Root / Sandbox",
		",tag,,owner,jdoe",
		",month,,2026-01,,,,,,1234.5,USD",
		",month,,Total,,,,,,1234.5,USD",
		",top_service,,AmazonEC2,,,,,,100,USD",
		",ec2,i-abc,web,t3.micro,running,us-east-1",
		",rds_cluster,cluster-1,aurora-postgresql,available,us-east-1",
		",openshift,ocm-prod-1,prod-cluster,Production,",
		"ocm-prod-1",
		"4.16.0",
		",inventory_error,,,us-west-2: denied",
		",count,unattached_ebs,Unattached EBS volumes,2",
		",count,vpcs,VPCs,1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("csv missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "1,234.50") {
		t.Fatalf("csv should not pretty-format money:\n%s", out)
	}
	for _, skip := range []string{",count,load_balancers,", ",count,unassociated_eips,"} {
		if strings.Contains(out, skip) {
			t.Errorf("csv should omit zero counts after an incomplete scan for %q\n%s", skip, out)
		}
	}
}

func TestWriteAccountDetailsCSVCompleteScanIncludesZeroCounts(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{
		AccountID:      "111111111111",
		AccountName:    "test-account",
		ResourceCounts: accountreview.ResourceCounts{VPCs: 3},
	}
	if err := WriteAccountDetails(&buf, FormatCSV, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, ",count,vpcs,VPCs,3") {
		t.Fatalf("nonzero count missing:\n%s", out)
	}
	if !strings.Contains(out, ",count,load_balancers,Load balancers,0") {
		t.Fatalf("complete scan should still emit zero counts:\n%s", out)
	}
	if strings.Contains(out, ",inventory_error,") {
		t.Fatalf("complete scan should not emit inventory_error:\n%s", out)
	}
}

func TestWriteAccountDetailsCSVOwnerErrorSection(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "test-account",
		OwnerError:  "owner tag is empty",
	}
	if err := WriteAccountDetails(&buf, FormatCSV, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, ",owner_error,,,owner tag is empty") {
		t.Fatalf("owner_error section missing:\n%s", out)
	}
	accountLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, ",account,") {
			accountLine = line
			break
		}
	}
	if accountLine == "" || strings.Contains(accountLine, "owner tag is empty") {
		t.Fatalf("owner_error should not be unlabeled attr3 on the account row:\n%s", out)
	}
}

func TestWriteAccountDetailsCSVSanitizesFormula(t *testing.T) {
	var buf bytes.Buffer
	d := accountreview.AccountDetails{
		AccountID:   "111111111111",
		AccountName: "=cmd",
		Tags:        []coreaccount.Tag{{Key: "owner", Value: "+1"}},
	}
	if err := WriteAccountDetails(&buf, FormatCSV, []accountreview.AccountDetails{d}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "'=cmd") {
		t.Fatalf("account name not sanitized:\n%s", out)
	}
	if !strings.Contains(out, "'+1") {
		t.Fatalf("tag value not sanitized:\n%s", out)
	}
}

func TestWriteAccountDetailsUnknownFormat(t *testing.T) {
	if err := WriteAccountDetails(&bytes.Buffer{}, Format("yaml"), nil); err == nil {
		t.Fatal("expected error")
	}
}
