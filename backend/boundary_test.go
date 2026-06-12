package backend_test

import (
	"os/exec"
	"strings"
	"testing"
)

const forbiddenCLIModule = "github.com/openshift-online/finops-tools/cli"

// TestDoesNotDependOnCLI ensures the backend module never imports cli packages.
// Shared logic belongs in core/; backend/ uses env-based config instead of cli configstore/oauth.
func TestDoesNotDependOnCLI(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("go", "list", "-deps", "./...").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
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
