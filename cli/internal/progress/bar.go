package progress

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mattn/go-isatty"
)

const barWidth = 24

// Bar renders indexed progress as an in-place bar on interactive terminals.
type Bar struct {
	w           io.Writer
	interactive bool
	label       string
	total       int
	completed   atomic.Int64
	lastDrawn   int
	finished    bool
	mu          sync.Mutex
}

// NewBar returns a progress bar for total items. Returns nil when quiet, total <= 1, or w is nil.
func NewBar(w io.Writer, quiet bool, label string, total int) *Bar {
	if quiet || total <= 1 || w == nil {
		return nil
	}
	return &Bar{
		w:           w,
		interactive: isTerminal(w),
		label:       label,
		total:       total,
	}
}

// Advance increments completed work and redraws the bar.
func (b *Bar) Advance() {
	if b == nil {
		return
	}
	b.completed.Add(1)
	b.draw(false)
}

// Finish marks the bar complete and leaves the terminal on a new line.
func (b *Bar) Finish() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished {
		return
	}
	b.finished = true
	b.render(b.total, true)
}

func (b *Bar) draw(force bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished {
		return
	}
	done := int(b.completed.Load())
	if done > b.total {
		done = b.total
	}
	if !force && done == b.lastDrawn {
		return
	}
	b.render(done, false)
}

func (b *Bar) render(done int, finalize bool) {
	b.lastDrawn = done
	pct := percent(done, b.total)
	bar := formatBar(done, b.total)
	line := fmt.Sprintf("→ %s %s %d%%", b.label, bar, pct)

	if b.interactive {
		_, _ = fmt.Fprint(b.w, "\r"+line)
		if finalize {
			_, _ = fmt.Fprintln(b.w)
		}
		return
	}
	if finalize && done < b.total {
		done = b.total
		pct = 100
		bar = formatBar(done, b.total)
		line = fmt.Sprintf("→ %s %s %d%%", b.label, bar, pct)
	}
	if finalize || shouldReportMilestone(done, b.total) {
		_, _ = fmt.Fprintln(b.w, line)
	}
}

func percent(done, total int) int {
	if total <= 0 {
		return 0
	}
	pct := int(math.Round(float64(done) / float64(total) * 100))
	if done == total {
		return 100
	}
	if pct > 99 && done < total {
		return 99
	}
	return pct
}

func formatBar(done, total int) string {
	if total <= 0 {
		return "[" + strings.Repeat("░", barWidth) + "]"
	}
	fraction := float64(done) / float64(total)
	filled := int(math.Round(fraction * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

func shouldReportMilestone(done, total int) bool {
	if total <= 1 {
		return false
	}
	if done == 1 || done == total {
		return true
	}
	if total <= 10 {
		return true
	}
	return done%25 == 0
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}
