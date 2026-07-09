package aws

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteProfileConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	const profiles = 20
	var wg sync.WaitGroup
	wg.Add(profiles)
	errCh := make(chan error, profiles)
	for i := 0; i < profiles; i++ {
		i := i
		go func() {
			defer wg.Done()
			profile := fmt.Sprintf("acct-%02d", i)
			errCh <- WriteProfile(path, profile, ProfileSession{
				AccessKeyID:     profile + "-key",
				SecretAccessKey: profile + "-secret",
				SessionToken:    profile + "-token",
				Region:          "us-east-1",
			})
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < profiles; i++ {
		profile := fmt.Sprintf("acct-%02d", i)
		got, ok, err := ReadProfile(path, profile)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got.AccessKeyID != profile+"-key" {
			t.Fatalf("profile %q: ok=%v got=%+v", profile, ok, got)
		}
	}
}
