package snowflakeoauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	defaultRedirectPath = "/oauth/callback"
	// DefaultRedirectURI must match the redirect URI registered for the Red Hat SSO client.
	DefaultRedirectURI = "http://127.0.0.1:8765/oauth/callback"
	loginTimeout       = 5 * time.Minute
)

// ClientConfig holds OAuth client settings for Red Hat SSO.
type ClientConfig struct {
	ClientID     string
	ClientSecret string
	Audience     string
	Issuer       IssuerURLs
	RedirectURI  string
}

// TokenSet is an OAuth access token with optional refresh token.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Valid reports whether the access token is present and not expired (with 30s skew).
func (t TokenSet) Valid() bool {
	if strings.TrimSpace(t.AccessToken) == "" {
		return false
	}
	if t.Expiry.IsZero() {
		return true
	}
	return time.Now().Add(30 * time.Second).Before(t.Expiry)
}

// Login obtains tokens via browser authorization code flow with PKCE.
func Login(ctx context.Context, cfg ClientConfig) (TokenSet, error) {
	if strings.TrimSpace(cfg.ClientID) == "" {
		return TokenSet{}, errors.New("oauth client_id is required (set via finops config oauth-client set or FINOPS_SNOWFLAKE_OAUTH_CLIENT_ID)")
	}
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		audience = DefaultAudience
	}
	// Omit scope on the authorize request so Keycloak uses the SSO client's default
	// optional scopes (IAM must assign session:role-any there).
	redirectURI := strings.TrimSpace(cfg.RedirectURI)
	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
	}
	listener, err := listenRedirect(redirectURI)
	if err != nil {
		return TokenSet{}, err
	}
	defer listener.Close()

	oauth2cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirectURI,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.Issuer.AuthorizeURL,
			TokenURL: cfg.Issuer.TokenURL,
		},
	}

	state, err := randomURLSafe(32)
	if err != nil {
		return TokenSet{}, err
	}
	verifier, challenge, err := pkceChallenge()
	if err != nil {
		return TokenSet{}, err
	}

	authOpts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("audience", audience),
	}
	authURL := oauth2cfg.AuthCodeURL(state, authOpts...)

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	resultCh := make(chan tokenResult, 1)
	go func() {
		resultCh <- waitForCallback(ctx, listener, oauth2cfg, state, verifier, audience)
	}()

	if _, err := fmt.Fprintf(os.Stderr, "Open this URL in your browser to sign in with Red Hat SSO:\n\n%s\n\n", authURL); err != nil {
		return TokenSet{}, err
	}
	if err := openBrowser(authURL); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "(Could not open browser automatically: %v)\n", err)
	}

	select {
	case <-ctx.Done():
		return TokenSet{}, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return TokenSet{}, res.err
		}
		return res.token, nil
	}
}

// Refresh exchanges a refresh token for a new access token.
func Refresh(ctx context.Context, cfg ClientConfig, refreshToken string) (TokenSet, error) {
	if strings.TrimSpace(cfg.ClientID) == "" {
		return TokenSet{}, errors.New("oauth client_id is required")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return TokenSet{}, errors.New("refresh token is required")
	}
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		audience = DefaultAudience
	}

	oauth2cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: cfg.Issuer.TokenURL,
		},
	}
	src := oauth2cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := src.Token()
	if err != nil {
		return TokenSet{}, fmt.Errorf("refresh oauth token: %w", err)
	}
	return tokenSetFromOAuth(tok), nil
}

type tokenResult struct {
	token TokenSet
	err   error
}

func waitForCallback(ctx context.Context, listener net.Listener, oauth2cfg *oauth2.Config, state, verifier, audience string) tokenResult {
	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	done := make(chan tokenResult, 1)
	mux.HandleFunc(defaultRedirectPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errMsg := q.Get("error"); errMsg != "" {
			desc := q.Get("error_description")
			done <- tokenResult{err: fmt.Errorf("oauth error %s: %s", errMsg, desc)}
			return
		}
		if q.Get("state") != state {
			done <- tokenResult{err: errors.New("oauth state mismatch")}
			return
		}
		code := q.Get("code")
		if code == "" {
			done <- tokenResult{err: errors.New("oauth code missing")}
			return
		}
		opts := []oauth2.AuthCodeOption{
			oauth2.VerifierOption(verifier),
			oauth2.SetAuthURLParam("audience", audience),
		}
		tok, err := oauth2cfg.Exchange(r.Context(), code, opts...)
		if err != nil {
			done <- tokenResult{err: fmt.Errorf("exchange oauth code: %w", err)}
			return
		}
		_, _ = io.WriteString(w, "<html><body><p>Login successful. You can close this window.</p></body></html>")
		done <- tokenResult{token: tokenSetFromOAuth(tok)}
	})

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			done <- tokenResult{err: fmt.Errorf("oauth callback server: %w", err)}
		}
	}()

	select {
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
		return tokenResult{err: ctx.Err()}
	case res := <-done:
		_ = srv.Shutdown(context.Background())
		return res
	}
}

func tokenSetFromOAuth(tok *oauth2.Token) TokenSet {
	out := TokenSet{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	if !tok.Expiry.IsZero() {
		out.Expiry = tok.Expiry
	}
	return out
}

func listenRedirect(redirectURI string) (net.Listener, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return nil, fmt.Errorf("parse redirect URI: %w", err)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	addr := net.JoinHostPort(host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return ln, nil
}

func pkceChallenge() (verifier, challenge string, err error) {
	raw, err := randomURLSafe(64)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(raw))
	return raw, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n], nil
}
