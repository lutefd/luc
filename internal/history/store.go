package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Store struct {
	root string
	mu   sync.Mutex
}

func NewStore(stateDir string) *Store {
	root := filepath.Join(stateDir, "history")
	_ = os.MkdirAll(filepath.Join(root, "sessions"), 0o755)
	return &Store{root: root}
}

func (s *Store) Append(ev EventEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.sessionPath(ev.SessionID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	return enc.Encode(ev)
}

func (s *Store) Load(sessionID string) ([]EventEnvelope, error) {
	path := s.sessionPath(sessionID)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []EventEnvelope
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev EventEnvelope
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}

	return out, scanner.Err()
}

func (s *Store) SaveMeta(meta SessionMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.metaPath(meta.SessionID)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *Store) Latest(projectID string) (SessionMeta, bool, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "sessions"))
	if errors.Is(err, os.ErrNotExist) {
		return SessionMeta{}, false, nil
	}
	if err != nil {
		return SessionMeta{}, false, err
	}

	var metas []SessionMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, "sessions", entry.Name()))
		if err != nil {
			return SessionMeta{}, false, err
		}
		var meta SessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return SessionMeta{}, false, err
		}
		if meta.ProjectID == projectID {
			metas = append(metas, meta)
		}
	}

	if len(metas) == 0 {
		return SessionMeta{}, false, nil
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	return metas[0], true, nil
}

func (s *Store) sessionPath(sessionID string) string {
	return filepath.Join(s.root, "sessions", sessionID+".jsonl")
}

func (s *Store) metaPath(sessionID string) string {
	return filepath.Join(s.root, "sessions", sessionID+".meta.json")
}
