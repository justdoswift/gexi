# gexi

A macOS terminal workspace: projects, sessions, split panes, and a CLI that
drives the whole thing from outside the app.

The site lives at <https://justdoswift.github.io/gexi/> and is built from
`docs/` on `main`; open `docs/index.html` to work on it locally.

Built on [mygo](https://mygo.egoist.dev) — native UI rendered by mygo,
terminals by Ghostty's `libghostty-vt`, no cgo, no Xcode.
The window and the CLI speak the same JSON-lines protocol over a Unix socket,
so an agent running in a pane can create projects, sessions, and panes, type
into them, and read their screens.

> **Status: early.** Projects and sessions, nested splits, focus, keyboard
> shortcuts, screen capture, persistence, agent session resume, a files drawer
> with changes and diffs, and the control CLI work today. Remote hosts work
> over ssh; there is no remote agent yet.
> Settings has a page each for general options, notifications, remote access,
> shortcuts, and storage.

## Build

Requires Go 1.27+ and macOS 13+ (Apple silicon or Intel).

```sh
go tool mygo dev        # development build with live reload
go tool mygo build      # packaged .app and .dmg in build/
scripts/build.sh        # mygo build; widens the .icns on ad-hoc builds only
```

Production builds are signed with the Developer ID in `mygo.json` and the
`.dmg` is notarized with the `justwrite-notary` keychain profile, then stapled:

```sh
go tool mygo build      # signed .app and notarized .dmg in build/
```

mygo compiles `AppIcon.icon` into an `.icns` capped at 256 px. macOS 26+ takes
the full-size icon from `Assets.car`, so this only matters on older systems.
`scripts/build.sh` widens the `.icns` from
`brand/icon-product/final/png/gexi-icon-1024.png` for
ad-hoc development builds; it leaves a Developer ID signed bundle alone, since
replacing a resource voids its signature and notarization ticket
(`FORCE_ICNS_PATCH=1` overrides that).

Build the CLI:

```sh
go build -o gexi ./cmd/gexi
```

## Releases

Installed copies update themselves from signed releases on GitHub
(`justdoswift/gexi`), configured by `updates` in `mygo.json`. The app checks
once a day in the background, and **gexi → Check for Updates…** checks on
demand.

Build a release with the update signing key, which also writes the update
archive and manifest next to the installer:

```sh
export MYGO_UPDATER_PRIVATE_KEY="$(cat ~/Library/Application\ Support/mygo/update-keys/mygo-update.key)"
go tool mygo build -upload    # draft GitHub release tagged v<version>
```

The key stays out of the repository: installed apps accept only updates
signed with it. Release notes come from the `## <version>` section of
`CHANGELOG.md`.

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

Panes also move by hand: drag a pane's title onto a side of another pane to
split toward that side, or onto its middle to trade places. A pane alone in
the window has nowhere to go, so its title keeps moving the window.

`⌘⇧↩`, or the expand button in a pane's header, fills the session with that
pane; the inward arrow it turns into, or the same shortcut, brings every pane
back.

### Worktrees and pull requests

Open a pane and press **W** in its header to give that pane its own git
worktree, branched from the latest `origin` base branch and kept under the
app's data folder. The pane moves into the worktree with a fresh shell. The
branch starts with a placeholder Dota 2 name — a hero, item, or ability; the
first prompt asks Claude Code to rename it to the task. The header then shows **PR**, which
pushes the branch and opens a pull request with `gh`; any pending changes are
committed first, with a message DeepSeek writes from the diff when a key is
set in Settings → Advanced. Without a key, or when DeepSeek fails, a small
dialog asks for the message. `DEEPSEEK_API_KEY` also supplies the key, and
`DEEPSEEK_MODEL` and `DEEPSEEK_BASE_URL` override the model and endpoint.
Once it exists, **PR #N**
opens the pull request on GitHub, and **Merge** reports the state by its
colour: green when GitHub would merge, amber while checks or reviews are still
pending, red when something blocks it. Merge merges on the press, and a blocked
press says why. Merging fast-forwards your local base branch and leaves the
pane in the worktree. The button then becomes **Archive**, which closes the
pane and clears the checkout away when you press it. The branch is kept, so
**Settings → Archived** lists it until you restore it (the checkout comes
back and its session opens again) or delete it (the branch goes too). Panes
that never press W work exactly as before. Opening and merging pull requests
needs the GitHub CLI, authenticated.

**↩** beside PR leaves the checkout: it returns the pane to the project
folder and deletes the worktree. A branch with unmerged commits is kept, and
uncommitted changes ask first. Closing a pane removes a clean checkout with
nothing ahead; otherwise the worktree stays. Settings → Advanced → Clean up
worktrees removes unused ones, and startup does the same for checkouts that
have nothing to lose.

### Remote hosts

The **Remote** row at the bottom of the sidebar is the whole feature.

**Reaching another Mac.** On that Mac, open Remote → *Allow this Mac to be
reached…*, enter the relay (`user@host`, any ssh account you already have),
and confirm. gexi writes a key, keeps a reverse tunnel
(`ssh -R 127.0.0.1:<port>:localhost:22`) to the relay, and copies a
connection code. On this Mac, Remote → *Connect to another Mac…*, paste the
code, and the Mac appears in the menu from then on. The code carries the
key, so treat it like a password; turning remote access off revokes it.
The tunnel restarts on launch and after a dropped link.

**Hosts in `~/.ssh/config`** are listed in the same menu (wildcards and `!`
patterns are skipped, `Include` is followed). Choosing one opens a session in
a project named after the host. The pane runs ssh with your own config, so
`ProxyJump`, identities, and the rest apply unchanged, plus:

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
| Zoom pane / show all | ⌘⇧↩ |
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
Claude Code pane is launched (`gexi run -- claude`, or a new session), gexi
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

Events only cover agents gexi launched or that a hook points at. A `claude`
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
