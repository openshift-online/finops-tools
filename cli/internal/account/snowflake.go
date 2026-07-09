package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/snowflakecred"
	"github.com/openshift-online/finops-tools/cli/internal/snowflakeoauth"
	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

// SnowflakeAccountSettings holds non-secret Snowflake connection options.
type SnowflakeAccountSettings struct {
	Account   string
	Role      string
	Warehouse string
	Database  string
	Schema    string
}

// SnowflakeLoginFunc obtains or refreshes OAuth tokens (injectable for tests).
type SnowflakeLoginFunc func(context.Context, snowflakeoauth.ClientConfig, snowflakeoauth.TokenSet) (snowflakeoauth.TokenSet, error)

// AddSnowflakeOptions configures AddSnowflake.
type AddSnowflakeOptions struct {
	Account       SnowflakeAccountSettings
	Alias         string
	OAuth         snowflakeoauth.ClientConfig
	TokensPath    string
	Login         SnowflakeLoginFunc
	ExistingToken snowflakeoauth.TokenSet
	ForceLogin    bool
}

// AddSnowflake logs in via Red Hat SSO OAuth and verifies Snowflake connectivity.
func AddSnowflake(ctx context.Context, opts AddSnowflakeOptions) (AddResult, error) {
	alias := strings.TrimSpace(opts.Alias)
	if alias == "" {
		alias = strings.TrimSpace(opts.Account.Account)
	}
	acct := opts.Account
	acct.Account = strings.TrimSpace(acct.Account)
	if acct.Account == "" {
		return AddResult{}, fmt.Errorf("snowflake account identifier is required")
	}

	login := opts.Login
	if login == nil {
		login = defaultSnowflakeLogin
	}

	refreshed := false
	tok := opts.ExistingToken
	if opts.ForceLogin || !tok.Valid() {
		var err error
		tok, err = login(ctx, opts.OAuth, tok)
		if err != nil {
			return AddResult{}, err
		}
		refreshed = true
	}

	tokensPath := opts.TokensPath
	if tokensPath == "" {
		var err error
		tokensPath, err = snowflakecred.DefaultTokensPath()
		if err != nil {
			return AddResult{}, err
		}
	}
	file, err := snowflakecred.Load(tokensPath)
	if err != nil {
		return AddResult{}, err
	}
	file.Set(alias, tok)
	if err := snowflakecred.Save(tokensPath, file); err != nil {
		return AddResult{}, err
	}

	claims, err := snowflakeoauth.ValidateDataverseToken(tok.AccessToken, opts.OAuth.Audience)
	if err != nil {
		return AddResult{}, fmt.Errorf("%w (decode at https://jwt.io to inspect iss, aud, and scope)", err)
	}
	loginName := claims.SnowflakeLoginName()
	if loginName == "" {
		return AddResult{}, fmt.Errorf("access token has no preferred_username or email claim for Snowflake login mapping")
	}

	db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account:   acct.Account,
		User:      loginName,
		Token:     tok.AccessToken,
		Role:      acct.Role,
		Warehouse: acct.Warehouse,
		Database:  acct.Database,
		Schema:    acct.Schema,
	})
	if err != nil {
		return AddResult{}, err
	}
	defer func() {
		_ = db.Close()
	}()

	if err := coresnowflake.Ping(ctx, db); err != nil {
		return AddResult{}, fmt.Errorf(
			"snowflake connection check: %w (if the token looks valid at jwt.io, ask a Snowflake admin to run SELECT SYSTEM$GET_LOGIN_FAILURE_DETAILS('<uuid>') with the error UUID)",
			err,
		)
	}

	return AddResult{
		Provider:  ProviderSnowflake,
		AccountID: acct.Account,
		Profile:   alias,
		Refreshed: refreshed,
	}, nil
}

func defaultSnowflakeLogin(ctx context.Context, oauthCfg snowflakeoauth.ClientConfig, existing snowflakeoauth.TokenSet) (snowflakeoauth.TokenSet, error) {
	if existing.Valid() {
		return existing, nil
	}
	if strings.TrimSpace(existing.RefreshToken) != "" {
		tok, err := snowflakeoauth.Refresh(ctx, oauthCfg, existing.RefreshToken)
		if err == nil && tok.Valid() {
			return tok, nil
		}
	}
	return snowflakeoauth.Login(ctx, oauthCfg)
}
