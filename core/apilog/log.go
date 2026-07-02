// Package apilog logs cloud provider API calls when verbose debugging is enabled via context.
package apilog

import "context"

// LogFunc receives one human-readable API query line.
type LogFunc func(line string)

type contextKey struct{}

// WithLog attaches an API query logger to ctx.
func WithLog(ctx context.Context, log LogFunc) context.Context {
	if log == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, log)
}

// Log emits line when a logger is attached to ctx.
func Log(ctx context.Context, line string) {
	if ctx == nil || line == "" {
		return
	}
	if log, ok := ctx.Value(contextKey{}).(LogFunc); ok && log != nil {
		log(line)
	}
}
