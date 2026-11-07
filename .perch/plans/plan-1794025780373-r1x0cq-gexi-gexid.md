## 结论先说

你的 `remote.go` 已经写完了，但它是 **ssh 登录**，不是远程工作台。远端跑 `claude` 时本地只看见 ssh 进程，agent 状态、文件抽屉、worktree、通知全断。连接码里还明文塞着私钥。

你要的形态（云服务器只转发、gexid 留在自己 Mac 上主动出站）是对的，而且代码里三个接缝都已经就位，不用重写。

## 三个接缝（已核实）

1. **`pane.go:241`** — `terminal.Options.Conn` 是 `io.ReadWriteCloser`，还支持可选的 `Resize(cols, rows)`。`connOf(p)` 现在返回具体类型 `*ptyConn`，改成接口就能塞网络流。远程窗格的渲染、拆分、缩放、选中与本地完全一致。
2. **`control.go` 的 `dispatch`** — 已经是完整工作台 API（`project.new` / `session.new` / `pane.split` / `pane.text` / `pane.send` / `event`）。远程协议直接复用，不新造动词。
3. **headless 可行性** — `terminal.New` 只调 `Load()`，不依赖 `mygo.App`，无 view 时半秒后按 80×24 启动。`invalidate()` 已有 `win != nil` 保护。**`onMain()` 没有**，headless 会死锁，这是第一件要修的事。

## 分阶段

**阶段 0 · headless 地基**
`onMain` 加 `w.win == nil` 同步分支；新增 `cmd/gexid`，`newWorkspace()` + `startControl()` 的 headless 版，先监听 TCP、只做 `ping` / `workspace.info` / `pane.list`。
验收：`gexid` 起来，CLI 能连上读到 pane 列表。

**阶段 1 · 远程窗格协议**
`panes.list` / `pane.attach` / `pane.resize` / `pane.detach` / `event`，长度前缀帧多路复用，每窗格一条字节流。断线按 pane id 重挂，远端 shell 不随连接退出，不需要 tmux。
验收：本地开远程窗格跑 `htop`，缩放布局正确。

**阶段 2 · 出站 + 云转发**
gexid 主动出站，设备 token 认证，指数退避重连。服务器只做配对、校验、密文转发，不解析内容。客户端与 gexid 之间端到端 TLS，服务器看不到明文。
验收：两台 Mac 不同网络，拔网线再插回自动恢复。

**阶段 3 · 体验补齐**
agent 事件与通知走同一条事件流（`events.go` 的 `handleEvent` 已就绪）；文件抽屉和 worktree 通过现有 control 动词远程调用；旧 ssh 中继保留为快速通道，但修掉私钥内嵌和端口冲突。

## 不做

不传屏幕像素，不做浏览器客户端，不做 SSE，不把 ssh 中继当最终形态。

## 建议从哪开始

阶段 0。改动小、可独立验证、不碰现有 UI 路径，而且是后面所有工作的地基。修完 `onMain` 就能立刻验证 headless workspace 跑不跑得起来——这是整个架构唯一还没被证实的假设。

完整计划存在 `.perch/plans/remote-workbench.md`。