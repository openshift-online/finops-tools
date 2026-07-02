// account_errors.go defines user-facing error messages for account command validation failures.
package cmd

import "fmt"

func errUnknownPayerAlias(alias string) error {
	return fmt.Errorf("unknown payer alias %q (register payer first: finops config account add aws <12-digit-id> --alias %s)", alias, alias)
}
