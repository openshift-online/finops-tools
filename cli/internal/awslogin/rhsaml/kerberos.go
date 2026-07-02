package rhsaml

import (
	"context"
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/extrace"
)

func hasKerberosTicket(ctx context.Context) error {
	cmd := extrace.CommandContext(ctx, "klist", "-s")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Kerberos ticket is missing or expired; run kinit first: %w", err)
	}
	return nil
}
