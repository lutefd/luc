package history

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Store struct {
	root      string
	mu        sync.Mutex
	appenders map[string]*sessionAppender
}

type sessionAppender struct {
	file    *os.File
	encoder *json.Encoder
}

func NewStore(stateDir string) *Store {
	root := filepath.Join(stateDir, "history")
	_ = os.MkdirAll(filepath.Join(root, "sessions"), 0o755)
	return &Store{
		root:      root,
		appenders: make(map[string]*sessionAppender),
	}
}

func (s *Store) Append(ev EventEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	appender, ok := s.appenders[ev.SessionID]
	if !ok {
		path := s.sessionPath(ev.SessionID)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		appender = &sessionAppender{
			file:    f,
			encoder: json.NewEncoder(f),
		}
		s.appenders[ev.SessionID] = appender
	}
	return appender.encoder.Encode(ev)
}

func (s *Store) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if appender, ok := s.appenders[sessionID]; ok {
		_ = appender.file.Close()
		delete(s.appenders, sessionID)
	}
	var errs []error
	for _, path := range []string{s.sessionPath(sessionID), s.metaPath(sessionID)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) DeleteEvent(sessionID string, seq uint64) error {
	if seq == 0 {
		return nil
	}
	events, err := s.Load(sessionID)
	if err != nil {
		return err
	}
	out := events[:0]
	for _, ev := range events {
		if ev.Seq != seq {
			out = append(out, ev)
		}
	}
	if len(out) == len(events) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if appender, ok := s.appenders[sessionID]; ok {
		if err := appender.file.Close(); err != nil {
			return err
		}
		delete(s.appenders, sessionID)
	}
	path := s.sessionPath(sessionID)
	if len(out) == 0 {
		return os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, ev := range out {
		if err := enc.Encode(ev); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
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
	dec := json.NewDecoder(f)
	for {
		var ev EventEnvelope
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		out = append(out, ev)
	}

	return out, nil
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

func (s *Store) Meta(sessionID string) (SessionMeta, bool, error) {
	data, err := os.ReadFile(s.metaPath(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return SessionMeta{}, false, nil
	}
	if err != nil {
		return SessionMeta{}, false, err
	}

	var meta SessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return SessionMeta{}, false, err
	}
	return meta, true, nil
}

func (s *Store) Latest(projectID string) (SessionMeta, bool, error) {
	metas, err := s.List(projectID)
	if err != nil {
		return SessionMeta{}, false, err
	}
	if len(metas) == 0 {
		return SessionMeta{}, false, nil
	}

	return metas[0], true, nil
}

func (s *Store) List(projectID string) ([]SessionMeta, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "sessions"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var metas []SessionMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, "sessions", entry.Name()))
		if err != nil {
			return nil, err
		}
		var meta SessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, err
		}
		if meta.ProjectID == projectID {
			metas = append(metas, meta)
		}
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})
	return metas, nil
}

func (s *Store) sessionPath(sessionID string) string {
	return filepath.Join(s.root, "sessions", sessionID+".jsonl")
}

func (s *Store) metaPath(sessionID string) string {
	return filepath.Join(s.root, "sessions", sessionID+".meta.json")
}

func (s *Store) CloseSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	appender, ok := s.appenders[sessionID]
	if !ok {
		return nil
	}
	delete(s.appenders, sessionID)
	return appender.file.Close()
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for sessionID, appender := range s.appenders {
		if err := appender.file.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(s.appenders, sessionID)
	}
	return errors.Join(errs...)
}
