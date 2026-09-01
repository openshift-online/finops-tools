package accountreview

import (
	"errors"
	"testing"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

func TestResolveOwnerEmail(t *testing.T) {
	tags := []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}

	email, err := ResolveOwnerEmail(tags, "")
	if err != nil {
		t.Fatalf("ResolveOwnerEmail() error = %v", err)
	}
	if email != "jdoe@redhat.com" {
		t.Fatalf("email = %q, want jdoe@redhat.com", email)
	}
}

func TestResolveOwnerEmailAlreadyQualified(t *testing.T) {
	tags := []coreaccount.Tag{{Key: "owner", Value: "joe@redhat.com"}}

	email, err := ResolveOwnerEmail(tags, "")
	if err != nil {
		t.Fatalf("ResolveOwnerEmail() error = %v", err)
	}
	if email != "joe@redhat.com" {
		t.Fatalf("email = %q", email)
	}
}

func TestResolveOwnerEmailRFC5322DisplayName(t *testing.T) {
	tags := []coreaccount.Tag{{Key: "owner", Value: "Jane Doe <JDOE@RedHat.com>"}}

	email, err := ResolveOwnerEmail(tags, "")
	if err != nil {
		t.Fatalf("ResolveOwnerEmail() error = %v", err)
	}
	if email != "jdoe@redhat.com" {
		t.Fatalf("email = %q, want jdoe@redhat.com", email)
	}
}

func TestResolveOwnerEmailMissingTag(t *testing.T) {
	_, err := ResolveOwnerEmail(nil, "")
	if !errors.Is(err, ErrOwnerTagMissing) {
		t.Fatalf("error = %v, want ErrOwnerTagMissing", err)
	}
}

func TestResolveOwnerEmailEmptyValue(t *testing.T) {
	tags := []coreaccount.Tag{{Key: "owner", Value: "  "}}
	_, err := ResolveOwnerEmail(tags, "")
	if !errors.Is(err, ErrOwnerTagEmpty) {
		t.Fatalf("error = %v, want ErrOwnerTagEmpty", err)
	}
}

func TestGroupReportsByAccount(t *testing.T) {
	reports := []AccountReport{
		{AccountID: "111111111111", OwnerEmail: "a@redhat.com"},
		{AccountID: "222222222222", OwnerError: ErrOwnerTagMissing.Error()},
	}
	groups, failures := GroupReports(reports, GroupByAccount)
	if len(groups) != 1 || groups[0].Reports[0].AccountID != "111111111111" {
		t.Fatalf("groups = %+v", groups)
	}
	if len(failures) != 1 || failures[0].AccountID != "222222222222" {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestGroupReportsByOwner(t *testing.T) {
	reports := []AccountReport{
		{AccountID: "111111111111", OwnerEmail: "a@redhat.com"},
		{AccountID: "222222222222", OwnerEmail: "a@redhat.com"},
		{AccountID: "333333333333", OwnerError: ErrOwnerTagMissing.Error()},
	}
	groups, failures := GroupReports(reports, GroupByOwner)
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if len(groups[0].Reports) != 2 {
		t.Fatalf("group reports = %d, want 2", len(groups[0].Reports))
	}
	if len(failures) != 1 {
		t.Fatalf("failures = %d, want 1", len(failures))
	}
}
