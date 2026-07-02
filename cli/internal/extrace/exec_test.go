package extrace

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLogCommandOffByDefault(t *testing.T) {
	var buf bytes.Buffer
	LogCommand(context.Background(), "klist", "-s")
	if buf.Len() != 0 {
		t.Fatalf("expected no output without verbose context, got %q", buf.String())
	}
}

func TestLogCommandDoesNotLogWhenWriterNil(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithWriter(context.Background(), nil)
	LogCommand(ctx, "klist", "-s")
	if buf.Len() != 0 {
		t.Fatalf("expected no output with nil writer, got %q", buf.String())
	}
}

func TestLogCommandWithWriter(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithWriter(context.Background(), &buf)
	LogCommand(ctx, "klist", "-s")
	got := strings.TrimSpace(buf.String())
	want := `+ "klist" "-s"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCommandContext(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithWriter(context.Background(), &buf)
	_ = CommandContext(ctx, "curl", "--negotiate")
	got := strings.TrimSpace(buf.String())
	if got != `+ "curl" "--negotiate"` {
		t.Fatalf("got %q", got)
	}
}

func TestLogCommandQuotesArgsWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithWriter(context.Background(), &buf)
	LogCommand(ctx, "curl", "-u", ":", "https://example.com/path with spaces")
	got := strings.TrimSpace(buf.String())
	want := `+ "curl" "-u" ":" "https://example.com/path with spaces"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
