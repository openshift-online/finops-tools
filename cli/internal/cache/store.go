// Package cache provides a generic on-disk cache for finops CLI data.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTTL is the default cache lifetime when Policy.TTL is unset.
const DefaultTTL = time.Hour

// Source identifies whether a value came from cache or a live fetch.
type Source string

const (
	SourceCache Source = "cache"
	SourceFetch Source = "fetch"
)

// Entry is one cached value with metadata.
type Entry[T any] struct {
	Key       string    `json:"key"`
	FetchedAt time.Time `json:"fetched_at"`
	Value     T         `json:"value"`
}

// Policy controls cache read/write behavior for GetOrFetch.
type Policy struct {
	Skip    bool
	Refresh bool
	TTL     time.Duration
}

// MissReason explains why a cache entry was not used.
type MissReason string

const (
	MissNotFound MissReason = "not_found"
	MissExpired  MissReason = "expired"
	MissInvalid  MissReason = "invalid"
	MissError    MissReason = "error"
	MissRefresh  MissReason = "refresh"
)

// Hooks emits optional progress while resolving cache entries.
type Hooks[T any] struct {
	OnHit       func(Entry[T])
	OnMiss      func(reason MissReason, err error)
	OnUpdate    func(T)
	OnSaveError func(error)
}

// Result is the outcome of GetOrFetch.
type Result[T any] struct {
	Value     T
	Source    Source
	FetchedAt time.Time
}

// Store persists cache entries under cache/<namespace>/ next to the finops config file.
type Store struct {
	configPath string
}

// New returns a cache store rooted at the directory containing configPath.
func New(configPath string) *Store {
	return &Store{configPath: configPath}
}

// Dir returns the cache directory for namespace.
func (s *Store) Dir(namespace string) string {
	return filepath.Join(filepath.Dir(s.configPath), "cache", sanitizeSegment(namespace))
}

// Path returns the cache file path for namespace and key.
func (s *Store) Path(namespace, key string) string {
	return filepath.Join(s.Dir(namespace), sanitizeSegment(key)+".json")
}

// Delete removes a cache entry when present. Missing files are ignored.
func (s *Store) Delete(namespace, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("cache key is required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return errors.New("cache namespace is required")
	}
	err := os.Remove(s.Path(namespace, key))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Load reads a cache entry when present.
func Load[T any](s *Store, namespace, key string) (Entry[T], error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Entry[T]{}, errors.New("cache key is required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return Entry[T]{}, errors.New("cache namespace is required")
	}

	data, err := os.ReadFile(s.Path(namespace, key))
	if err != nil {
		return Entry[T]{}, err
	}

	var entry Entry[T]
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry[T]{}, fmt.Errorf("decode cache entry: %w", err)
	}
	if entry.Key != key {
		return Entry[T]{}, fmt.Errorf("cache key mismatch: have %q, want %q", entry.Key, key)
	}
	return entry, nil
}

// Save writes a cache entry atomically.
func Save[T any](s *Store, namespace string, entry Entry[T]) error {
	entry.Key = strings.TrimSpace(entry.Key)
	if entry.Key == "" {
		return errors.New("cache key is required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return errors.New("cache namespace is required")
	}
	if entry.FetchedAt.IsZero() {
		entry.FetchedAt = time.Now().UTC()
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}

	dir := s.Dir(namespace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	path := s.Path(namespace, entry.Key)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace cache entry: %w", err)
	}
	return nil
}

// Fresh reports whether entry is within ttl of now and passes valid.
func Fresh[T any](entry Entry[T], ttl time.Duration, now time.Time, valid func(T) bool) bool {
	if ttl <= 0 {
		return false
	}
	if entry.FetchedAt.IsZero() {
		return false
	}
	if valid != nil && !valid(entry.Value) {
		return false
	}
	return !now.After(entry.FetchedAt.Add(ttl))
}

// EffectiveTTL returns ttl or DefaultTTL when ttl is unset.
func EffectiveTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultTTL
	}
	return ttl
}

// GetOrFetch returns a cached value when policy allows, otherwise calls fetch and optionally persists the result.
func GetOrFetch[T any](
	s *Store,
	namespace, key string,
	policy Policy,
	now time.Time,
	hooks *Hooks[T],
	fetch func() (T, error),
	valid func(T) bool,
) (Result[T], error) {
	ttl := EffectiveTTL(policy.TTL)

	if policy.Refresh && hooks != nil && hooks.OnMiss != nil {
		hooks.OnMiss(MissRefresh, nil)
	}

	if !policy.Skip && !policy.Refresh {
		entry, err := Load[T](s, namespace, key)
		switch {
		case err == nil && Fresh(entry, ttl, now, valid):
			if hooks != nil && hooks.OnHit != nil {
				hooks.OnHit(entry)
			}
			return Result[T]{
				Value:     entry.Value,
				Source:    SourceCache,
				FetchedAt: entry.FetchedAt,
			}, nil
		case err == nil:
			if hooks != nil && hooks.OnMiss != nil {
				hooks.OnMiss(MissExpired, nil)
			}
		case errors.Is(err, os.ErrNotExist):
			if hooks != nil && hooks.OnMiss != nil {
				hooks.OnMiss(MissNotFound, nil)
			}
		default:
			if hooks != nil && hooks.OnMiss != nil {
				hooks.OnMiss(MissError, err)
			}
		}
	}

	value, err := fetch()
	if err != nil {
		return Result[T]{}, err
	}

	if policy.Skip {
		return Result[T]{
			Value:  value,
			Source: SourceFetch,
		}, nil
	}

	fetchedAt := now.UTC()
	if err := Save(s, namespace, Entry[T]{
		Key:       key,
		FetchedAt: fetchedAt,
		Value:     value,
	}); err != nil {
		if hooks != nil && hooks.OnSaveError != nil {
			hooks.OnSaveError(err)
			return Result[T]{
				Value:     value,
				Source:    SourceFetch,
				FetchedAt: fetchedAt,
			}, nil
		}
		return Result[T]{}, fmt.Errorf("save cache entry: %w", err)
	}

	if hooks != nil && hooks.OnUpdate != nil {
		hooks.OnUpdate(value)
	}

	return Result[T]{
		Value:     value,
		Source:    SourceFetch,
		FetchedAt: fetchedAt,
	}, nil
}

func sanitizeSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	segment = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		default:
			return r
		}
	}, segment)
	return segment
}
