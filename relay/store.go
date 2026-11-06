package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	StatusQueued       = "queued"
	StatusTranscribing = "transcribing"
	StatusWriting      = "writing"
	StatusDone         = "done"
	StatusFailed       = "failed"
)

// Job is the unit the watch polls. It deliberately carries no audio path in
// JSON: the watch has no business knowing where the bytes live.
type Job struct {
	ID       string    `json:"id"`
	ClientID string    `json:"clientId"`
	Status   string    `json:"status"`
	Text     string    `json:"text,omitempty"`
	NoteID   int64     `json:"noteId,omitempty"`
	Error    string    `json:"error,omitempty"`
	Created  time.Time `json:"createdAt"`

	AudioPath string `json:"-"`
	Duration  int64  `json:"durationMs,omitempty"`
}

// Store keeps jobs in memory and mirrors them to a JSON file so a restart
// never loses a memo that was already accepted (202) from the watch.
type Store struct {
	mu   sync.RWMutex
	path string
	jobs map[string]*Job
	// clientId -> job id, so a retried upload never creates a duplicate note.
	index map[string]string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path:  filepath.Join(dir, "jobs.json"),
		jobs:  map[string]*Job{},
		index: map[string]string{},
	}
	s.load()
	return s, nil
}

func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []*Job
	if json.Unmarshal(b, &list) != nil {
		return
	}
	for _, j := range list {
		s.jobs[j.ID] = j
		if j.ClientID != "" {
			s.index[j.ClientID] = j.ID
		}
	}
}

func (s *Store) flush() {
	list := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		list = append(list, j)
	}
	if b, err := json.MarshalIndent(list, "", "  "); err == nil {
		tmp := s.path + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, s.path)
		}
	}
}

func (s *Store) ByClientID(clientID string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.index[clientID]
	if !ok {
		return nil, false
	}
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Store) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Store) Put(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	if j.ClientID != "" {
		s.index[j.ClientID] = j.ID
	}
	s.flush()
}

func (s *Store) Update(id string, fn func(*Job)) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil
	}
	fn(j)
	s.flush()
	return j
}

// Pending returns jobs that still owe the user a result — used on boot to
// resume work that was interrupted by a restart.
func (s *Store) Pending() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Job
	for _, j := range s.jobs {
		switch j.Status {
		case StatusQueued, StatusTranscribing, StatusWriting:
			out = append(out, j)
		}
	}
	return out
}
