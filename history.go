package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	historySize = 50
	// undoFor is how long pluck keeps what a copy replaced, for --undo. It
	// may be a password, so it shouldn't sit on disk for long.
	undoFor = 5 * time.Minute
)

var historyPath = filepath.Join(home(), ".claude", "pluck", "history.jsonl")

type historyEntry struct {
	Time float64 `json:"time"`
	Text string  `json:"text"`
}

// undoPath holds what the clipboard had before the latest copy.
func undoPath() string {
	return filepath.Join(filepath.Dir(historyPath), "undo.txt")
}

func loadHistory() []historyEntry {
	f, err := os.Open(historyPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var entries []historyEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		var e historyEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

// writePrivate replaces a file only you can read, all at once, so two
// sessions finishing together can't leave it half written.
func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func saveHistory(entries []historyEntry) error {
	if len(entries) > historySize {
		entries = entries[len(entries)-historySize:]
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return writePrivate(historyPath, buf.Bytes())
}

// put copies text and adds it to the history, keeping what it replaced so
// --undo can restore it. Copying something already in the history moves it
// to the top instead of adding a repeat.
func put(text string) bool {
	before, hadBefore := pasteText()
	if !copyText(text) {
		return false
	}
	var entries []historyEntry
	for _, e := range loadHistory() {
		if e.Text != text {
			entries = append(entries, e)
		}
	}
	saveHistory(append(entries, historyEntry{Time: float64(time.Now().UnixNano()) / 1e9, Text: text}))
	if hadBefore && before != text {
		writePrivate(undoPath(), []byte(before))
	}
	return true
}

// forgetOldUndo deletes what --undo would restore once it's older than
// undoFor. pluck runs after every Claude reply, so it doesn't stay much longer.
func forgetOldUndo() {
	if info, err := os.Stat(undoPath()); err == nil && time.Since(info.ModTime()) > undoFor {
		os.Remove(undoPath())
	}
}
