package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const historySize = 50

var historyPath = filepath.Join(home(), ".claude", "pluck", "history.jsonl")

type historyEntry struct {
	Time     float64 `json:"time"`
	Text     string  `json:"text"`
	Replaced *string `json:"replaced,omitempty"`
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

func saveHistory(entries []historyEntry) error {
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(historyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Chmod(0o600)
	if len(entries) > historySize {
		entries = entries[len(entries)-historySize:]
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// put copies text, keeping what it replaced so --undo can restore it. Only
// the newest entry keeps the replaced text, so anything else you had copied
// (a password, say) doesn't pile up on disk. Copying something already in
// the history moves it to the top instead of adding a repeat.
func put(text string) bool {
	before, hadBefore := pasteText()
	if !copyText(text) {
		return false
	}
	var entries []historyEntry
	for _, e := range loadHistory() {
		if e.Text != text {
			e.Replaced = nil
			entries = append(entries, e)
		}
	}
	e := historyEntry{Time: float64(time.Now().UnixNano()) / 1e9, Text: text}
	if hadBefore && before != text {
		e.Replaced = &before
	}
	saveHistory(append(entries, e))
	return true
}
