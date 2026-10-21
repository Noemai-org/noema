package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) // SYNTHETIC

func TestPersistsAcrossRestartAndDedupes(t *testing.T) { // REQ-API-008
	p := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if o, seq, _ := s.Add("msg_1", 100, []byte(`{"a":1}`), t0); o != Stored || seq != 1 {
		t.Fatalf("first add: %v %d", o, seq)
	}
	if o, seq, _ := s.Add("msg_1", 100, []byte(`{"a":1}`), t0); o != Duplicate || seq != 1 {
		t.Fatalf("replay must be a duplicate: %v %d", o, seq)
	}
	if o, _, _ := s.Add("msg_1", 100, []byte(`{"a":2}`), t0); o != Conflict {
		t.Fatalf("same id, different body must conflict: %v", o)
	}
	s.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Len() != 1 {
		t.Fatalf("len after restart = %d", s.Len())
	}
	if o, _, _ := s.Add("msg_1", 100, []byte(`{"a":1}`), t0); o != Duplicate {
		t.Fatalf("replay after restart must still be a duplicate: %v", o)
	}
	if o, seq, _ := s.Add("msg_2", 101, []byte(`x`), t0); o != Stored || seq != 2 {
		t.Fatalf("second add: %v %d", o, seq)
	}
	got := s.After(1, 10)
	if len(got) != 1 || got[0].ID != "msg_2" || string(got[0].Body) != "x" {
		t.Fatalf("After(1) = %+v", got)
	}
}

func TestTornTailIsCutAndCorruptionRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	s, _ := Open(p)
	s.Add("a", 1, []byte("1"), t0)
	s.Close()
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"seq":2,"id":"b","tim`) // a crash mid-write
	f.Close()
	s, err := Open(p)
	if err != nil {
		t.Fatalf("a torn tail must be cut, not refused: %v", err)
	}
	if s.Len() != 1 {
		t.Fatalf("len = %d", s.Len())
	}
	if o, seq, _ := s.Add("b", 2, []byte("2"), t0); o != Stored || seq != 2 {
		t.Fatalf("add after repair: %v %d", o, seq)
	}
	s.Close()
	if s, err = Open(p); err != nil || s.Len() != 2 {
		t.Fatalf("reopen after repair: %v", err)
	}
	s.Close()

	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	os.WriteFile(bad, []byte("not json\n"), 0o600)
	if _, err := Open(bad); err == nil {
		t.Fatal("a corrupt line must refuse to start")
	}
}
