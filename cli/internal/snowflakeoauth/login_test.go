package snowflakeoauth

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestListenRedirectDefaultURI(t *testing.T) {
	ln, err := listenRedirect(DefaultRedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ln.Close()
	}()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("addr type %T", ln.Addr())
	}
	if addr.Port != 8765 {
		t.Fatalf("port = %d, want 8765", addr.Port)
	}
}

func TestWaitForCallbackServeErrorReturnsImmediately(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	res := waitForCallback(ctx, ln, &oauth2.Config{}, "state", "verifier", DefaultAudience)
	elapsed := time.Since(start)

	if res.err == nil {
		t.Fatal("expected serve error, got nil")
	}
	if elapsed > time.Second {
		t.Fatalf("waitForCallback blocked %s waiting for timeout; want immediate serve error", elapsed)
	}
}
