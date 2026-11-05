# Changelog

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
