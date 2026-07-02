package apilog

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLogOffByDefault(t *testing.T) {
	var buf bytes.Buffer
	Log(context.Background(), "Organizations.DescribeAccount account=123456789012")
	if buf.Len() != 0 {
		t.Fatalf("expected no output without logger, got %q", buf.String())
	}
}

func TestLogIgnoresEmptyLine(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithLog(context.Background(), func(line string) {
		buf.WriteString(line)
	})
	Log(ctx, "")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty line, got %q", buf.String())
	}
}

func TestLogWithLogger(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithLog(context.Background(), func(line string) {
		buf.WriteString(line)
		buf.WriteByte('\n')
	})
	Log(ctx, "Organizations.DescribeAccount account=123456789012")
	got := strings.TrimSpace(buf.String())
	want := "Organizations.DescribeAccount account=123456789012"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
