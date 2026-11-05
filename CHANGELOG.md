# Changelog

## Unreleased

- Worktree branches take Dota 2 hero, item, and ability names as their
  placeholder before the first prompt renames them to the task.

## 0.0.11

- A files drawer on the right shows the project's changes and files: the
  changes tab opens a file's diff, the files tab walks the folder tree and
  reads a file in place. The pill beside a pane's title shows the branch and
  the lines changed, and opens the drawer; View > Toggle Files and ⌘⇧E do
  too.
- Every local pane can own a git worktree. W in a pane's header creates one
  from the latest origin base branch and moves the pane into it; the first
  prompt asks Claude Code to name the branch after the task, and the header
  then offers PR and Merge. Merge merges the pull request on GitHub,
  fast-forwards the local base branch, and removes the worktree. The branch
  pill and the files drawer follow the pane's worktree.

## 0.0.10

- General → Interface can pin the app's appearance to Light or Dark, or leave
  it on Automatic to follow the system.
- Settings → Advanced and `gexi info` report the running version again; the
  number had been stuck at 0.0.8.

## 0.0.9

- A pane zooms to fill the session with ⌘⇧↩ or the expand button in its
  header; the inward arrow it turns into, or the same shortcut, brings every
  pane back.
- The chrome's glyphs are IconMCP outlines, tuned to the weight and ink of
  the SF Symbols they replace.
- The sidebar says an agent is at work by running a band of light across the
  session's name instead of showing a spinner beside it: the label shimmers
  while the agent works and holds full ink while it waits for you.

## 0.0.8

- A Claude Code started by hand in a pane now lights the sidebar's activity
  orb: Gexi merges its event hooks into your own Claude settings, guarded so
  they fire only inside a Gexi pane.

## 0.0.7

- The sidebar's activity loader is the thinking orb from libraries.dev/orbs:
  particles on tilted orbits while an agent works, one still frame while it
  waits for you.

## 0.0.6

- The icon is transparent around its rounded shape, so the development
  build and the packages for other platforms no longer show white corners.

## 0.0.5

- A pane moves by its title: dropped on a side of another pane it splits
  toward that side, dropped in the middle it trades places with it.
- Settings is a window of its own: a sidebar of pages (General, Notifications,
  Remote, Keyboard, Advanced) beside grouped forms, with the language, sidebar,
  notification and remote-access switches all in one place.
- Panes fade and grow in as they open, and shrink out before they close.

## 0.0.4

- The terminal font is JetBrains Mono Nerd Font Mono, embedded, so panes
  draw Powerline and devicon glyphs on a Mac without the font installed.
- The headers move and zoom the window from any blank spot, not only from
  the title text.
- Claude hooks ride along with resumed sessions, and idle notifications no
  longer leave the sidebar loader on.
- New app icon and brand marks, the update window included.
- A project stays when its last session closes, and an empty workspace
  draws a session card rather than a blank window.
- The interface speaks English and Simplified Chinese, from View >
  Language or the system's.

## 0.0.3

- Releases are signed with the Developer ID and notarized, so the disk image
  opens without the Gatekeeper warning.

## 0.0.2

- Remote Macs: one dialog turns this Mac reachable through an ssh relay and
  hands over a connection code; pasting the code on another Mac adds it to
  the Remote menu.
- Remote row moved to the bottom of the sidebar.
- The app updates itself from signed GitHub releases.
