package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fence = "```"

// setup swaps in a fake clipboard and a temporary history file.
func setup(t *testing.T) *string {
	t.Helper()
	board := "what you had"
	savedCopy, savedPaste, savedHistory, savedFind := copyText, pasteText, historyPath, findTranscript
	copyText = func(text string) bool { board = text; return true }
	pasteText = func() (string, bool) { return board, true }
	historyPath = filepath.Join(t.TempDir(), "history.jsonl")
	t.Cleanup(func() {
		copyText, pasteText, historyPath, findTranscript = savedCopy, savedPaste, savedHistory, savedFind
	})
	return &board
}

func writeTranscript(t *testing.T, rows ...map[string]any) string {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range rows {
		line, _ := json.Marshal(r)
		buf.Write(append(line, '\n'))
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func prompt(text string) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}
}

func reply(text string) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
		"content": []map[string]any{{"type": "text", "text": text}}}}
}

func toolResult() map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user",
		"content": []map[string]any{{"type": "tool_result", "content": "ok"}}}}
}

func runHook(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	in, _ := json.Marshal(payload)
	var out bytes.Buffer
	stopHook(bytes.NewReader(in), &out)
	if strings.TrimSpace(out.String()) == "" {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("hook output isn't JSON: %q", out.String())
	}
	return result
}

// --- blocks ---

func TestPluckBlockWithLimit(t *testing.T) {
	found := blocks("Here:\n\n" + fence + "pluck 280\nHello “there”, $HOME `x`\n" + fence + "\n")
	want := block{Kind: "pluck", Text: "Hello “there”, $HOME `x`", Limit: 280}
	if len(found) != 1 || found[0] != want {
		t.Fatalf("got %+v", found)
	}
}

func TestQuoteLosesMarkersAndInlineMarkdown(t *testing.T) {
	found := blocks("> **Bold** and `code`\n> second line\n\nafter")
	if found[0].Text != "Bold and code\nsecond line" {
		t.Fatalf("got %q", found[0].Text)
	}
}

func TestIndentedFenceInList(t *testing.T) {
	found := blocks("1. Step\n   " + fence + "bash\n   ls -la\n   " + fence + "\n")
	if found[0].Text != "ls -la" || found[0].Kind != "bash" {
		t.Fatalf("got %+v", found[0])
	}
}

func TestFirstPluckSkipsOtherBlocks(t *testing.T) {
	md := fence + "bash\nls\n" + fence + "\n\n" + fence + "pluck\nthe one\n" + fence
	if b := firstPluck(md); b == nil || b.Text != "the one" {
		t.Fatalf("got %+v", b)
	}
}

