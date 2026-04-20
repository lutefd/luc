package rpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

type jsonlWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newJSONLWriter(w io.Writer) *jsonlWriter {
	return &jsonlWriter{w: w}
}

func (w *jsonlWriter) WriteJSONLine(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(data)
	return err
}

func newJSONLScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	return scanner
}

func decodeLine(line []byte, value any) error {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return json.Unmarshal(line, value)
}
