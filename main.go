// Command pluck copies text from Claude Code's last reply to the clipboard,
// without the markdown and terminal wrap junk that selecting it brings along.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const usage = `pluck: copy text from Claude's last reply to the clipboard, cleanly.

Claude puts paste-ready text in a fenced block tagged "pluck" (or "pluck 280"
to add a character limit). When Claude finishes a reply, the Stop hook copies
the first pluck block. Run pluck yourself (!pluck at the Claude prompt) to pick
a different one.

  pluck              the first pluck block of the last reply, else its first
                     block, else the whole reply as plain text
  pluck N            the Nth block (code block or > quote) of the last reply
  pluck -l           list the blocks in the last reply
  pluck --history    list recent copies; pluck --history N copies item N again
  pluck --undo       put back what was on the clipboard before the last copy`

// The ^ marks stand in for backticks, which a Go raw string can't hold.
var instructions = strings.ReplaceAll(`The pluck plugin is installed. Whenever your reply delivers a piece of writing the user asked for (a post, email, message, bio, description, essay, story, form answer, store listing, PR description, any "write me..." request) or text they'll paste somewhere else (including a command for another terminal), put exactly that text in a fenced code block whose info string is ^pluck^, like ^^^pluck on its own line, then the text, then ^^^. When you finish your reply, the first pluck block is copied to the user's clipboard automatically.

- If the destination has a character limit, add it after the tag, like ^^^pluck 280. A block over its limit is sent back to you to shorten.
- Put only the deliverable inside: no surrounding quotes, no notes to the user. Paragraphs and line breaks are fine. Leave out markdown formatting (headings, bold, bullet syntax) unless the user wants markdown.
- Use one pluck block per reply unless the user asks for several. They can copy others with ^!pluck N^.
- Don't use pluck blocks for explanations, answers to questions, code you're explaining, or files you're editing.`, "^", "`")

var truncated = regexp.MustCompile(`… \[\+\d+ chars\]\s*$`)

func say(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func sessionStart(w io.Writer) {
	say(w, map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":     "SessionStart",
		"additionalContext": instructions,
	}})
}

type stopPayload struct {
	LastAssistantMessage string `json:"last_assistant_message"`
	TranscriptPath       string `json:"transcript_path"`
	StopHookActive       bool   `json:"stop_hook_active"`
}

