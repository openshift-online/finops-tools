package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteFullDataDumpJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dump.json")

	dump := FullDataDump{
		GeneratedAt: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		WindowStart: "2026-05",
		WindowEnd:   "2026-09",
		Months:      []string{"2026-05", "2026-06", "2026-07", "2026-08", "2026-09"},
		DataSources: []string{},
	}
	if err := WriteFullDataDumpJSON(path, dump); err != nil {
		t.Fatalf("WriteFullDataDumpJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got FullDataDump
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.WindowStart != dump.WindowStart || got.WindowEnd != dump.WindowEnd {
		t.Errorf("window = %q..%q, want %q..%q", got.WindowStart, got.WindowEnd, dump.WindowStart, dump.WindowEnd)
	}
	if len(got.Months) != len(dump.Months) {
		t.Fatalf("Months = %v, want %v", got.Months, dump.Months)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file should not remain after write: %v", err)
	}
}
