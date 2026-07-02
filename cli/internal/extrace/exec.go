// Package extrace logs external command invocations when verbose mode is enabled via context.
package extrace

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
)

type contextKey struct{}

// WithWriter enables verbose logging of external commands to w for descendants of ctx.
func WithWriter(ctx context.Context, w io.Writer) context.Context {
	if w == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, w)
}

func writer(ctx context.Context) io.Writer {
	if ctx == nil {
		return nil
	}
	w, _ := ctx.Value(contextKey{}).(io.Writer)
	return w
}

// LogCommand prints the command line when verbose mode is enabled.
func LogCommand(ctx context.Context, name string, args ...string) {
	w := writer(ctx)
	if w == nil {
		return
	}
	argv := append([]string{name}, args...)
	line := "+"
	for _, arg := range argv {
		line += " " + strconv.Quote(arg)
	}
	_, _ = fmt.Fprintln(w, line)
}

// CommandContext creates an exec.Cmd and logs it when verbose mode is enabled.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	LogCommand(ctx, name, args...)
	return exec.CommandContext(ctx, name, args...)
}
