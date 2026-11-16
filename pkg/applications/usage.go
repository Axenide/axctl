package applications

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Usage struct {
	Count    int   `json:"count"`
	LastUsed int64 `json:"lastUsed"`
}

type UsageStore struct {
	mu     sync.RWMutex
	path   string
	values map[string]Usage
}

func NewUsageStore() (*UsageStore, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	store := &UsageStore{
		path:   filepath.Join(stateHome, "axctl", "application-usage.json"),
		values: make(map[string]Usage),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *UsageStore) Score(id string) int {
	s.mu.RLock()
	usage, ok := s.values[id]
	s.mu.RUnlock()
	if !ok || usage.Count < 1 {
		return 0
	}
	days := time.Since(time.UnixMilli(usage.LastUsed)).Hours() / 24
	if days < 0 {
		days = 0
	}
	return int(200*math.Exp(-days/7) + math.Log(float64(usage.Count+1))*20)
}

func (s *UsageStore) Record(id string) error {
	if id == "" {
		return fmt.Errorf("application ID is required")
	}
	s.mu.Lock()
	usage := s.values[id]
	usage.Count++
	usage.LastUsed = time.Now().UnixMilli()
	s.values[id] = usage
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

func (s *UsageStore) Snapshot() map[string]Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]Usage, len(s.values))
	for id, usage := range s.values {
		result[id] = usage
	}
	return result
}

func (s *UsageStore) Import(values map[string]Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, imported := range values {
		if id == "" || imported.Count < 1 {
			continue
		}
		if existing, ok := s.values[id]; !ok || imported.LastUsed > existing.LastUsed {
			s.values[id] = imported
		}
	}
	return s.saveLocked()
}

func (s *UsageStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.values); err != nil {
		return fmt.Errorf("parse application usage: %w", err)
	}
	if s.values == nil {
		s.values = make(map[string]Usage)
	}
	return nil
}

func (s *UsageStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".application-usage-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
