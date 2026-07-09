// credentials_file.go reads and writes profiles in the AWS shared credentials file (~/.aws/credentials).
package aws

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// credentialsFileMu serializes read-modify-write on the shared credentials file.
var credentialsFileMu sync.Mutex

// ProfileSession holds temporary AWS credentials for a credentials file profile.
type ProfileSession struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
}

func (s ProfileSession) hasAccessKeys() bool {
	return s.AccessKeyID != "" && s.SecretAccessKey != ""
}

// complete reports whether the session has temporary credentials (SAML/STS).
func (s ProfileSession) complete() bool {
	return s.hasAccessKeys() && s.SessionToken != ""
}

// static reports long-lived access key credentials (no session token).
func (s ProfileSession) static() bool {
	return s.hasAccessKeys() && s.SessionToken == ""
}

// DefaultCredentialsPath returns ~/.aws/credentials.
func DefaultCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "credentials"), nil
}

// ReadProfile loads a profile from an AWS credentials INI file.
func ReadProfile(path, profile string) (ProfileSession, bool, error) {
	sections, err := parseINI(path)
	if err != nil {
		return ProfileSession{}, false, err
	}
	keys, ok := sections[profile]
	if !ok {
		return ProfileSession{}, false, nil
	}
	sess := ProfileSession{
		AccessKeyID:     keys["aws_access_key_id"],
		SecretAccessKey: keys["aws_secret_access_key"],
		SessionToken:    keys["aws_session_token"],
		Region:          keys["region"],
	}
	if !sess.hasAccessKeys() {
		return ProfileSession{}, false, nil
	}
	if sess.Region == "" {
		sess.Region = "us-east-1"
	}
	return sess, true, nil
}

// WriteProfile merges credentials for profile into path without removing other profiles.
func WriteProfile(path, profile string, sess ProfileSession) error {
	if !sess.complete() && !sess.static() {
		return fmt.Errorf("incomplete session credentials for profile %q", profile)
	}
	if sess.Region == "" {
		sess.Region = "us-east-1"
	}

	credentialsFileMu.Lock()
	defer credentialsFileMu.Unlock()

	sections, err := parseINI(path)
	if err != nil {
		return err
	}
	if sections == nil {
		sections = make(map[string]map[string]string)
	}
	keys := sections[profile]
	if keys == nil {
		keys = make(map[string]string)
		sections[profile] = keys
	}
	keys["aws_access_key_id"] = sess.AccessKeyID
	keys["aws_secret_access_key"] = sess.SecretAccessKey
	if sess.SessionToken != "" {
		keys["aws_session_token"] = sess.SessionToken
	} else {
		delete(keys, "aws_session_token")
	}
	keys["region"] = sess.Region

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if err := writeINI(tmp, sections); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func parseINI(path string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[string]string), nil
		}
		return nil, err
	}

	sections := make(map[string]map[string]string)
	var current string

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if sections[current] == nil {
				sections[current] = make(map[string]string)
			}
			continue
		}
		if current == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		sections[current][strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return sections, nil
}

func writeINI(w *os.File, sections map[string]map[string]string) error {
	names := sortedSectionNames(sections)
	for i, name := range names {
		if i > 0 {
			if _, err := w.WriteString("\n"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "[%s]\n", name); err != nil {
			return err
		}
		keys := sortedKeys(sections[name])
		for _, key := range keys {
			if _, err := fmt.Fprintf(w, "%s = %s\n", key, sections[name][key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedSectionNames(sections map[string]map[string]string) []string {
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
