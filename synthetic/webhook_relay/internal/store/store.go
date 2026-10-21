// Package store keeps verified deliveries in an append-only file, one JSON object per line, flushed to disk before
// the relay answers the sender. A restart reads the file again, so a delivery acknowledged to Tushe is never lost.
// The relay is a mailbox, not an authority: the core's Record is (INV-30, DEC-2026-10-16 E6).
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Entry is one stored delivery. Body is the exact bytes received; it is base64 in JSON.
type Entry struct {
	Seq        int64  `json:"seq"`
	ID         string `json:"id"`
	Timestamp  int64  `json:"timestamp"`
	ReceivedAt string `json:"received_at"`
	BodySHA256 string `json:"body_sha256"`
	Body       []byte `json:"body"`
}

// Outcome of an Add.
type Outcome int

const (
	Stored    Outcome = iota // a new delivery
	Duplicate                // the same id and the same body again: acknowledged, not stored twice
	Conflict                 // the same id with a different body: refused
)

// Store is safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	f       *os.File
	entries []Entry
	byID    map[string]int // id -> index in entries
}

// Open reads the file (creating it if absent) and returns a Store. A torn last line, left by a crash during a write,
// is cut off; a corrupt line anywhere else is an error, because the file is the relay's only memory.
func Open(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read store: %w", err)
	}
	s := &Store{byID: map[string]int{}}
	valid := 0
	rest := data
	for len(rest) > 0 {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			break // torn tail: no newline
		}
		var e Entry
		if err := json.Unmarshal(rest[:nl], &e); err != nil || e.ID == "" || e.Seq != int64(len(s.entries))+1 {
			return nil, fmt.Errorf("store line %d is corrupt; refusing to start so no delivery is silently dropped", len(s.entries)+1)
		}
		s.byID[e.ID] = len(s.entries)
		s.entries = append(s.entries, e)
		valid += nl + 1
		rest = rest[nl+1:]
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	if err := f.Truncate(int64(valid)); err != nil {
		f.Close()
		return nil, fmt.Errorf("trim store: %w", err)
	}
	if _, err := f.Seek(int64(valid), 0); err != nil {
		f.Close()
		return nil, err
	}
	s.f = f
	return s, nil
}

// Close closes the file.
func (s *Store) Close() error { return s.f.Close() }

// Add stores a verified delivery unless it is a duplicate or a conflict. It returns only after the line is on disk.
func (s *Store) Add(id string, timestamp int64, body []byte, now time.Time) (Outcome, int64, error) {
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.byID[id]; ok {
		if s.entries[i].BodySHA256 == hexSum {
			return Duplicate, s.entries[i].Seq, nil
		}
		return Conflict, s.entries[i].Seq, nil
	}
	e := Entry{Seq: int64(len(s.entries)) + 1, ID: id, Timestamp: timestamp, ReceivedAt: now.UTC().Format(time.RFC3339Nano), BodySHA256: hexSum, Body: append([]byte(nil), body...)}
	line, err := json.Marshal(e)
	if err != nil {
		return Stored, 0, err
	}
	if _, err := s.f.Write(append(line, '\n')); err != nil {
		return Stored, 0, fmt.Errorf("append: %w", err)
	}
	if err := s.f.Sync(); err != nil {
		return Stored, 0, fmt.Errorf("sync: %w", err)
	}
	s.byID[id] = len(s.entries)
	s.entries = append(s.entries, e)
	return Stored, e.Seq, nil
}

// After returns up to limit entries with Seq greater than after, in order.
func (s *Store) After(after int64, limit int) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if after < 0 {
		after = 0
	}
	if after >= int64(len(s.entries)) {
		return nil
	}
	end := int(after) + limit
	if end > len(s.entries) {
		end = len(s.entries)
	}
	return append([]Entry(nil), s.entries[after:end]...)
}

// Len is the number of stored deliveries.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
