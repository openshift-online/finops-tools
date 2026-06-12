package backend_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const forbiddenCLIModule = "github.com/openshift-online/finops-tools/cli"

// TestDoesNotDependOnCLI ensures the backend module never imports cli packages.
// Shared logic belongs in core/; backend/ uses env-based config instead of cli configstore/oauth.
func TestDoesNotDependOnCLI(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-test", "./...")
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("go list -deps -test timed out after 10s")
	}
	if err != nil {
		t.Fatalf("go list -deps -test: %v\n%s", err, out)
	}

	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if dep == "" {
			continue
		}
		if dep == forbiddenCLIModule || strings.HasPrefix(dep, forbiddenCLIModule+"/") {
			t.Fatalf("backend must not depend on cli; found dependency %q", dep)
		}
	}
}
