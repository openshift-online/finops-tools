package account

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/organizations"
)

// organizationsMigrateStub provides not-implemented defaults for migrate-related OrganizationsAPI methods.
// Embed in test fakes so existing fakes compile when the interface grows.
type organizationsMigrateStub struct{}

func (organizationsMigrateStub) InviteAccountToOrganization(
	context.Context,
	*organizations.InviteAccountToOrganizationInput,
	...func(*organizations.Options),
) (*organizations.InviteAccountToOrganizationOutput, error) {
	return nil, errors.New("not implemented")
}

func (organizationsMigrateStub) ListHandshakesForAccount(
	context.Context,
	*organizations.ListHandshakesForAccountInput,
	...func(*organizations.Options),
) (*organizations.ListHandshakesForAccountOutput, error) {
	return nil, errors.New("not implemented")
}

func (organizationsMigrateStub) AcceptHandshake(
	context.Context,
	*organizations.AcceptHandshakeInput,
	...func(*organizations.Options),
) (*organizations.AcceptHandshakeOutput, error) {
	return nil, errors.New("not implemented")
}

func (organizationsMigrateStub) ListParents(
	context.Context,
	*organizations.ListParentsInput,
	...func(*organizations.Options),
) (*organizations.ListParentsOutput, error) {
	return nil, errors.New("not implemented")
}

func (organizationsMigrateStub) MoveAccount(
	context.Context,
	*organizations.MoveAccountInput,
	...func(*organizations.Options),
) (*organizations.MoveAccountOutput, error) {
	return nil, errors.New("not implemented")
}
