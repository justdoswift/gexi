# gexi 远程工作台：实施计划

2026-11-07。接 `~/Library/Application Support/gexi/remote-access-plan.md`。
方向已定：**B —— 云服务器只做转发，gexid 留在自己的 Mac 上主动出站。**

## 现状核对（2026-11-07 实测）

- `go build ./...` 通过，`go test -run 'Remote|Relay|SafeName|AuthorizedKey'` 通过。
- `remote.go`（14KB）已经是完整的 ssh 中继方案：反向隧道、连接码、导入、重连。
- 但它只是 ssh 登录：远端 agent 状态、文件抽屉、worktree、通知全部丢失。
- 连接码明文携带私钥（`remoteCode.Key`），端口按设备名 crc32 映射（22000-24000），同名设备会撞。
- 本机 `settings.json` 里 `remoteListen: false`，远程功能从未启用过。

## 三个接缝（已核实，这是全部改动面）

1. **`pane.go:241` `terminal.Options{Conn: connOf(p)}`**
   `Conn` 是 `io.ReadWriteCloser`，且支持可选的 `Resize(cols, rows)`。
   `connOf(p)` 现在返回具体类型 `*ptyConn`，改成接口即可塞网络流。
   远程窗格的渲染、拆分、缩放、选中与本地完全一致，不需要另写终端。

2. **`control.go` 的 `dispatch`**
   已经是完整的工作台 API：`workspace.info` / `project.new` / `session.new` /
   `pane.list` / `pane.text` / `pane.send` / `pane.split` / `pane.close` /
   `pane.focus` / `event` / `app.settings`。远程协议直接复用这套 JSON-lines，
   不新造一套动词。

3. **`workspace` 的 headless 可行性**
   - `terminal.New` 只调 `Load()`，不依赖 `mygo.App` 初始化。无 view 时半秒后按 80×24 启动。
   - `w.invalidate()` 已有 `w.win != nil` 保护，headless 下是空操作。
   - `w.onMain()` **没有保护**，headless 会死锁（`w.win.Update` 空指针）。
     这是 headless 化的第一件事：`onMain` 在 `w.win == nil` 时直接同步执行。
   - `p.term` 只用了 5 处（`Title` / `Dir` / `Send` / `Text` / `Close`），
     `pane.text()` 和 `pane.send()` 都走它，headless 下保留一个无 view 的
     `terminal.New` 实例即可，不需要重写。

## 分阶段

### 阶段 0：headless 地基（不动远程，先让 workspace 能无窗口跑）

- `onMain` 加 `w.win == nil` 同步执行分支。
- 新增 `cmd/gexid`：`newWorkspace()` + `startControl()` 的 headless 版本，
  监听 TCP 而不是 Unix socket，先只做 `ping` / `workspace.info` / `pane.list`。
- 验收：`gexid` 在终端里起来，`gexi` CLI 能连上它读到 pane 列表。

### 阶段 1：远程窗格协议

- 协议消息：`panes.list` / `pane.attach` / `pane.resize` / `pane.detach` / `event`。
- 每个窗格一条字节流，与 JSON-lines 控制通道多路复用（长度前缀帧）。
- `pane.attach` 返回 pane id 后，客户端用这条流当 `terminal.Options.Conn`。
- 断线重连按 pane id 重新挂载，远端 shell 不随连接退出（不需要 tmux）。
- 验收：本地 gexi 开一个远程窗格，跑 `htop`，缩放窗口布局正确。

### 阶段 2：出站连接 + 云转发

- gexid 主动出站连云服务器，设备 token 认证，断线指数退避重连。
- 云服务器只做：设备配对、token 校验、双向密文转发。不解析内容。
- 内层 TLS：客户端与 gexid 之间端到端加密，服务器转发时看不到明文。
- 验收：两台 Mac 在不同网络下，A 连 B 的窗格，拔网线再插回自动恢复。

### 阶段 3：体验补齐

- agent 事件与通知走同一条事件流（`events.go` 的 `handleEvent` 已就绪）。
- 文件抽屉 / worktree 通过 `control.go` 的现有动词远程调用。
- 旧的 ssh 中继（`remote.go`）保留为「快速通道」，与远程工作台并存，
  但修掉两个问题：连接码不再内嵌私钥（改为一次性配对 token），
  端口改用随机分配 + 注册表，避免同名设备冲突。

## 不做

- 不传屏幕像素（ScreenCaptureKit + WebRTC 范围外）。
- 不做浏览器客户端。
- 不做 SSE（单向，终端输入没有上行通道）。
- 不重提「自建短配对码 + ssh 中继」当最终形态。

## 第一步

阶段 0 的 `onMain` 修复 + `cmd/gexid` 骨架。这是所有后续工作的地基，
且改动小、可独立验证，不碰现有 UI 路径。
