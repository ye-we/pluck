package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A block is a fenced code block or a > quote in a reply.
type block struct {
	Kind   string // the fence's language tag ("pluck", "bash", ...), "code" or "quote"
	Text   string
	Limit  int // character limit from a "pluck 280" tag, 0 if none
	Hidden int // hidden characters removed from Text (see clean)
}

var (
	fenceLine   = regexp.MustCompile("^(\\s*)(```+|~~~+)\\s*([^`]*)$")
	quoteMarker = regexp.MustCompile(`^[ \t]*>[ \t]?`)
	boldStars   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	boldUnders  = regexp.MustCompile(`__(.+?)__`)
	inlineCode  = regexp.MustCompile("`([^`\\n]+)`")
	headingMark = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	blankRuns   = regexp.MustCompile(`\n{3,}`)
)

// blocks returns the fenced code blocks and > quotes in markdown, in order.
func blocks(markdown string) []block {
	var found []block
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); {
		if m := fenceLine.FindStringSubmatch(lines[i]); m != nil {
			indent, marker := len(m[1]), m[2]
			info := strings.Fields(m[3])
			b := block{Kind: "code"}
			if len(info) > 0 {
				b.Kind = info[0]
			}
			if len(info) > 1 {
				if n, err := strconv.Atoi(info[1]); err == nil && n > 0 {
					b.Limit = n
				}
			}
			var body []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), marker); i++ {
				line := lines[i]
				if indent > 0 && len(line) >= indent && strings.TrimSpace(line[:indent]) == "" {
					line = line[indent:]
				}
				body = append(body, line)
			}
			b.Text, b.Hidden = clean(strings.Trim(strings.Join(body, "\n"), "\n"))
			found = append(found, b)
			i++
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), ">") {
			var body []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), ">"); i++ {
				body = append(body, quoteMarker.ReplaceAllString(lines[i], ""))
			}
			b := block{Kind: "quote"}
			b.Text, b.Hidden = clean(unmark(strings.TrimSpace(strings.Join(body, "\n"))))
			found = append(found, b)
			continue
		}
		i++
	}
	return found
}

func firstPluck(markdown string) *block {
	for _, b := range blocks(markdown) {
		if b.Kind == "pluck" {
			return &b
		}
	}
	return nil
}

// unmark drops inline markdown that shouldn't reach a form field.
func unmark(text string) string {
	text = boldStars.ReplaceAllString(text, "$1")
	text = boldUnders.ReplaceAllString(text, "$1")
	text = emphasis(text, '*')
	text = emphasis(text, '_')
	return inlineCode.ReplaceAllString(text, "$1")
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// emphasis removes *single* (or _single_) emphasis marks. A mark only counts
// when it hugs a word on the inside and isn't glued to a word on the outside,
// so "2 * 3 * 4" and snake_case names survive.
func emphasis(text string, mark rune) string {
	rs := []rune(text)
	var out strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] != mark {
			out.WriteRune(rs[i])
			continue
		}
		opens := (i == 0 || (rs[i-1] != mark && !isWord(rs[i-1]))) &&
			i+1 < len(rs) && rs[i+1] != mark && !unicode.IsSpace(rs[i+1])
		closeAt := -1
		if opens {
			for j := i + 1; j < len(rs) && rs[j] != '\n'; j++ {
				if rs[j] == mark {
					closeAt = j
					break
				}
			}
		}
		if closeAt > i+1 && !unicode.IsSpace(rs[closeAt-1]) &&
			(closeAt+1 == len(rs) || (rs[closeAt+1] != mark && !isWord(rs[closeAt+1]))) {
			out.WriteString(string(rs[i+1 : closeAt]))
			i = closeAt
			continue
		}
		out.WriteRune(rs[i])
	}
	return out.String()
}

// isDivider reports whether a line is a markdown horizontal rule: three or
// more of the same -, * or _ and nothing else but spaces.
func isDivider(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.ContainsRune("-*_", rune(trimmed[0])) {
		return false
	}
	count := 0
	for _, r := range trimmed {
		switch {
		case r == rune(trimmed[0]):
			count++
		case r != ' ' && r != '\t':
			return false
		}
	}
	return count >= 3
}

// plain turns a whole reply into plain text: no headings, dividers or
// inline markdown.
func plain(markdown string) string {
	text := headingMark.ReplaceAllString(markdown, "")
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if !isDivider(line) {
			kept = append(kept, line)
		}
	}
	text = blankRuns.ReplaceAllString(unmark(strings.Join(kept, "\n")), "\n\n")
	return strings.TrimSpace(text)
}

// hidden reports whether r is a character you can't see that changes what a
// paste does: terminal control codes (a pasted escape code can end a shell's
// paste protection, a carriage return can run a line), text direction
// overrides that reorder what's shown, and tag characters, invisible text
// that AI models read. Zero-width joiners stay: emoji made of several emoji,
// like a family, need them.
func hidden(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200b || r == 0xfeff:
		return true
	case r >= 0xe0000 && r <= 0xe007f:
		return true
	}
	return false
}

// clean removes hidden characters from text that's about to be pasted
// somewhere, and says how many it removed.
func clean(text string) (string, int) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	removed := 0
	text = strings.Map(func(r rune) rune {
		if hidden(r) {
			removed++
			return -1
		}
		return r
	}, text)
	return text, removed
}

// chars counts characters the way people and form fields do, not bytes.
func chars(text string) int {
	return utf8.RuneCountInString(text)
}

func preview(text string) string {
	const width = 60
	text, _ = clean(text)
	flat := []rune(strings.Join(strings.Fields(text), " "))
	if len(flat) <= width {
		return string(flat)
	}
	return string(flat[:width-3]) + "..."
}