func TestPlainKeepsMathAndSnakeCase(t *testing.T) {
	got := plain("## Title\n\n---\n\nSome **bold** and *italic* words, a_snake_name, 2 * 3 * 4.\n\n* a point")
	want := "Title\n\nSome bold and italic words, a_snake_name, 2 * 3 * 4.\n\n* a point"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestCharsCountsCharactersNotBytes(t *testing.T) {
	if n := chars("Ethiopia’s “test” 📲"); n != 19 {
		t.Fatalf("got %d", n)
	}
}

// --- stop hook ---

func TestHookCopiesFirstPluckBlock(t *testing.T) {
	board := setup(t)
	msg := "Text\n\n" + fence + "pluck\nPaste me\n" + fence + "\n\n" + fence + "bash\nls\n" + fence
	out := runHook(t, map[string]any{"last_assistant_message": msg})
	if *board != "Paste me" || out["systemMessage"] != "Copied to clipboard (8 chars)" {
		t.Fatalf("board %q, out %v", *board, out)
	}
}

func TestHookLeavesClipboardAloneWithoutClipBlock(t *testing.T) {
	board := setup(t)
	out := runHook(t, map[string]any{"last_assistant_message": fence + "bash\nls\n" + fence})
	if out != nil || *board != "what you had" {
		t.Fatalf("board %q, out %v", *board, out)
	}
}

func TestHookSendsOverLimitBlockBack(t *testing.T) {
	board := setup(t)
	out := runHook(t, map[string]any{"last_assistant_message": fence + "pluck 5\ntoo long\n" + fence})
	if out["decision"] != "block" || !strings.Contains(out["reason"].(string), "8 characters, over its 5") {
		t.Fatalf("out %v", out)
	}
	if *board != "what you had" {
		t.Fatalf("copied anyway: %q", *board)
	}
}

func TestHookCopiesOverLimitOnSecondTry(t *testing.T) {
	board := setup(t)
	out := runHook(t, map[string]any{
		"last_assistant_message": fence + "pluck 5\ntoo long\n" + fence,
		"stop_hook_active":       true,
	})
	if *board != "too long" || !strings.Contains(out["systemMessage"].(string), "still 3 over the 5 limit") {
		t.Fatalf("board %q, out %v", *board, out)
	}
}

func TestHookFallsBackToTranscriptWhenMessageIsCut(t *testing.T) {
	board := setup(t)
	path := writeTranscript(t, prompt("go"), reply(fence+"pluck\nfull text here\n"+fence))
	runHook(t, map[string]any{
		"last_assistant_message": fence + "pluck\nfull te… [+7 chars]",
		"transcript_path":        path,
	})
	if *board != "full text here" {
		t.Fatalf("board %q", *board)
	}
}

// --- transcript ---

func TestBashInputAndToolResultsAreNotPrompts(t *testing.T) {
	path := writeTranscript(t,
		prompt("write it"), reply("part one"), toolResult(), reply("part two"),
		prompt("<bash-input>pluck</bash-input>"))
	segments, current, err := lastReply(path)
	if err != nil || !current || strings.Join(segments, "|") != "part one|part two" {
		t.Fatalf("segments %q current %v err %v", segments, current, err)
	}
}

func TestNewPromptWithoutReplyReturnsPreviousReply(t *testing.T) {
	path := writeTranscript(t, prompt("first"), reply("answer"), prompt("second"))
	segments, current, _ := lastReply(path)
	if strings.Join(segments, "|") != "answer" || current {
		t.Fatalf("segments %q current %v", segments, current)
	}
}

// --- manual ---

func runManual(t *testing.T, path string, args ...string) string {
	t.Helper()
	findTranscript = func() string { return path }
	var out bytes.Buffer
	if err := manual(args, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestReplyWithoutBlocksIsCopiedWhole(t *testing.T) {
	board := setup(t)
	path := writeTranscript(t, prompt("write a long text"), reply("## Title\n\nSome **bold** words.\n\n- a point"))
	out := runManual(t, path)
	if *board != "Title\n\nSome bold words.\n\n- a point" || !strings.Contains(out, "Copied the whole reply") {
		t.Fatalf("board %q out %q", *board, out)
	}
}

func TestNumberPicksThatBlock(t *testing.T) {
	board := setup(t)
	path := writeTranscript(t, prompt("go"), reply(fence+"pluck\none\n"+fence+"\n\n"+fence+"bash\ntwo\n"+fence))
	runManual(t, path, "2")
	if *board != "two" {
		t.Fatalf("board %q", *board)
	}
}

// --- history ---

func TestUndoRestoresWhatWasReplaced(t *testing.T) {
	board := setup(t)
	put("first pluck")
	put("second pluck")
	if err := undo(&bytes.Buffer{}); err != nil || *board != "first pluck" {
		t.Fatalf("board %q err %v", *board, err)
	}
}

func TestOnlyNewestEntryKeepsReplacedText(t *testing.T) {
	setup(t)
	put("one")
	put("two")
	entries := loadHistory()
	if entries[0].Replaced != nil || entries[1].Replaced == nil || *entries[1].Replaced != "one" {
		t.Fatalf("entries %+v", entries)
	}
}

func TestHistoryNCopiesAgain(t *testing.T) {
	board := setup(t)
	put("older")
	put("newer")
	if err := history([]string{"2"}, &bytes.Buffer{}); err != nil || *board != "older" {
		t.Fatalf("board %q err %v", *board, err)
	}
}

func TestCopyingNewestAgainDoesNotRepeat(t *testing.T) {
	setup(t)
	put("same")
	put("same")
	put("other")
	put("other")
	if entries := loadHistory(); len(entries) != 2 {
		t.Fatalf("entries %+v", entries)
	}
}

func TestCopyingFromHistoryMovesItToTheTop(t *testing.T) {
	setup(t)
	put("a")
	put("b")
	put("c")
	history([]string{"3"}, &bytes.Buffer{})
	var texts []string
	for _, e := range loadHistory() {
		texts = append(texts, e.Text)
	}
	if strings.Join(texts, ",") != "b,c,a" {
		t.Fatalf("history oldest first: %v", texts)
	}
}

func TestUndoAfterRepeatStillRestoresWhatWasThere(t *testing.T) {
	board := setup(t)
	put("pluck")
	*board = "something you copied"
	put("pluck")
	undo(&bytes.Buffer{})
	if *board != "something you copied" {
		t.Fatalf("board %q", *board)
	}
}

// --- options ---

func TestOptionsWorkWithOneOrTwoDashes(t *testing.T) {
	board := setup(t)
	put("older")
	put("newer")
	if err := dispatch([]string{"-history", "2"}, nil, &bytes.Buffer{}); err != nil || *board != "older" {
		t.Fatalf("board %q err %v", *board, err)
	}
	if err := dispatch([]string{"--undo"}, nil, &bytes.Buffer{}); err != nil || *board != "newer" {
		t.Fatalf("board %q err %v", *board, err)
	}
}

func TestUnknownOptionIsNamed(t *testing.T) {
	setup(t)
	err := dispatch([]string{"--histroy"}, nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown option --histroy") {
		t.Fatalf("err %v", err)
	}
}

func TestHistoryFileIsPrivate(t *testing.T) {
	setup(t)
	put("x")
	info, err := os.Stat(historyPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", info.Mode().Perm(), err)
	}
}
