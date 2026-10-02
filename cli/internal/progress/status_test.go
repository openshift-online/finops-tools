package progress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestWriterStep(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf, false)
	w.Step("Fetching costs")
	if !strings.Contains(buf.String(), "Fetching costs") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestWriterQuiet(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf, true)
	w.Step("hidden")
	if buf.Len() != 0 {
		t.Fatalf("got %q", buf.String())
	}
}

func TestWriterStepSanitizesESC(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf, false)
	w.Step("Scanning \x1b[31mred\x1b[0m")
	if strings.Contains(buf.String(), "\x1b") {
		t.Fatalf("progress still contains ESC: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "Scanning red") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestWriterStepConcurrent(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf, false)
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			w.Step("line")
		}()
	}
	wg.Wait()
	if got := strings.Count(buf.String(), "→ line\n"); got != n {
		t.Fatalf("lines = %d, want %d\n%s", got, n, buf.String())
	}
}
