package account

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

type fakeMigrateOrganizations struct {
	memberIDs map[string]struct{}

	inviteAccountID string
	inviteNotes     string
	inviteHandshake *types.Handshake
	inviteErr       error

	handshakes []types.Handshake
	listHSErr  error

	acceptHandshakeID string
	acceptHandshake   *types.Handshake
	acceptErr         error

	parentsByChild map[string]string
	listParentsErr error

	movedAccountID string
	movedSource    string
	movedDest      string
	moveErr        error
}

func (f *fakeMigrateOrganizations) ListAccounts(
	_ context.Context,
	_ *organizations.ListAccountsInput,
	_ ...func(*organizations.Options),
) (*organizations.ListAccountsOutput, error) {
	accounts := make([]types.Account, 0, len(f.memberIDs))
	for id := range f.memberIDs {
		accounts = append(accounts, types.Account{
			Id:     aws.String(id),
			Name:   aws.String(id),
			Status: types.AccountStatusActive,
		})
	}
	return &organizations.ListAccountsOutput{Accounts: accounts}, nil
}

func (f *fakeMigrateOrganizations) DescribeAccount(
	context.Context,
	*organizations.DescribeAccountInput,
	...func(*organizations.Options),
) (*organizations.DescribeAccountOutput, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) ListTagsForAccount(
	context.Context, string, *string,
) ([]Tag, *string, error) {
	return nil, nil, errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) SetAccountTag(context.Context, string, string, string) error {
	return errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) DescribeOrganization(
	context.Context,
	*organizations.DescribeOrganizationInput,
	...func(*organizations.Options),
) (*organizations.DescribeOrganizationOutput, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) ListRoots(
	context.Context,
	*organizations.ListRootsInput,
	...func(*organizations.Options),
) (*organizations.ListRootsOutput, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) ListOrganizationalUnitsForParent(
	context.Context,
	*organizations.ListOrganizationalUnitsForParentInput,
	...func(*organizations.Options),
) (*organizations.ListOrganizationalUnitsForParentOutput, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) ListAccountsForParent(
	context.Context,
	*organizations.ListAccountsForParentInput,
	...func(*organizations.Options),
) (*organizations.ListAccountsForParentOutput, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMigrateOrganizations) InviteAccountToOrganization(
	_ context.Context,
	params *organizations.InviteAccountToOrganizationInput,
	_ ...func(*organizations.Options),
) (*organizations.InviteAccountToOrganizationOutput, error) {
	if params != nil && params.Target != nil {
		f.inviteAccountID = aws.ToString(params.Target.Id)
	}
	if params != nil {
		f.inviteNotes = aws.ToString(params.Notes)
	}
	if f.inviteErr != nil {
		return nil, f.inviteErr
	}
	return &organizations.InviteAccountToOrganizationOutput{Handshake: f.inviteHandshake}, nil
}

func (f *fakeMigrateOrganizations) ListHandshakesForAccount(
	_ context.Context,
	_ *organizations.ListHandshakesForAccountInput,
	_ ...func(*organizations.Options),
) (*organizations.ListHandshakesForAccountOutput, error) {
	if f.listHSErr != nil {
		return nil, f.listHSErr
	}
	return &organizations.ListHandshakesForAccountOutput{Handshakes: f.handshakes}, nil
}

func (f *fakeMigrateOrganizations) AcceptHandshake(
	_ context.Context,
	params *organizations.AcceptHandshakeInput,
	_ ...func(*organizations.Options),
) (*organizations.AcceptHandshakeOutput, error) {
	f.acceptHandshakeID = aws.ToString(params.HandshakeId)
	if f.acceptErr != nil {
		return nil, f.acceptErr
	}
	hs := f.acceptHandshake
	if hs == nil {
		hs = &types.Handshake{
			Id:    params.HandshakeId,
			State: types.HandshakeStateAccepted,
		}
	}
	return &organizations.AcceptHandshakeOutput{Handshake: hs}, nil
}

func (f *fakeMigrateOrganizations) ListParents(
	_ context.Context,
	params *organizations.ListParentsInput,
	_ ...func(*organizations.Options),
) (*organizations.ListParentsOutput, error) {
	if f.listParentsErr != nil {
		return nil, f.listParentsErr
	}
	child := aws.ToString(params.ChildId)
	parentID, ok := f.parentsByChild[child]
	if !ok {
		return &organizations.ListParentsOutput{}, nil
	}
	return &organizations.ListParentsOutput{
		Parents: []types.Parent{{Id: aws.String(parentID), Type: types.ParentTypeOrganizationalUnit}},
	}, nil
}

