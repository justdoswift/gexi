# Changelog

## 0.0.23

- 分屏后不再淡化失焦的窗格：所有窗格保持全强度。

## 0.0.22

- 合并后的工作树又有改动时（未提交的改动，或合并之后的新提交），窗格标题栏的「归档」会变回 PR，可以直接为这部分新工作再开一个 PR；归档在这期间不出现，避免把新工作一起清掉。
- 再开 PR 会新建一个，不会把已合并的旧 PR 当成当前 PR 返回。

## 0.0.21

- 从 worktree 里的窗格拆分新窗格时，新窗格落在项目目录，不再进入同一个 checkout：工作树只属于按下 W 的那个窗格，两个窗格不会共用分支和暂存区。手动 cd 进的 worktree 仍跟随。
- agent 结束一轮或等待授权时，通知正文改成它自己那句话的第一行（去掉 markdown 标记，超过 120 字截断）；副标题仍显示 agent · 项目 · 分支。

## 0.0.20

- 侧栏项目行不再画文件夹图标：本地项目本来就放在文件夹里；远程项目仍保留服务器图标。
- 加号图标调大，和旁边的关闭叉看起来一样大。
- 窗格标题栏不再按固定宽度取舍：先留够标题和按钮，再按实测宽度放分支 pill 和路径。窗口宽时都显示，分屏变窄时依次收起；路径最多占标题栏 40%，放不下时只留最后几段。
- 识别到 agent 的窗格里，路径也保留下来（此前一检测到 Claude Code 就消失），用来分辨它在哪个 checkout。

## 0.0.19

- 窗格待在某个 git worktree 的目录里时，右上角会自动认出它，不再显示 W，而是该分支的 PR：还没 PR 时按一下就能建，有 PR 时打开它并可直接合并，合并后只留 PR 链接。
- 自动识别覆盖 cd 进 worktree、从这里拆出的窗格，以及外部工具建的工作树；离开回到项目目录后恢复 W。认出的 checkout 不属于 gexi，所以没有 Leave，也不会自动归档或随窗格关闭被删掉。

## 0.0.18

- 按 W 时不再要求先停掉窗格里的 agent。会先建好工作树，再停掉 agent，把项目里未提交的改动搬进新工作树，并用同一种 agent 在新工作树里开一个新会话；原对话不带走，Claude Code 的首条提示会说明改动来自哪里。
- 搬运失败时改动退回项目目录，空工作树删除，原会话在项目目录恢复；停不掉 agent 时提示原因。
- agent 被停止的瞬间退出窗格时保留它的位置，由这次操作收尾。

## 0.0.17

- 通知改由 gexi 自己发出，横幅上带应用名和图标，不再是「脚本编辑器」。标题是事件，副标题是代理，正文是项目与工作树分支，一眼看出是哪一处在等。
- 「等你输入」改为「需要你确认」。
- 设置 → 高级里的 DeepSeek API 密钥说明只讲密钥的用途，不再提配置文件和 DEEPSEEK_API_KEY。
- 外框按钮保持单行，说明变长或窗口变窄时标签不再折行。

## 0.0.16

- Pressing PR commits any uncommitted changes in the worktree first, with a
  message DeepSeek writes from the diff when a key is set in Settings →
  Advanced. Without a key, or when DeepSeek fails, a dialog asks for the
  message. Unresolved merge conflicts still stop it rather than committing
  the markers.
- Leaving a worktree moves into the worktree chip's pull-down menu: the label
  keeps its action, and the chevron opens Leave.
- Settings → Archived restores an archived worktree's branch and session, or
  deletes it for good.
- Merge merges on the press when GitHub would merge; the colour already says
  so, and a blocker that appeared since the last look is flashed instead.
- The sidebar stays at full strength when focus dims the other panes.
- A branch with changes Git counts no lines for takes the amber colour on its
  name rather than a separate dot.
- The app is `gexi` everywhere, with `gexi.app` as the bundle; the Application
  Support folder keeps its `Gexi` spelling, so existing data stays.
- The official site is at https://justdoswift.github.io/gexi/.

## 0.0.15

- A worktree pane's pull request and merge are separate: #N opens the pull
  request, and Merge refreshes GitHub's checks, review, and mergeability,
  asks before merging, and carries the state as its colour — green ready,
  amber while checking, red blocked. A new pull request is re-read after five
  and twenty seconds, so checks that appear after creation update the button.
- A pane restores with the agent's last resume command across restarts,
  instead of dropping back to the shell.

## 0.0.14

- PR and Merge are separate: PR #N opens the pull request, and Merge refreshes
  GitHub's checks, review, and mergeability, asks before merging, and stays
  muted while GitHub would refuse it.
- Merging a pull request no longer clears the worktree away on its own: the
  pane stays open, and the PR button becomes Archive, which closes the pane
  and removes the checkout when you press it.
- A worktree pane's PR button keeps its label once the pull request exists:
  clicking it opens a menu that merges the pull request or opens it on
  GitHub, in place of turning into a separate Merge button.

## 0.0.13

- A worktree pane can return to the project with ↩: the checkout is deleted,
  a branch with unmerged commits is kept, and uncommitted changes ask first.
  Closing a pane removes a clean checkout with nothing ahead; Settings →
  Advanced → Clean up worktrees removes unused ones, and startup prunes
  checkouts that have nothing to lose.

## 0.0.12

- A new app icon: an ink tile split like the workspace, the focused pane in
  violet. `brand/icon-product/` holds the mark, a mono variant and eight PNG
  sizes; the icns widen step reads from the 1024 render.
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
  orb: gexi merges its event hooks into your own Claude settings, guarded so
  they fire only inside a gexi pane.

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