// stopHook copies the first pluck block of the reply Claude just finished.
func stopHook(r io.Reader, w io.Writer) {
	var p stopPayload
	json.NewDecoder(r).Decode(&p)

	// Prefer the message Claude Code hands the hook. Fall back to the
	// transcript if it's missing or cut short.
	var b *block
	if !truncated.MatchString(p.LastAssistantMessage) {
		b = firstPluck(p.LastAssistantMessage)
	}
	if b == nil && p.TranscriptPath != "" {
		// The final message can land in the transcript a moment after Stop.
		for try := 0; try < 6; try++ {
			segments, current, err := lastReply(p.TranscriptPath)
			if err != nil {
				return
			}
			if current {
				for i := len(segments) - 1; i >= 0 && b == nil; i-- {
					b = firstPluck(segments[i])
				}
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	if b == nil {
		return
	}

	n := chars(b.Text)
	over := b.Limit > 0 && n > b.Limit
	if over && !p.StopHookActive {
		say(w, map[string]string{
			"decision": "block",
			"reason": fmt.Sprintf("The pluck block is %d characters, over its %d character limit. "+
				"Rewrite it in a new pluck block (keep the ```pluck %d tag) so it fits.", n, b.Limit, b.Limit),
		})
		return
	}
	if put(b.Text) {
		note := fmt.Sprintf("Copied to clipboard (%d chars)", n)
		if over {
			note = fmt.Sprintf("Copied to clipboard (%d chars, still %d over the %d limit)", n, n-b.Limit, b.Limit)
		}
		say(w, map[string]string{"systemMessage": note})
	}
}

func ago(seconds float64) string {
	for _, u := range []struct {
		name string
		size float64
	}{{"d", 86400}, {"h", 3600}, {"m", 60}} {
		if seconds >= u.size {
			return fmt.Sprintf("%d%s ago", int(seconds/u.size), u.name)
		}
	}
	return "just now"
}

func pick(arg string, count int, hint string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > count {
		return 0, fmt.Errorf("pick 1-%d (see %s)", count, hint)
	}
	return n, nil
}

func history(args []string, w io.Writer) error {
	entries := loadHistory()
	if len(entries) == 0 {
		return errors.New("nothing copied yet")
	}
	newestFirst := make([]historyEntry, len(entries))
	for i, e := range entries {
		newestFirst[len(entries)-1-i] = e
	}
	if len(args) > 0 {
		n, err := pick(args[0], len(newestFirst), "pluck --history")
		if err != nil {
			return err
		}
		text := newestFirst[n-1].Text
		if !put(text) {
			return errors.New("couldn't reach a clipboard")
		}
		fmt.Fprintf(w, "Copied %d chars: %s\n", chars(text), preview(text))
		return nil
	}
	now := float64(time.Now().UnixNano()) / 1e9
	for i, e := range newestFirst {
		if i == 20 {
			break
		}
		fmt.Fprintf(w, "%2d. %8s  %5d chars  %s\n", i+1, ago(now-e.Time), chars(e.Text), preview(e.Text))
	}
	return nil
}

func undo(w io.Writer) error {
	entries := loadHistory()
	if len(entries) == 0 || entries[len(entries)-1].Replaced == nil {
		return errors.New("nothing to undo")
	}
	text := *entries[len(entries)-1].Replaced
	entries[len(entries)-1].Replaced = nil
	saveHistory(entries)
	if !copyText(text) {
		return errors.New("couldn't reach a clipboard")
	}
	fmt.Fprintf(w, "Restored what you had copied before (%d chars)\n", chars(text))
	return nil
}

// manual handles pluck, pluck N and pluck -l.
func manual(args []string, w io.Writer) error {
	path := findTranscript()
	if path == "" {
		return errors.New("no Claude Code transcript found")
	}
	segments, _, err := lastReply(path)
	if err != nil {
		return err
	}
	reply := strings.Join(segments, "\n\n")
	found := blocks(reply)

	if len(args) > 0 && args[0] == "-l" {
		if len(found) == 0 {
			fmt.Fprintln(w, "No blocks in the last reply. Run pluck to copy all of it.")
		}
		for i, b := range found {
			limit := ""
			if b.Limit > 0 {
				limit = fmt.Sprintf("/%d", b.Limit)
			}
			fmt.Fprintf(w, "%d. [%s] %d%s chars  %s\n", i+1, b.Kind, chars(b.Text), limit, preview(b.Text))
		}
		return nil
	}

	if len(found) == 0 {
		if len(args) > 0 {
			return errors.New("the last reply has no code block or quote; run pluck to copy all of it")
		}
		text := plain(reply)
		if text == "" {
			return errors.New("the last reply has no text")
		}
		if !put(text) {
			return errors.New("couldn't reach a clipboard")
		}
		fmt.Fprintf(w, "Copied the whole reply (%d chars): %s\n", chars(text), preview(text))
		return nil
	}

	b := found[0]
	if len(args) > 0 {
		n, err := pick(args[0], len(found), "pluck -l")
		if err != nil {
			return err
		}
		b = found[n-1]
	} else {
		for _, candidate := range found {
			if candidate.Kind == "pluck" {
				b = candidate
				break
			}
		}
	}
	if !put(b.Text) {
		return errors.New("couldn't reach a clipboard")
	}
	n, warning := chars(b.Text), ""
	if b.Limit > 0 && n > b.Limit {
		warning = fmt.Sprintf(" (%d over the %d limit)", n-b.Limit, b.Limit)
	}
	fmt.Fprintf(w, "Copied %d chars%s: %s\n", n, warning, preview(b.Text))
	return nil
}

// dispatch runs one command. Options work with one dash or two.
func dispatch(args []string, in io.Reader, out io.Writer) error {
	option := ""
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		option = strings.TrimLeft(args[0], "-")
	}
	switch option {
	case "":
		return manual(args, out)
	case "l", "list":
		return manual([]string{"-l"}, out)
	case "history":
		return history(args[1:], out)
	case "undo":
		return undo(out)
	case "h", "help":
		fmt.Fprintln(out, usage)
	case "hook":
		stopHook(in, out)
	case "session-start":
		sessionStart(out)
	default:
		return fmt.Errorf("unknown option %s (see pluck -h)", args[0])
	}
	return nil
}

func main() {
	if err := dispatch(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pluck:", err)
		os.Exit(1)
	}
}
