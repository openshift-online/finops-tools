package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/snowflakecred"
	"github.com/openshift-online/finops-tools/cli/internal/snowflakeoauth"
)

var errSnowflakeTokensNotFound = errors.New("snowflake oauth tokens not found")

type snowflakeTokenSession struct {
	resolveOAuthClient func(secretsPath string) (clientID, clientSecret string, err error)
	loadToken          func(alias, tokensPath string) (snowflakeoauth.TokenSet, error)
	persistToken       func(alias, tokensPath string, tok snowflakeoauth.TokenSet) error
	refresh            func(ctx context.Context, cfg snowflakeoauth.ClientConfig, refreshToken string) (snowflakeoauth.TokenSet, error)
	login              func(ctx context.Context, cfg snowflakeoauth.ClientConfig) (snowflakeoauth.TokenSet, error)
}

func defaultSnowflakeTokenSession() snowflakeTokenSession {
	return snowflakeTokenSession{
		resolveOAuthClient: configstore.ResolveSnowflakeOAuthClient,
		loadToken:          loadSnowflakeToken,
		persistToken:       persistSnowflakeToken,
		refresh:            snowflakeoauth.Refresh,
		login:              snowflakeoauth.Login,
	}
}

func snowflakeOAuthConfig(cfg configstore.File, clientID, clientSecret, ssoEnv string) (snowflakeoauth.ClientConfig, error) {
	issuer, err := snowflakeoauth.IssuerForEnv(ssoEnv)
	if err != nil {
		return snowflakeoauth.ClientConfig{}, err
	}
	if strings.TrimSpace(clientID) == "" {
		return snowflakeoauth.ClientConfig{}, fmt.Errorf(
			"snowflake oauth client_id not configured; run finops config oauth-client set --client-id <id> or set FINOPS_SNOWFLAKE_OAUTH_CLIENT_ID",
		)
	}
	return snowflakeoauth.ClientConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Audience:     cfg.SnowflakeOAuthAudience(),
		Issuer:       issuer,
	}, nil
}

func loadSnowflakeToken(alias, tokensPath string) (snowflakeoauth.TokenSet, error) {
	path := tokensPath
	if path == "" {
		var err error
		path, err = snowflakecred.DefaultTokensPath()
		if err != nil {
			return snowflakeoauth.TokenSet{}, err
		}
	}
	file, err := snowflakecred.Load(path)
	if err != nil {
		return snowflakeoauth.TokenSet{}, err
	}
	tok, ok := file.Get(alias)
	if !ok {
		return snowflakeoauth.TokenSet{}, fmt.Errorf("%w for snowflake alias %q", errSnowflakeTokensNotFound, alias)
	}
	return tok, nil
}

func (s snowflakeTokenSession) ensureAccessToken(
	ctx context.Context,
	cfg configstore.File,
	alias, secretsPath, tokensPath string,
	acct configstore.SnowflakeAccount,
) (snowflakeoauth.TokenSet, snowflakeoauth.ClientConfig, error) {
	clientID, clientSecret, err := s.resolveOAuthClient(secretsPath)
	if err != nil {
		return snowflakeoauth.TokenSet{}, snowflakeoauth.ClientConfig{}, err
	}
	sso := acct.SSO
	if strings.TrimSpace(sso) == "" {
		sso = cfg.SnowflakeSSOIssuer()
	}
	oauthCfg, err := snowflakeOAuthConfig(cfg, clientID, clientSecret, sso)
	if err != nil {
		return snowflakeoauth.TokenSet{}, snowflakeoauth.ClientConfig{}, err
	}

	tok, err := s.loadToken(alias, tokensPath)
	if err != nil && !errors.Is(err, errSnowflakeTokensNotFound) {
		return snowflakeoauth.TokenSet{}, snowflakeoauth.ClientConfig{}, err
	}
	if err == nil && tok.Valid() {
		return tok, oauthCfg, nil
	}
	if err == nil && strings.TrimSpace(tok.RefreshToken) != "" {
		refreshed, err := s.refresh(ctx, oauthCfg, tok.RefreshToken)
		if err == nil && refreshed.Valid() {
			if err := s.persistToken(alias, tokensPath, refreshed); err != nil {
				return snowflakeoauth.TokenSet{}, snowflakeoauth.ClientConfig{}, err
			}
			return refreshed, oauthCfg, nil
		}
	}
	reauthenticated, err := s.login(ctx, oauthCfg)
	if err != nil {
		return snowflakeoauth.TokenSet{}, snowflakeoauth.ClientConfig{}, fmt.Errorf("snowflake oauth login failed for alias %q: %w", alias, err)
	}
	if err := s.persistToken(alias, tokensPath, reauthenticated); err != nil {
		return snowflakeoauth.TokenSet{}, snowflakeoauth.ClientConfig{}, err
	}
	return reauthenticated, oauthCfg, nil
}

func ensureSnowflakeAccessToken(
	ctx context.Context,
	cfg configstore.File,
	alias, secretsPath, tokensPath string,
	acct configstore.SnowflakeAccount,
) (snowflakeoauth.TokenSet, snowflakeoauth.ClientConfig, error) {
	return defaultSnowflakeTokenSession().ensureAccessToken(ctx, cfg, alias, secretsPath, tokensPath, acct)
}

func persistSnowflakeToken(alias, tokensPath string, tok snowflakeoauth.TokenSet) error {
	path := tokensPath
	if path == "" {
		var err error
		path, err = snowflakecred.DefaultTokensPath()
		if err != nil {
			return err
		}
	}
	file, err := snowflakecred.Load(path)
	if err != nil {
		return err
	}
	file.Set(alias, tok)
	return snowflakecred.Save(path, file)
}
