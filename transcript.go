package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	projectsDir    = filepath.Join(home(), ".claude", "projects")
	nonAlnum       = regexp.MustCompile(`[^A-Za-z0-9]`)
	commandEcho    = regexp.MustCompile(`^\s*<(bash-|local-command|command-)`)
	findTranscript = defaultFindTranscript
)

func home() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return dir
}

func newest(pattern string) string {
	files, _ := filepath.Glob(pattern)
	best, bestTime := "", int64(0)
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().UnixNano() > bestTime {
			best, bestTime = f, info.ModTime().UnixNano()
		}
	}
	return best
}

// defaultFindTranscript finds the session log: this session's when run from
// inside Claude Code, else the newest one for this folder, else the newest.
func defaultFindTranscript() string {
	if session := os.Getenv("CLAUDE_CODE_SESSION_ID"); session != "" {
		if hits, _ := filepath.Glob(filepath.Join(projectsDir, "*", session+".jsonl")); len(hits) > 0 {
			return hits[0]
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if f := newest(filepath.Join(projectsDir, nonAlnum.ReplaceAllString(cwd, "-"), "*.jsonl")); f != "" {
			return f
		}
	}
	return newest(filepath.Join(projectsDir, "*", "*.jsonl"))
}

type entry struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type part struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// content returns a message's text, and whether it carries tool results.
func (e entry) content() (string, bool) {
	raw := bytes.TrimSpace(e.Message.Content)
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		json.Unmarshal(raw, &s)
		return s, false
	}
	var parts []part
	json.Unmarshal(raw, &parts)
	var texts []string
	for _, p := range parts {
		if p.Type == "tool_result" {
			return "", true
		}
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, " "), false
}

// isPrompt reports whether an entry is a message the user typed, as opposed
// to tool results or echoes of ! and / commands.
func (e entry) isPrompt() bool {
	if e.Type != "user" || e.IsMeta {
		return false
	}
	text, toolResult := e.content()
	return !toolResult && !commandEcho.MatchString(text)
}

func (e entry) texts() []string {
	var parts []part
	json.Unmarshal(e.Message.Content, &parts)
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return texts
}

// lastReply returns the text segments of the newest assistant reply that has
// any text, and whether that reply answers the newest prompt.
func lastReply(path string) ([]string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	replies := [][]string{nil}
	reader := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var e entry
			if json.Unmarshal(line, &e) == nil {
				switch {
				case e.isPrompt():
					replies = append(replies, nil)
				case e.Type == "assistant":
					last := len(replies) - 1
					replies[last] = append(replies[last], e.texts()...)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
	}
	for i := len(replies) - 1; i >= 0; i-- {
		if len(replies[i]) > 0 {
			return replies[i], i == len(replies)-1, nil
		}
	}
	return nil, false, nil
}
