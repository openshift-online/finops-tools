// Package progress writes human-readable status lines to stderr during long operations.
package progress

import (
	"fmt"
	"io"
	"sync"

	"github.com/openshift-online/finops-tools/cli/internal/output"
)

// Writer emits step messages to w (typically os.Stderr).
type Writer struct {
	w     io.Writer
	quiet bool
	mu    sync.Mutex
}

// New returns a progress writer. When quiet is true, Step is a no-op.
func New(w io.Writer, quiet bool) *Writer {
	return &Writer{w: w, quiet: quiet}
}

// Step prints a status line prefixed with an arrow.
func (p *Writer) Step(message string) {
	if p == nil || p.quiet || p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = fmt.Fprintf(p.w, "→ %s\n", output.SanitizeTerminal(message))
}
