package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestBarAdvanceNonInteractive(t *testing.T) {
	var buf bytes.Buffer
	bar := NewBar(&buf, false, "Preparing account configuration…", 100)
	if bar == nil {
		t.Fatal("expected bar")
	}
	bar.interactive = false

	for i := 0; i < 100; i++ {
		bar.Advance()
	}
	bar.Finish()

	out := buf.String()
	if strings.Count(out, "→") < 2 {
		t.Fatalf("expected milestone lines, got %q", out)
	}
	if !strings.Contains(out, "100%") {
		t.Fatalf("expected final 100%% line, got %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Fatalf("non-interactive output must not use carriage return: %q", out)
	}
}

func TestBarNilSafe(t *testing.T) {
	var bar *Bar
	bar.Advance()
	bar.Finish()
}

func TestBarSkippedForSingleItem(t *testing.T) {
	var buf bytes.Buffer
	if got := NewBar(&buf, false, "label", 1); got != nil {
		t.Fatalf("expected nil bar for single item, got %#v", got)
	}
}

func TestFormatBar(t *testing.T) {
	got := formatBar(12, 24)
	if !strings.Contains(got, "█") || !strings.Contains(got, "░") {
		t.Fatalf("got %q", got)
	}
}
