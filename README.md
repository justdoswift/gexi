# Gexi

A macOS terminal workspace: projects, sessions, split panes, and a CLI that
drives the whole thing from outside the app.

Built on [mygo](https://mygo.egoist.dev) — native UI rendered by mygo,
terminals by Ghostty's `libghostty-vt`, no cgo, no Xcode.
The window and the CLI speak the same JSON-lines protocol over a Unix socket,
so an agent running in a pane can create projects, sessions, and panes, type
into them, and read their screens.

> **Status: early.** Projects and sessions, nested splits, focus, keyboard
> shortcuts, screen capture, persistence, agent session resume, and the control
> CLI work today. Remote hosts work over ssh; there is no remote agent yet.
> The settings window is minimal.

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

A project is a folder; the sessions opened in it hold the panes. The window
starts with one project and a shell in it. Split it, or drive it from a terminal:

```sh
gexi ping
gexi info
gexi list
gexi split --vertical -- "npm run dev"
gexi run -- "claude"
gexi capture -p 3f2a --scrollback
gexi send -e "ls -la"
gexi new-session --cwd ~/project
gexi session-select <session id>
gexi new-project --cwd ~/project
gexi focus -p <pane id>
gexi close -p <pane id>
```

Pane arguments accept a full id or any unique prefix, and default to
`$GEXI_PANE` (set inside a pane) or the focused pane.

### Remote hosts

The server button in the sidebar header lists the concrete hosts in
`~/.ssh/config` (wildcards and `!` patterns are skipped, `Include` is
followed). Choosing one opens a session in a project named after the host.
The pane runs ssh with your own config, so `ProxyJump`, identities, and the
rest apply unchanged, plus:

- `ControlMaster=auto` with `ControlPersist=10m`, so every pane to the same
  host shares one connection and the next session reuses it.
- `ServerAliveInterval=15`, so a dead link is noticed while you are away.

With "Keep remote sessions" on (the default, in Settings → Remote), the
remote command is `tmux new -A -s gexi-<id> || exec ${SHELL:-sh} -l`: each
pane gets its own tmux session, a dropped link reconnects to the same shell,
and a host without tmux still gets a login shell. Remote panes restore like
local ones — the saved layout reruns the ssh command and reattaches.

What stays local is agent detection: `gexi` sees the ssh process, not the
agent on the far side, so a remote `claude` does not report activity.

### Shortcuts

| Action | Keys |
|---|---|
| New session | ⌘T |
| New project | ⌘⇧N |
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
snapshotted as plain text; the next launch rebuilds the projects, sessions, and splits and
paints the old screens above fresh shells. Agent panes resume their newest
session in the pane's directory: Claude Code with `--resume <session>`,
Codex with `codex resume --last`, and omp, Devin, and Grok with
`--continue`; Droid with `droid --resume --last`. A detected agent also
names its pane and sidebar row with its own logo instead of the terminal
title.

### Agent events and notifications

A pane's screen says what the agent printed, not what it is doing. When a
Claude Code pane is launched (`gexi run -- claude`, or a new session), Gexi
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
│      │           │           │        GEXI_PANE / GEXI_INSTANCE / GEXI_SOCKET
│      └─────┬─────┘           │
│      terminal plugin          │
└──────────────────────────────┘
```

- `main.go` — app bootstrap, window, close-to-background.
- `workspace.go` — projects, sessions, focus, pane resolution, main-thread
  marshaling.
- `split.go` — binary split tree (split / remove / collapse).
- `pane.go` — one terminal per pane.
- `ui.go` — sidebar, toolbar, recursive split rendering.
- `control.go` — Unix socket server and the control methods.
- `cmd/gexi` — the client.

## License

MIT. See [LICENSE](LICENSE).
