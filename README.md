# pluck

Pluck text out of Claude Code's replies and onto your clipboard, clean.

Selecting text in a terminal picks up line breaks where it wrapped, indentation and box characters. pluck skips the terminal: Claude marks the text you'll paste, and it lands on your clipboard exactly as written.

## Install

In Claude Code:

```
/plugin marketplace add ye-we/pluck
/plugin install pluck@pluck
```

Then start a new session. There's nothing else to install: pluck ships ready-to-run programs for macOS, Linux and Windows.

## How it works

1. At the start of each session, pluck tells Claude to put any writing you ask for (posts, emails, essays, bios, form answers, store listings, PR descriptions) and anything you'll paste elsewhere (like a command for another terminal) in a `pluck` block:

   ````
   ```pluck
   The exact text to paste.
   ```
   ````

2. When Claude finishes a reply, the first pluck block is copied to your clipboard, and you see `Copied to clipboard (N chars)`.

Ordinary code blocks are never copied automatically.

### Character limits

When the destination has a limit, Claude tags the block with it, like ` ```pluck 280 `. If the text is over, pluck sends it back to Claude to shorten before copying anything. If it's still over after one retry, pluck copies it anyway and tells you by how much.

## Copy by hand

Type these at the Claude prompt. The `!` runs them as shell commands, and the copy itself takes a fraction of a second. You can also run `pluck` in any other terminal, or bind it to a key with the macOS Shortcuts app (a Run Shell Script action), to copy without touching the Claude session.

| Command | What it does |
| --- | --- |
| `!pluck` | Copy the first pluck block of the last reply, else its first block, else the whole reply as plain text |
| `!pluck 2` | Copy the 2nd block (code block or `>` quote) |
| `!pluck -l` | List the blocks in the last reply |
| `!pluck --history` | List recent copies |
| `!pluck --history 3` | Copy item 3 from that list again |
| `!pluck --undo` | Put back what was on your clipboard before the last copy |

To use `pluck` outside Claude Code too, clone this repo and link `bin/pluck` onto your PATH:

```
ln -s "$PWD/bin/pluck" /usr/local/bin/pluck
```

## Platforms

- **macOS:** `pbcopy`. Tested.
- **Linux:** `wl-copy` on Wayland, `xclip` or `xsel` on X11.
- **Windows and WSL:** PowerShell `Set-Clipboard`. On Windows, Claude Code runs hooks through Git Bash, which pluck's launcher needs.
- **Over SSH:** OSC 52, which asks your local terminal to set the clipboard. Works in iTerm2, Ghostty, kitty, WezTerm and others. Inside tmux, add `set -g set-clipboard on` to your tmux config.

Only macOS has been tested so far. Reports from other setups are welcome.

## Privacy

pluck keeps your last 50 copies in `~/.claude/pluck/history.jsonl`, readable only by you. For `--undo` it also keeps what your clipboard held before the latest copy, and only that one, so things like copied passwords don't pile up. Delete the file any time.

Nothing is sent anywhere.

## How it finds the text

The automatic copy reads the reply Claude Code hands to its Stop hook. The manual commands read the session transcript in `~/.claude/projects`. That format is internal to Claude Code, so an update could break the manual commands until pluck catches up.

## Development

pluck is written in Go. The plugin ships prebuilt programs in `dist/`, and `bin/pluck` is a small launcher that runs the right one for the machine.

```
go test ./...
./scripts/build.sh        # tests, then rebuilds dist/ for every platform
claude plugin validate .
```

Commit the rebuilt `dist/` along with your change, and bump the version in `.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json`.

## License

MIT
