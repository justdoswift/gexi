# gexi — logo brief

**What it is.** A macOS terminal workspace: projects on the left, sessions in
panes, a CLI that drives it. Built with Go and mygo.

**Audience.** Developers who live in a terminal and prefer a calm, friendly
tool to a loud one.

**Adjectives.** Friendly, calm, witty, precise.

**Constraints.**
- A snake, cute rather than realistic (user's direction).
- No terminal clichés: no prompt caret, no split-pane rectangle, no `>_`.
- Must work at 16 px, in one colour, and as a macOS 26 app icon.
- Wordmark "gexi" lowercase; the mark must stand alone.

**Category clichés to avoid.** Carets and `>_`, split rectangles, neon on
carbon, command bars, brackets.

**Word map.** gexi → gecko/snake → coil, glide, tongue, scales · workspace →
projects, sessions, panes, calm · friendly → round, big eyes, blush.
Intersection: a coiled snake that is settled, not threatening.

## Concepts (longlist, one line each)

1. **Coiled G** — the snake's body draws an uppercase G, the head resting on
   the spur. (letterform)
2. **Snake badge** — a round, wide-eyed snake head, sticker-cute. (mascot)
3. **Nest** — the snake curls inside a rounded square (the window) and peeks
   over its top edge. (combination)
4. **S-nek** — the body's S becomes an endless loop; sessions without end.
   (abstract)
5. **Sleeping coil** — a closed-eye spiral: a calm workspace. (mascot)
6. **Hood G** — a cobra hood whose silhouette reads as G. (pictorial)
7. **Three rings** — a coil of three loops for projects, head on top.
   (abstract)
8. **Hiss** — a single continuous calligraphic stroke from tail to tongue.
   (letterform)

Picked three, most different: **Coiled G**, **Snake badge**, **Nest**.

## 选定（2026-11-06）

蛇的约束作废（用户：「不一定非要蛇，从产品角度出发」）。改从产品动作推导，
方向记录在 `icon-product/README.md`，三个候选：窗格 / 分叉 / 嵌套。

用户选定 **A · 窗格**：一块窗格按真实布局切成三块，右下角是正在跑的那个会话。

- 墨黑 `#121212` 底、纸白 `#F8F6F5` 窗格、紫 `#AC46EA` 只给聚焦窗格。
- 矢量与 8 档 PNG：`icon-product/final/`；对比图：`icon-product/preview.png`。
- `AppIcon.icon` 的 `01-mark.svg` / `02-back.svg` / `icon.json` 已按本方案重建。
- 旧的像素蛇方案（dinkie）保留在 `renders/`、`concepts/` 里，不再使用。

## 选定（2026-11-04，已作废）

用户在 icon.qiaomu.ai 上选定 **dinkie-icons:snake**（12×12 像素蛇）＋ 石墨线性渐变底
（#000000 → #64748b，135°）＋ 白色单体标记，缩放 0.6，圆角 22。

- 矢量：`concepts/e-dinkie.svg`（底 + 颗粒 + 标记，1024）
- 1024 预览：`renders/e-dinkie.png`
- 导出记录：`icon-dinkie/`（配置与 1024/favicon；完整平台包见用户 Downloads 的 My-App-icons.zip）

像素蛇方案保留了"盘踞、安静"的读法，同时比写实蛇更抽象、不吓人。

## 应用图标（2026-11-04）

`AppIcon.icon`（macOS 26 分层格式）已按本方案重建：`02-back.svg` 满幅石墨渐变 + 颗粒，
`01-mark.svg` 白色像素蛇（60%，居中），`fill` 改为深色底。旧的三层（Body/Head/Face）
已删除。`resources/icon.png`、`resources/icon.svg` 未被 mygo 引用，原样保留。

## 构建

`go tool mygo build` 会把 `AppIcon.icon` 编成最高 256 px 的 `AppIcon.icns`。
要全尺寸就用 `scripts/build.sh`（或对已构建的包跑 `scripts/fix-app-icon.sh
<app>`）：它从 `../icon-product/final/png/gexi-icon-1024.png` 重新生成 16–1024 的 icns、替换并重签名。
源图换了，这一步跟着换。dmg 仍是 mygo 打的 256 px 版本。

## 签名与公证

`mygo.json` 的 `macos` 已配置 Developer ID
（`Developer ID Application: Peng Ge (6SUWJ2JVNM)`）和公证 profile
`justwrite-notary`（与 JustWrite 同一套凭据）。`go tool mygo build` 会签名 app、
签 dmg、提交公证并 staple。

`scripts/fix-app-icon.sh` 遇到 Developer ID 签名的包会跳过——替换 icns 会让签名和
公证票据失效；需要时用 `FORCE_ICNS_PATCH=1` 强制替换并重签（会失去公证）。

正式构建的 dmg 已公证并 staple（Gatekeeper 显示 Notarized Developer ID）。
这类包里 AppIcon.icns 仍是 mygo/actool 的 256 px，完整 1024 走 Assets.car；
要连 icns 一起换成全尺寸，只能重签并重新公证（或用 FORCE_ICNS_PATCH=1
本地临时替换，未公证）。
