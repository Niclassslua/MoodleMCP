package syncstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Entry struct {
	TimeModified int64  `json:"timemodified"`
	Size         int64  `json:"size"`
	Path         string `json:"path"`
}

type State struct {
	mu       sync.Mutex
	Entries  map[string]Entry `json:"entries"`
	LastSync int64            `json:"lastSync,omitempty"`
	path     string
}

func Load(courseDir string) (*State, error) {
	path := filepath.Join(courseDir, "course_state.json")
	s := &State{Entries: map[string]Entry{}, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		// allow legacy / empty
		s.Entries = map[string]Entry{}
	}
	if s.Entries == nil {
		s.Entries = map[string]Entry{}
	}
	s.path = path
	return s, nil
}

func (s *State) Unchanged(key string, mtime, size int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Entries[key]
	if !ok {
		return false
	}
	return e.TimeModified == mtime && e.Size == size
}

func (s *State) Set(key string, mtime, size int64, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Entries[key] = Entry{TimeModified: mtime, Size: size, Path: path}
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
