package live

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// userAgent identifies requests to mondaynightpinball.com.
const userAgent = "mnp.tips (https://github.com/negz/mnp)"

// An HTTPSource gets matches from mondaynightpinball.com.
type HTTPSource struct {
	base   string
	client *http.Client
	record string
	log    *slog.Logger

	mu   sync.Mutex
	last map[string][]byte // The last body recorded for each match.
}

// An HTTPSourceOption configures an HTTPSource.
type HTTPSourceOption func(*HTTPSource)

// WithRecordDir records each new version of a match's JSON to the given
// directory, so matches can be studied later.
func WithRecordDir(dir string) HTTPSourceOption {
	return func(s *HTTPSource) {
		s.record = dir
	}
}

// WithLogger logs problems recording matches.
func WithLogger(l *slog.Logger) HTTPSourceOption {
	return func(s *HTTPSource) {
		s.log = l
	}
}

// NewHTTPSource returns a source that gets matches from the site at the given
// base URL, e.g. https://www.mondaynightpinball.com.
func NewHTTPSource(base string, c *http.Client, opts ...HTTPSourceOption) *HTTPSource {
	s := &HTTPSource{
		base:   base,
		client: c,
		log:    slog.New(slog.DiscardHandler),
		last:   make(map[string][]byte),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Match gets the match with the given key.
func (s *HTTPSource) Match(ctx context.Context, key string) (*Match, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/matches/"+key+".json", nil)
	if err != nil {
		return nil, fmt.Errorf("create request for match %s: %w", key, err)
	}
	req.Header.Set("User-Agent", userAgent)

	rsp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get match %s: %w", key, err)
	}
	defer rsp.Body.Close() //nolint:errcheck // Nothing to do with the error.

	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get match %s: %s", key, rsp.Status)
	}

	body, err := io.ReadAll(rsp.Body)
	if err != nil {
		return nil, fmt.Errorf("read match %s: %w", key, err)
	}

	m := &Match{}
	if err := json.Unmarshal(body, m); err != nil {
		return nil, fmt.Errorf("decode match %s: %w", key, err)
	}

	if err := s.save(key, body); err != nil {
		s.log.Error("cannot record match", "match", key, "err", err)
	}

	return m, nil
}

// save records the body of a match if it changed since it was last recorded.
func (s *HTTPSource) save(key string, body []byte) error {
	if s.record == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if bytes.Equal(s.last[key], body) {
		return nil
	}

	dir := filepath.Join(s.record, key)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	name := filepath.Join(dir, strconv.FormatInt(time.Now().UnixMilli(), 10)+".json")
	if err := os.WriteFile(name, body, 0o600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	s.last[key] = body
	return nil
}

// A CachedSource remembers matches from another source for a while, so many
// people looking at a match cause one request for it.
type CachedSource struct {
	wrapped Source
	ttl     time.Duration
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	mu  sync.Mutex
	at  time.Time
	m   *Match
	err error
}

// NewCachedSource returns a source that remembers matches, and errors getting
// them, for the given time.
func NewCachedSource(s Source, ttl time.Duration) *CachedSource {
	return &CachedSource{wrapped: s, ttl: ttl, now: time.Now, entries: make(map[string]*entry)}
}

// Match returns the match with the given key, getting it from the wrapped
// source if it isn't remembered. Concurrent calls for a match share one get.
func (s *CachedSource) Match(ctx context.Context, key string) (*Match, error) {
	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		e = &entry{}
		s.entries[key] = e
	}
	s.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.at.IsZero() && s.now().Sub(e.at) < s.ttl {
		return e.m, e.err
	}
	// Everyone waiting shares this get, so it shouldn't fail because the
	// first of them went away.
	e.m, e.err = s.wrapped.Match(context.WithoutCancel(ctx), key)
	e.at = s.now()
	return e.m, e.err
}
