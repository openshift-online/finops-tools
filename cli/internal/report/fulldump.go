// fulldump.go implements "finops report create executive-summary
// --data-dump-out": a JSON export of every computed field of the
// executive-summary report, grouped into separate JSON sections rather than
// one flat object, so each data source can be loaded/queried independently
// downstream and so merges in the PR48 breakup series can diff their
// sections against committed golden dumps. Merge 0 (this skeleton) writes
// only the report metadata (window, months); later merges extend the dump
// with their sections as those computations land.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FullDataDump is every computed field of the executive-summary report, as
// JSON. See this file's package doc comment.
type FullDataDump struct {
	GeneratedAt string   `json:"generated_at"`
	WindowStart string   `json:"window_start"`
	WindowEnd   string   `json:"window_end"`
	Months      []string `json:"months"`
	DataSources []string `json:"data_sources"`
}

// WriteFullDataDumpJSON marshals dump as indented JSON and writes it to path
// atomically (temp file + rename), creating parent directories as needed.
func WriteFullDataDumpJSON(path string, dump FullDataDump) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create data dump directory: %w", err)
	}
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data dump: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write data dump temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename data dump temp file: %w", err)
	}
	return nil
}
