# JustCli

A macOS terminal workspace: split panes, a session sidebar, and a CLI that
drives the whole thing from outside the app.

This is the Go rewrite, built on [mygo](https://mygo.egoist.dev) — native UI
rendered by mygo, terminals by Ghostty's `libghostty-vt`, no cgo, no Xcode.
The window and the CLI speak the same JSON-lines protocol over a Unix socket,
so an agent running in a pane can create tabs and panes, type into them, and
read their screens.

> **Status: early.** Tabs, nested splits, focus, screen capture, persistence,
> agent session resume, and the control CLI work today. Settings and keyboard
> shortcuts are still in progress.

## Build

Requires Go 1.27+ and macOS 13+ (Apple silicon or Intel).

```sh
go tool mygo dev        # development build with live reload
go tool mygo build      # packaged .app and .dmg in build/
```

Build the CLI:

```sh
go build -o justcli ./cmd/justcli
```

## Use

The window starts with one shell. Split it, or drive it from a terminal:

```sh
justcli ping
justcli info
justcli list
justcli split --vertical -- "npm run dev"
justcli run -- "claude"
justcli capture -p 3f2a --scrollback
justcli send -e "ls -la"
justcli new-tab --cwd ~/project
justcli tab-select <tab id>
justcli focus -p <pane id>
justcli close -p <pane id>
```

Pane arguments accept a full id or any unique prefix, and default to
`$JUST_PANE` (set inside a pane) or the focused pane.

Closing the window hides it: shells and agents keep running, and clicking the
Dock icon brings the workspace back. `⌘Q` is the real quit.

On quit the layout is saved to `workspace.json` and each pane's last screen is
snapshotted as plain text; the next launch rebuilds the tabs and splits and
paints the old screens above fresh shells. A Claude Code pane comes back with
`--resume <session>`, a Codex pane with `codex resume --last`.

### Socket and storage

The control socket lives at `$JUSTCLI_HOME/control.sock`, or
`~/Library/Application Support/JustCliGo/control.sock` by default. Set
`JUSTCLI_HOME` to run a second instance beside another build.

## How it fits together

```
┌──────────────────────────────┐
│ window (mygo native UI)      │
│  ┌────────┐  ┌────────┐      │        justcli (Unix socket, JSON lines)
│  │pane    │  │pane    │      │  ◀──────────────────────────────────────
│  └───┬────┘  └───┬────┘      │        agents inside panes use
│      │           │           │        JUST_PANE / JUST_SESSION / JUST_SOCKET
│      └─────┬─────┘           │
│      terminal plugin          │
└──────────────────────────────┘
```

- `main.go` — app bootstrap, window, close-to-background.
- `workspace.go` — tabs, focus, pane resolution, main-thread marshaling.
- `split.go` — binary split tree (split / remove / collapse).
- `pane.go` — one terminal per pane.
- `ui.go` — sidebar, toolbar, recursive split rendering.
- `control.go` — Unix socket server and the control methods.
- `cmd/justcli` — the client.

## License

MIT. See [LICENSE](LICENSE).
