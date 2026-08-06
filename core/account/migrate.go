package account

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

var (
	accountIDPattern   = regexp.MustCompile(`^\d{12}$`)
	handshakeIDPattern = regexp.MustCompile(`^h-[0-9a-z]{8,32}$`)
)

// InviteResult is the handshake created by InviteAccountToOrganization.
type InviteResult struct {
	HandshakeID string
	State       string
}

// InviteAccount invites accountID to join the organization owned by cfg (destination management account).
func InviteAccount(ctx context.Context, cfg aws.Config, accountID, notes string) (InviteResult, error) {
	return inviteAccountWithClient(ctx, newOrganizationsClient(cfg), accountID, notes)
}

func inviteAccountWithClient(ctx context.Context, client OrganizationsAPI, accountID, notes string) (InviteResult, error) {
	accountID = strings.TrimSpace(accountID)
	if err := validateAccountID(accountID); err != nil {
		return InviteResult{}, err
	}

	input := &organizations.InviteAccountToOrganizationInput{
		Target: &types.HandshakeParty{
			Id:   aws.String(accountID),
			Type: types.HandshakePartyTypeAccount,
		},
	}
	if notes = strings.TrimSpace(notes); notes != "" {
		input.Notes = aws.String(notes)
	}

	out, err := client.InviteAccountToOrganization(ctx, input)
	if err != nil {
		return InviteResult{}, fmt.Errorf("invite account %s to organization: %w", accountID, err)
	}
	if out.Handshake == nil {
		return InviteResult{}, fmt.Errorf("invite account %s: empty handshake in response", accountID)
	}
	handshakeID := strings.TrimSpace(aws.ToString(out.Handshake.Id))
	if handshakeID == "" {
		return InviteResult{}, fmt.Errorf("invite account %s: missing handshake ID", accountID)
	}
	return InviteResult{
		HandshakeID: handshakeID,
		State:       string(out.Handshake.State),
	}, nil
}

// AcceptInviteHandshake accepts an INVITE handshake using member-account credentials in cfg.
// When handshakeID is empty, the first open INVITE handshake for the account is accepted.
func AcceptInviteHandshake(ctx context.Context, cfg aws.Config, handshakeID string) (InviteResult, error) {
	return acceptInviteHandshakeWithClient(ctx, newOrganizationsClient(cfg), handshakeID)
}

func acceptInviteHandshakeWithClient(ctx context.Context, client OrganizationsAPI, handshakeID string) (InviteResult, error) {
	handshakeID = strings.TrimSpace(handshakeID)
	if handshakeID == "" {
		id, err := findOpenInviteHandshakeIDWithClient(ctx, client)
		if err != nil {
			return InviteResult{}, err
		}
		handshakeID = id
	} else if !handshakeIDPattern.MatchString(handshakeID) {
		return InviteResult{}, fmt.Errorf("invalid handshake ID %q", handshakeID)
	}

	out, err := client.AcceptHandshake(ctx, &organizations.AcceptHandshakeInput{
		HandshakeId: aws.String(handshakeID),
	})
	if err != nil {
		return InviteResult{}, fmt.Errorf("accept handshake %s: %w", handshakeID, err)
	}
	result := InviteResult{HandshakeID: handshakeID}
	if out.Handshake != nil {
		if id := strings.TrimSpace(aws.ToString(out.Handshake.Id)); id != "" {
			result.HandshakeID = id
		}
		result.State = string(out.Handshake.State)
	}
	return result, nil
}

func findOpenInviteHandshakeIDWithClient(ctx context.Context, client OrganizationsAPI) (string, error) {
	var token *string
	for {
		out, err := client.ListHandshakesForAccount(ctx, &organizations.ListHandshakesForAccountInput{
			Filter: &types.HandshakeFilter{
				ActionType: types.ActionTypeInviteAccountToOrganization,
			},
			NextToken: token,
		})
		if err != nil {
			return "", fmt.Errorf("list handshakes for account: %w", err)
		}
		for _, hs := range out.Handshakes {
			if hs.Action != types.ActionTypeInviteAccountToOrganization {
				continue
			}
			switch hs.State {
			case types.HandshakeStateRequested, types.HandshakeStateOpen:
				id := strings.TrimSpace(aws.ToString(hs.Id))
				if id != "" {
					return id, nil
				}
			}
		}
		if out.NextToken == nil || aws.ToString(out.NextToken) == "" {
			break
		}
		token = out.NextToken
	}
	return "", fmt.Errorf("no open INVITE handshake found for this account")
}