func (f *fakeMigrateOrganizations) MoveAccount(
	_ context.Context,
	params *organizations.MoveAccountInput,
	_ ...func(*organizations.Options),
) (*organizations.MoveAccountOutput, error) {
	f.movedAccountID = aws.ToString(params.AccountId)
	f.movedSource = aws.ToString(params.SourceParentId)
	f.movedDest = aws.ToString(params.DestinationParentId)
	if f.moveErr != nil {
		return nil, f.moveErr
	}
	return &organizations.MoveAccountOutput{}, nil
}

func TestInviteAccountWithClient(t *testing.T) {
	client := &fakeMigrateOrganizations{
		inviteHandshake: &types.Handshake{
			Id:    aws.String("h-abcdefgh"),
			State: types.HandshakeStateRequested,
		},
	}
	got, err := inviteAccountWithClient(context.Background(), client, "111111111111", "finops migrate")
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if got.HandshakeID != "h-abcdefgh" {
		t.Fatalf("handshake ID = %q", got.HandshakeID)
	}
	if client.inviteAccountID != "111111111111" {
		t.Fatalf("invited account = %q", client.inviteAccountID)
	}
	if client.inviteNotes != "finops migrate" {
		t.Fatalf("notes = %q", client.inviteNotes)
	}
}

func TestInviteAccountWithClientRejectsInvalidID(t *testing.T) {
	_, err := inviteAccountWithClient(context.Background(), &fakeMigrateOrganizations{}, "not-an-id", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAcceptInviteHandshakeUsesProvidedID(t *testing.T) {
	client := &fakeMigrateOrganizations{}
	got, err := acceptInviteHandshakeWithClient(context.Background(), client, "h-12345678")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if client.acceptHandshakeID != "h-12345678" {
		t.Fatalf("accepted = %q", client.acceptHandshakeID)
	}
	if got.State != string(types.HandshakeStateAccepted) {
		t.Fatalf("state = %q", got.State)
	}
}

func TestAcceptInviteHandshakeFindsOpenInvite(t *testing.T) {
	client := &fakeMigrateOrganizations{
		handshakes: []types.Handshake{
			{Id: aws.String("h-expired01"), Action: types.ActionTypeInviteAccountToOrganization, State: types.HandshakeStateExpired},
			{Id: aws.String("h-openinv01"), Action: types.ActionTypeInviteAccountToOrganization, State: types.HandshakeStateRequested},
		},
	}
	got, err := acceptInviteHandshakeWithClient(context.Background(), client, "")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got.HandshakeID != "h-openinv01" {
		t.Fatalf("handshake = %q", got.HandshakeID)
	}
	if client.acceptHandshakeID != "h-openinv01" {
		t.Fatalf("accepted = %q", client.acceptHandshakeID)
	}
}

func TestOrganizationContainsAccountWithClient(t *testing.T) {
	client := &fakeMigrateOrganizations{
		memberIDs: map[string]struct{}{
			"111111111111": {},
			"222222222222": {},
		},
	}
	ok, err := organizationContainsAccountWithClient(context.Background(), client, "111111111111")
	if err != nil || !ok {
		t.Fatalf("contains = %v, err = %v", ok, err)
	}
	ok, err = organizationContainsAccountWithClient(context.Background(), client, "333333333333")
	if err != nil || ok {
		t.Fatalf("contains missing = %v, err = %v", ok, err)
	}
}

func TestMoveAccountToParentWithClient(t *testing.T) {
	client := &fakeMigrateOrganizations{
		parentsByChild: map[string]string{
			"111111111111": "ou-abcd-source01",
		},
	}
	if err := moveAccountToParentWithClient(context.Background(), client, "111111111111", "ou-abcd-dest0001"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if client.movedAccountID != "111111111111" || client.movedSource != "ou-abcd-source01" || client.movedDest != "ou-abcd-dest0001" {
		t.Fatalf("move args account=%s source=%s dest=%s", client.movedAccountID, client.movedSource, client.movedDest)
	}
}

func TestMoveAccountToParentNoopWhenSameParent(t *testing.T) {
	client := &fakeMigrateOrganizations{
		parentsByChild: map[string]string{
			"111111111111": "ou-abcd-same0001",
		},
	}
	if err := moveAccountToParentWithClient(context.Background(), client, "111111111111", "ou-abcd-same0001"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if client.movedAccountID != "" {
		t.Fatal("expected no MoveAccount call")
	}
}
