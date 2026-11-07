package main

// Message history for the built-in Messages screen. Each radio gets an
// append-only JSON Lines file in OfflineLibrary/data/messages/<node id>.jsonl,
// so history travels with the drive. Lines are written whole and never
// rewritten: if the drive drops out mid-write, at most the last line is lost,
// and readers skip any line that doesn't parse.
//
// The server doesn't interpret records beyond requiring JSON objects; the UI
// defines their shape (messages and later status updates).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

const (
	maxMessageBatchBytes = 256 << 10 // per POST
	maxMessageRecord     = 8 << 10   // per line
)

// Meshtastic node IDs are 32-bit numbers, written as 8 hex digits.
var radioIDPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

var messageFileMu sync.Mutex

func (s *server) messagesFile(radio string) (string, bool) {
	if !radioIDPattern.MatchString(radio) {
		return "", false
	}
	return filepath.Join(s.root, "data", "messages", radio+".jsonl"), true
}

// libraryPresent reports whether the drive (library.json) is reachable, so
// history isn't written to a stray folder when the drive is unplugged.
func (s *server) libraryPresent() bool {
	_, err := os.Stat(filepath.Join(s.root, "library.json"))
	return err == nil
}

func (s *server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	path, ok := s.messagesFile(r.PathValue("radio"))
	if !ok {
		http.Error(w, "bad radio id", http.StatusBadRequest)
		return
	}
	if !s.libraryPresent() {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		writeJSON(w, []json.RawMessage{})
		return
	}
	if err != nil {
		http.Error(w, "message history can't be read right now", http.StatusServiceUnavailable)
		return
	}
	defer f.Close()

	records := []json.RawMessage{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), maxMessageRecord*2)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' || !json.Valid(line) {
			continue // e.g. a line cut short by a disconnect
		}
		records = append(records, json.RawMessage(bytes.Clone(line)))
	}
	writeJSON(w, records)
}

func (s *server) handleAppendMessages(w http.ResponseWriter, r *http.Request) {
	path, ok := s.messagesFile(r.PathValue("radio"))
	if !ok {
		http.Error(w, "bad radio id", http.StatusBadRequest)
		return
	}
	if !s.libraryPresent() {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessageBatchBytes))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var batch []json.RawMessage
	if err := json.Unmarshal(body, &batch); err != nil {
		http.Error(w, "expected a JSON array of records", http.StatusBadRequest)
		return
	}
	var out bytes.Buffer
	for _, rec := range batch {
		var compact bytes.Buffer
		if err := json.Compact(&compact, rec); err != nil || compact.Len() == 0 || compact.Bytes()[0] != '{' {
			http.Error(w, "each record must be a JSON object", http.StatusBadRequest)
			return
		}
		if compact.Len() > maxMessageRecord {
			http.Error(w, "record too large", http.StatusRequestEntityTooLarge)
			return
		}
		out.Write(compact.Bytes())
		out.WriteByte('\n')
	}
	if out.Len() == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	messageFileMu.Lock()
	defer messageFileMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, "can't save messages to the drive", http.StatusServiceUnavailable)
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		http.Error(w, "can't save messages to the drive", http.StatusServiceUnavailable)
		return
	}
	_, werr := f.Write(out.Bytes())
	cerr := f.Close()
	if werr != nil || cerr != nil {
		http.Error(w, "can't save messages to the drive", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