// OrganizationContainsAccount reports whether accountID is an ACTIVE member of the organization for cfg.
func OrganizationContainsAccount(ctx context.Context, cfg aws.Config, accountID string) (bool, error) {
	return organizationContainsAccountWithClient(ctx, newOrganizationsClient(cfg), accountID)
}

func organizationContainsAccountWithClient(ctx context.Context, client OrganizationsAPI, accountID string) (bool, error) {
	accountID = strings.TrimSpace(accountID)
	if err := validateAccountID(accountID); err != nil {
		return false, err
	}
	out, err := client.DescribeAccount(ctx, &organizations.DescribeAccountInput{
		AccountId: aws.String(accountID),
	})
	if err != nil {
		var notFound *types.AccountNotFoundException
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("describe account %s: %w", accountID, err)
	}
	if out.Account == nil {
		return false, nil
	}
	return out.Account.Status == types.AccountStatusActive, nil
}

// AccountParentID returns the current parent root or OU ID for accountID (payer credentials).
func AccountParentID(ctx context.Context, cfg aws.Config, accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if err := validateAccountID(accountID); err != nil {
		return "", err
	}
	return accountParentIDWithClient(ctx, newOrganizationsClient(cfg), accountID)
}

// VerifyParentExists checks that parentID (OU or organization root) exists in the organization for cfg.
func VerifyParentExists(ctx context.Context, cfg aws.Config, parentID string) error {
	return verifyParentExistsWithClient(ctx, newOrganizationsClient(cfg), parentID)
}

func verifyParentExistsWithClient(ctx context.Context, client OrganizationsAPI, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	if err := ValidateParentID(parentID); err != nil {
		return err
	}
	_, err := client.ListAccountsForParent(ctx, &organizations.ListAccountsForParentInput{
		ParentId:   aws.String(parentID),
		MaxResults: aws.Int32(1),
	})
	if err != nil {
		var notFound *types.ParentNotFoundException
		if errors.As(err, &notFound) {
			return fmt.Errorf("parent %s not found in organization", parentID)
		}
		return fmt.Errorf("verify parent %s: %w", parentID, err)
	}
	return nil
}

// MoveAccountToParent moves accountID to destinationParentID within the same organization (payer credentials).
func MoveAccountToParent(ctx context.Context, cfg aws.Config, accountID, destinationParentID string) error {
	return moveAccountToParentWithClient(ctx, newOrganizationsClient(cfg), accountID, destinationParentID)
}

func moveAccountToParentWithClient(ctx context.Context, client OrganizationsAPI, accountID, destinationParentID string) error {
	accountID = strings.TrimSpace(accountID)
	destinationParentID = strings.TrimSpace(destinationParentID)
	if err := validateAccountID(accountID); err != nil {
		return err
	}
	if err := ValidateParentID(destinationParentID); err != nil {
		return err
	}

	sourceParentID, err := accountParentIDWithClient(ctx, client, accountID)
	if err != nil {
		return err
	}
	if sourceParentID == destinationParentID {
		return nil
	}

	_, err = client.MoveAccount(ctx, &organizations.MoveAccountInput{
		AccountId:           aws.String(accountID),
		SourceParentId:      aws.String(sourceParentID),
		DestinationParentId: aws.String(destinationParentID),
	})
	if err != nil {
		return fmt.Errorf("move account %s to %s: %w", accountID, destinationParentID, err)
	}
	return nil
}

func accountParentIDWithClient(ctx context.Context, client OrganizationsAPI, accountID string) (string, error) {
	out, err := client.ListParents(ctx, &organizations.ListParentsInput{
		ChildId: aws.String(accountID),
	})
	if err != nil {
		return "", fmt.Errorf("list parents for account %s: %w", accountID, err)
	}
	if len(out.Parents) == 0 {
		return "", fmt.Errorf("list parents for account %s: no parent found", accountID)
	}
	parentID := strings.TrimSpace(aws.ToString(out.Parents[0].Id))
	if parentID == "" {
		return "", fmt.Errorf("list parents for account %s: empty parent ID", accountID)
	}
	return parentID, nil
}

func validateAccountID(accountID string) error {
	if !accountIDPattern.MatchString(accountID) {
		return fmt.Errorf("invalid AWS account ID %q (expected 12 digits)", accountID)
	}
	return nil
}
