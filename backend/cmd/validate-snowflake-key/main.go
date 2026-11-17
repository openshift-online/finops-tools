// Command validate-snowflake-key checks that a PEM file is an unencrypted RSA
// private key accepted by core/snowflake.ParsePrivateKey (same rules as the
// backend). Used by `make openshift-snowflake-key` before replacing a Secret.
package main

import (
	"fmt"
	"os"

	"github.com/openshift-online/finops-tools/core/snowflake"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-private-key.pem>\n", os.Args[0])
		os.Exit(2)
	}
	path := os.Args[1]
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", path, err)
		os.Exit(1)
	}
	if _, err := snowflake.ParsePrivateKey(string(pemBytes)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s is not a usable Snowflake private key: %v\n", path, err)
		os.Exit(1)
	}
}
