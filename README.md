# Gexi

A macOS terminal workspace: split panes, a session sidebar, and a CLI that
drives the whole thing from outside the app.

Built on [mygo](https://mygo.egoist.dev) — native UI rendered by mygo,
terminals by Ghostty's `libghostty-vt`, no cgo, no Xcode.
The window and the CLI speak the same JSON-lines protocol over a Unix socket,
so an agent running in a pane can create tabs and panes, type into them, and
read their screens.

> **Status: early.** Tabs, nested splits, focus, keyboard shortcuts, screen
> capture, persistence, agent session resume, and the control CLI work today.
> The settings window is minimal, and remote access is not there yet.

## Build

Requires Go 1.27+ and macOS 13+ (Apple silicon or Intel).

```sh
go tool mygo dev        # development build with live reload
go tool mygo build      # packaged .app and .dmg in build/
```

Build the CLI:

```sh
go build -o gexi ./cmd/gexi
```

## Use

The window starts with one shell. Split it, or drive it from a terminal:

```sh
gexi ping
gexi info
gexi list
gexi split --vertical -- "npm run dev"
gexi run -- "claude"
gexi capture -p 3f2a --scrollback
gexi send -e "ls -la"
gexi new-tab --cwd ~/project
gexi tab-select <tab id>
gexi focus -p <pane id>
gexi close -p <pane id>
```

Pane arguments accept a full id or any unique prefix, and default to
`$GEXI_PANE` (set inside a pane) or the focused pane.

### Shortcuts

| Action | Keys |
|---|---|
| New tab | ⌘T |
| Split right | ⌘D |
| Split down | ⌘⇧D |
| Focus previous / next pane | ⌘[ / ⌘] |
| Close pane | ⌘W |
| Settings | ⌘, |
| Quit (kills processes) | ⌘Q |

The window follows Rex's chrome: no title bar, the traffic lights over the
top-left corner, and a sidebar toggle that never moves. With the sidebar
hidden, the top-left pane's header makes room for them; the other panes keep
their own alignment.

Closing the window hides it: shells and agents keep running, and clicking the
Dock icon brings the workspace back. `⌘Q` is the real quit.

On quit the layout is saved to `workspace.json` and each pane's last screen is
snapshotted as plain text; the next launch rebuilds the tabs and splits and
paints the old screens above fresh shells. Agent panes resume their newest
session in the pane's directory: Claude Code with `--resume <session>`,
Codex with `codex resume --last`, and omp, Devin, and Grok with
`--continue`; Droid with `droid --resume --last`. A detected agent also
names its pane and sidebar tab with its own logo instead of the terminal
title.

### Agent events and notifications

A pane's screen says what the agent printed, not what it is doing. When a
Claude Code pane is launched (`gexi run -- claude`, or a new tab), Gexi
passes an extra `--settings` file of hooks, so the agent reports its own
lifecycle over the socket:

| Hook | Event | Pane state |
|---|---|---|
| `UserPromptSubmit` | `turn.start` | `working` |
| `Stop` | `turn.end` | `idle` |
| `Notification` | `needs-input` | `waiting` |
| `SessionStart` / `SessionEnd` | `session.start` / `session.end` | `idle` |

The state is in `gexi list --json` under `activity`. `turn.end` and
`needs-input` also post a notification. The settings file is layered on top of
the user's own, so existing hooks keep firing.

Any process in a pane can report the same events:

```sh
gexi event turn.start --agent claude
gexi event needs-input --agent codex -p 3f2a
echo '{"session_id":"…"}' | gexi event turn.end --agent claude
```

Events only cover agents Gexi launched or that a hook points at. A `claude`
typed into a plain shell has no hooks, and there `activity` stays unset.

### Socket and storage

The control socket lives at `$GEXI_HOME/control.sock`, or
`~/Library/Application Support/Gexi/control.sock` by default. Set
`GEXI_HOME` to run a second instance beside another build.

## How it fits together

```
┌──────────────────────────────┐
│ window (mygo native UI)      │
│  ┌────────┐  ┌────────┐      │        gexi (Unix socket, JSON lines)
│  │pane    │  │pane    │      │  ◀──────────────────────────────────────
│  └───┬────┘  └───┬────┘      │        agents inside panes use
│      │           │           │        GEXI_PANE / GEXI_SESSION / GEXI_SOCKET
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
- `cmd/gexi` — the client.

## License

MIT. See [LICENSE](LICENSE).
