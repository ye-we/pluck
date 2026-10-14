package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Swapped out in tests.
var (
	copyText  = systemCopy
	pasteText = systemPaste
)

const (
	psCopy  = "[Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"
	psPaste = "[Console]::OutputEncoding=[Text.Encoding]::UTF8; Get-Clipboard -Raw"
)

type backend struct{ copy, paste []string }

func remote() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}

func wsl() bool {
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(release)), "microsoft")
}

// backends lists the clipboard tools that can work here, best first.
func backends() []backend {
	var all []backend
	if runtime.GOOS == "darwin" {
		all = append(all, backend{[]string{"pbcopy"}, []string{"pbpaste"}})
	} else {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			all = append(all, backend{[]string{"wl-copy"}, []string{"wl-paste", "--no-newline"}})
		}
		if os.Getenv("DISPLAY") != "" {
			all = append(all,
				backend{[]string{"xclip", "-selection", "clipboard"}, []string{"xclip", "-selection", "clipboard", "-o"}},
				backend{[]string{"xsel", "--clipboard", "--input"}, []string{"xsel", "--clipboard", "--output"}})
		}
		if runtime.GOOS == "windows" || wsl() {
			ps := []string{"powershell.exe", "-NoProfile", "-Command"}
			all = append(all, backend{append(ps, psCopy), append(append([]string{}, ps...), psPaste)})
		}
	}
	var usable []backend
	for _, b := range all {
		if _, err := exec.LookPath(b.copy[0]); err == nil {
			usable = append(usable, b)
		}
	}
	return usable
}

func run(args []string, input []byte) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.Output()
	return out, err == nil
}

// osc52 asks the terminal itself to set the clipboard. It works over SSH in
// terminals that support OSC 52 (iTerm2, Ghostty, kitty, WezTerm, ...).
func osc52(text string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	_, err = fmt.Fprintf(tty, "\033]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err == nil
}

func systemCopy(text string) bool {
	// Over SSH the local tools would fill the server's clipboard, not yours.
	if !remote() {
		for _, b := range backends() {
			if _, ok := run(b.copy, []byte(text)); ok {
				return true
			}
		}
	}
	return osc52(text)
}

func systemPaste() (string, bool) {
	if remote() {
		return "", false
	}
	for _, b := range backends() {
		if out, ok := run(b.paste, nil); ok {
			return string(out), true
		}
	}
	return "", false
}
