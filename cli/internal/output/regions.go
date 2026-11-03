package output

import (
	"fmt"
	"io"
	"strings"
)

func writeExcludedRegions(w io.Writer, regions []string) error {
	if len(regions) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "  Excluded regions (regional scans): %s\n", SanitizeTerminal(strings.Join(regions, ", ")))
	return err
}
