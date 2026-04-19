package logging

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "charm.land/log/v2"
)

type Entry struct {
	Time    time.Time
	Level   string
	Message string
}

type Ring struct {
	mu      sync.Mutex
	entries []Entry
	limit   int
}

func NewRing(limit int) *Ring {
	return &Ring{limit: limit}
}

func (r *Ring) Add(level, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = append(r.entries, Entry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
	})
	if len(r.entries) > r.limit {
		r.entries = append([]Entry(nil), r.entries[len(r.entries)-r.limit:]...)
	}
}

func (r *Ring) Snapshot() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

type mirrorWriter struct {
	target io.Writer
	ring   *Ring
}

func (w *mirrorWriter) Write(p []byte) (int, error) {
	w.ring.Add("log", string(p))
	return w.target.Write(p)
}

type Manager struct {
	Logger *log.Logger
	Ring   *Ring
}

func New(stateDir string) (*Manager, error) {
	path := filepath.Join(stateDir, "logs", "luc.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	ring := NewRing(256)
	logger := log.NewWithOptions(&mirrorWriter{target: f, ring: ring}, log.Options{
		ReportTimestamp: true,
		Formatter:       log.JSONFormatter,
		Level:           log.InfoLevel,
	})

	return &Manager{
		Logger: logger,
		Ring:   ring,
	}, nil
}
