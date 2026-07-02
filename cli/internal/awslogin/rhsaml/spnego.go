package rhsaml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/openshift-online/finops-tools/cli/internal/extrace"
)

type spnegoHTTPGetter interface {
	Get(ctx context.Context, url string) (string, error)
}

type defaultSPNEGOHTTPGetter struct {
	timeout time.Duration
}

func (g defaultSPNEGOHTTPGetter) Get(ctx context.Context, url string) (string, error) {
	if html, err := g.getWithGoSPNEGO(ctx, url); err == nil {
		return html, nil
	}
	return g.getWithCurlSPNEGO(ctx, url)
}

func (g defaultSPNEGOHTTPGetter) getWithGoSPNEGO(ctx context.Context, targetURL string) (string, error) {
	cl, err := newKerberosClient(ctx)
	if err != nil {
		return "", err
	}
	httpClient := spnego.NewClient(cl, &http.Client{Timeout: g.timeout}, "")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("create SPNEGO request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("SPNEGO GET request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read SPNEGO response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("SPNEGO GET request returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

func (g defaultSPNEGOHTTPGetter) getWithCurlSPNEGO(ctx context.Context, targetURL string) (string, error) {
	cmd := extrace.CommandContext(ctx, "curl",
		"--silent",
		"--show-error",
		"--fail",
		"--location",
		"--negotiate",
		"-u", ":",
		targetURL,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("SPNEGO GET request failed: %s", msg)
	}
	return string(out), nil
}

func newKerberosClient(ctx context.Context) (*client.Client, error) {
	ccachePath, err := kerberosCCachePath(ctx)
	if err != nil {
		return nil, err
	}
	cc, err := credentials.LoadCCache(ccachePath)
	if err != nil {
		return nil, fmt.Errorf("load Kerberos credential cache %q: %w", ccachePath, err)
	}
	krbConfigPath, err := kerberosConfigPath()
	if err != nil {
		return nil, err
	}
	krbConfig, err := config.Load(krbConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load Kerberos config %q: %w", krbConfigPath, err)
	}
	cl, err := client.NewFromCCache(cc, krbConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kerberos client from cache: %w", err)
	}
	return cl, nil
}

func kerberosCCachePath(ctx context.Context) (string, error) {
	raw := strings.TrimSpace(os.Getenv("KRB5CCNAME"))
	if raw != "" {
		if strings.HasPrefix(raw, "FILE:") {
			return strings.TrimPrefix(raw, "FILE:"), nil
		}
		if strings.HasPrefix(raw, "API:") {
			return "", fmt.Errorf("KRB5CCNAME=%q is not a FILE cache; set KRB5CCNAME to a FILE cache path for native SPNEGO", raw)
		}
		return raw, nil
	}
	uidOut, err := extrace.CommandContext(ctx, "id", "-u").Output()
	if err != nil {
		return "", fmt.Errorf("resolve current uid for default Kerberos cache: %w", err)
	}
	uid := strings.TrimSpace(string(uidOut))
	if uid == "" {
		return "", fmt.Errorf("resolve current uid for default Kerberos cache: empty uid")
	}
	return filepath.Join("/tmp", "krb5cc_"+uid), nil
}

func kerberosConfigPath() (string, error) {
	if cfg := strings.TrimSpace(os.Getenv("KRB5_CONFIG")); cfg != "" {
		return cfg, nil
	}
	const defaultPath = "/etc/krb5.conf"
	if _, err := os.Stat(defaultPath); err != nil {
		return "", fmt.Errorf("Kerberos config not found at %s: %w", defaultPath, err)
	}
	return defaultPath, nil
}
