# Changelog

## 0.0.81

远程窗格恢复成和本地一样：同样的字号、铺满卡片、拖窗口就重排，只是内容来自另一台 Mac。被远程那台的窗格按同一个网格显示，卡片更大时居中，不再挤在左上角。

## 0.0.79

回滚输入预测：预显的字符和真实显示对不齐，反而更乱，先回到 0.0.76 的显示行为。远程推送和节流保留。

## 0.0.76

远程画面的推送按屏幕刷新率节流，积压的几帧合成一次写入：远程窗口不再为每一小段输出各重绘一次，整屏刷新时更顺。

## 0.0.75

远程画面不再定时去问：对方安静时不占用它的主线程，有输出才推过来。之前按键要排在每秒几十次的画面读取后面，现在直接处理，输入明显跟手。

## 0.0.74

远程画面改成一条常驻连接推送：对方一有输出就推过来，不再靠这台机器反复问。按键不再排在轮询后面，画面更新也少了一次往返；对方是旧版本时自动退回原来的轮询方式。

## 0.0.73

远程窗口不再空闲时也每秒 60 次拉取画面：有输出时全速，安静下来降到每秒 8 次，一按键立刻恢复全速。慢网络下按键回显不再和空闲轮询抢带宽。

## 0.0.72

- 远端输入合并发送：一次发送期间敲下的按键会攒成一个请求，打字不再按每个键各等一个往返；外网下输入延迟明显下降。

## 0.0.71

- 修复远程不断重连：内置中继的令牌让每台 Mac 都去认领同一条隧道，两台机器每秒互相把对方踢下线。现在只有指定主机名的那台 Mac 认领隧道，其他 Mac 只把中继当地址用。

## 0.0.70

- 修复远程窗格内容重复、输入紊乱：撤掉 0.0.65 引入的长轮询，恢复按帧拉取。长轮询会把请求发出时的旧网格挂在服务端，窗口缩放时回来的画面还是旧尺寸，于是重复渲染。
- 「连接另一台 Mac」的弹窗只留一个配对码输入框，标题下不再堆说明。

## 0.0.69

- 连接另一台 Mac 时自动选路：配对会记下对方的局域网地址，之后优先走局域网；局域网不通（不在同一个网络、离开家里）自动退回中继，恢复后再切回来。

## 0.0.68

- 修复内置中继下打开手机网页提示「remote access is off」：配对接口现在把中继也算作已开启，不再只看局域网开关。

## 0.0.67

- 内置中继的机器，远程页只剩两样：访问地址和 6 位配对码；「允许被访问」开关和中继设置都不再出现。
- 地址优先显示中继地址，本机端口被占用也不影响它显示。

## 0.0.66

- 远程设置只留配对码：这台 Mac 显示 6 位码和访问地址，另一台设备输入这 6 位就能连。中继直接编译进程序，设置里不出现任何中继配置。
- 有中继时，「连接另一台 Mac」不再问名称和地址，只问配对码。

## 0.0.65

- 远程窗格改成长轮询：程序有输出时立即返回，空闲时把请求挂起 25 秒再答。空闲窗格的请求从每秒 62 次降到每 25 秒 1 次，敲键盘后约 10ms 就能看到回显，局域网和外网都一样受益。
- 设置 › 权限页去掉与页面标题重复的「权限」分组标题。

## 0.0.64

- 修复 0.0.63 一打开就卡死：自建中继的初始化在启动时落到主线程自己等自己，窗口建好后不再响应，控制接口也没起来。现在默认配置下能正常启动，开启中继的机器同样不受影响。

## 0.0.63

- 远程访问新增「自建中继」：在自己的服务器上跑 gexi-relay（提供 Docker 镜像），Mac 主动外连并校验服务器证书指纹，手机或另一台 Mac 用 https 地址即可连入——不需要公网 IP，也不用配路由器端口映射；中继只转发给认领该隧道的那台 Mac，不会被当成开放代理。
- 远程地址支持 `https://`；配对码改为 10 分钟有效，过期后需要重新生成，猜码限流除了按单个地址，还增加了全服上限。
- 设置新增「权限」页：屏幕录制、完全磁盘访问、辅助功能的状态一目了然，可以直接请求系统授权，或打开系统设置并把 gexi 拖进列表；在 gexi 里运行的终端和 agent 会继承这些授权。

## 0.0.62

- 抽屉的「改动」页新增「未提交 / 分支」切换：分支视图显示这条分支自 base 分叉以来的全部改动，包含已提交、未提交和未跟踪的文件，右侧标出「相对 main」；提交或推送之后仍然能看，文件 diff 的旧侧取合并基点。
- 合并 PR 遇到冲突时，Amp 和 Pi 窗格也能让各自的 agent 自动解决：gexi 先把 base 合进来，由 agent 消除冲突标记，再由 gexi 暂存、提交、推送并完成合并；失败时会中止合并，checkout 回到原样。

## 0.0.61

- 离开或归档工作树后，项目自己的 checkout 会切回这棵工作树的 base 分支（通常是 main）；checkout 里如果有你自己未提交的改动，则保持原样。
- 所有弹窗换成 Liquid Glass 外观：离开/归档确认、创建工作树、提交信息、重命名、连接另一台 Mac 现在共用一套玻璃面板、按钮和输入框，Enter/Escape 的行为一致；归档确认不再弹系统对话框。
- 更新窗口左侧的 app 图标去掉米色底，透明背景和窗口融为一体，不再出现一块白方框。

## 0.0.60

- 工作树流程闭环：W 现在先填分支名、选择是否搬走未提交改动，多窗格共用项目目录时会提示；离开/归档会先停掉 checkout 里的 agent，归档前确认，自动归档遇到未提交或 ignored 文件会跳过。
- 未合并提交的分支会保留在工作树页，可按项目分组查看、恢复或批量清理；关闭窗格或归档时会保存 agent 的恢复命令，后台运行的窗格也会在重启后回来。PR 的提交信息先弹窗确认，git stash 失败时会给出恢复命令。
- 侧栏改成带分组标签和会话卡片的新样式，并支持右键重命名/关闭会话和项目；手动设置的会话名称会持久保存。
- 修复一个会话有多个 pane 时，一个 pane 结束就让左侧 loading 提前停止：只要还有 pane 在工作，session 会继续显示 loading；Amp 切到已运行的线程或 hook 漏报时也会正确恢复状态。

## 0.0.59

- 更新窗口（检查更新、下载、更新说明）左侧的 app 图标换成新的 panes 标：之前只有 Dock 和 Finder 用新图标，更新窗口读到的还是旧的回退图标。

## 0.0.58

- 侧栏和标题栏里 agent 名字前重复的小图标去掉了：品牌标旁边不再出现 Claude Code 的 `✳`、Amp 的盲文转圈符等终端标题自带的符号；Codex、OpenCode 等其他 agent 也一并处理，远程窗格和远程会话同样生效。
- 修复远程窗格不全屏时最下面的输入框偶尔看不见：观看端现在按宿主的实际网格计算字高，请求的行数不会再超出卡片；宿主屏幕仍比卡片高时，镜像会贴住最下面有文字的一行显示，而不是从顶部画起把输入行裁掉。

## 0.0.57

- 修复远程观看时被看的那台 Mac 的窗格狂闪：以前观看端每 16ms 报一次卡片网格，宿主每次都把窗格终端改成那个尺寸，本地视图又按自己的布局改回去，终端每秒收到几十次尺寸变化；Amp、Claude Code 这类全屏程序每次都整屏重绘，看起来就是狂闪。现在同一个网格只应用一次，之后尊重本地布局落定的尺寸。

## 0.0.56

- 任务完成时，完成任务的窗格会升起一道 Spark 光环：紫到金的四层环带从终端底部扫过，带几像素的镜头弯折、极轻的色散和底/侧玻璃边光。标题栏和侧栏不参与。
- 光环只在任务真正结束时出现：还有后台 subagent 在跑就等最后一个结束；同一窗格再次完成会重开；开始新回合或停在等待输入时不会放。
- 打开「减少动态」后只保留 600ms 的原地半强度淡入淡出，不做弯折、色散和边光；静止时没有任何常驻开销。
- Amp 可以作为窗格里的 agent：自动识别 `amp`，恢复最近的线程；Amp 插件通过控制 socket 报告回合、等待输入和会话事件，侧栏标记和通知与其他 agent 一致。
- Amp 的批准等待会显示成「等待输入」；工作树的提交信息也可以由 `amp -x` 生成。

## 0.0.55

- 侧栏默认和画布外框同色，窗格卡片落在同一层底色里，整体像被一圈边框包住；浅色、深色都不再整窗铺液态玻璃。
- 液态玻璃收为侧栏的可选项，默认关闭。打开后侧栏才透出桌面的系统材质；原来的「液态玻璃」设置会重置为关闭。
- 窗格和标题栏恢复主题自己的实色背景，终端文字不再随桌面壁纸变淡或发灰。

## 0.0.54

- 远程窗格恢复本机字号：0.0.53 为了让整屏塞进卡片把对方的画面整体缩小，字明显比自己的会话小。现在把本机窗口能放下的行列数告诉那台 Mac，它把该窗格重排到这个大小——字号和本机一致，输入到卡片边缘就换行。停止观看几秒后，那台 Mac 的窗格恢复它自己的宽度。两台 Mac 都需要升级。

## 0.0.53

- 另一台 Mac 的侧栏按它那边的项目分组：会话放在各自项目下，项目行的 + 会在那台 Mac 的该项目里新建会话，不再落到它当前选中的项目——之前可能落在 home，接着按 W 也建不了 worktree。
- 在另一台 Mac 上关闭带 agent 的会话不再卡住：以前那台 Mac 会弹确认框，而这台看不到；现在远端关闭直接生效，卡片同时淡出。
- 远程窗格按卡片等比缩放显示，整行都在可见范围内：输入到卡片边缘就换行，不再等到主机终端的宽度才换行，也不再看不见右边一截。

## 0.0.52

- 修复远程窗格一片空白、看不到输入行的问题：卡片比那台 Mac 的窗口小时，原来固定显示屏幕最底部，而 shell 的提示符写在空屏幕的第一行，刚打开的会话就被整个裁掉（输入其实已经生效）。现在按那台机器上最下面一行有内容的行定位——shell 的提示符、全屏程序的输入框都在——滚动、清屏、进出全屏程序都能跟上。

## 0.0.51

- 修复工作树卡在「PR」却无内容可提的问题：合并之后状态被重置、分支上又没有新提交时，按 PR 只会报错，「归档」也不再出现。现在选中会话或启动 gexi 时，如果一个工作树是干净的、且没有比 main 多出来的提交，会查回它已经合并的 PR，「归档」自动恢复；有内容可提的工作树不受影响。

## 0.0.50

- 另一台 Mac 的窗格不再跟着卡片缩放了：文字大小和本机窗格完全一致。卡片比那台机器上的窗口小时，保留屏幕底部（终端的输入行在那里），上方裁掉；想看全屏按 ⇧⌘↩ 放大这个窗格。
- 另一台 Mac 的窗格标题栏补齐了 checkout 控件：W 创建 worktree，以及 PR、合并、归档，和本机一样；合并按钮的颜色按那台 Mac 上的判断来。
- 芯片右侧菜单里的「离开 checkout」在远程窗格也能用：有未提交改动或未合并提交时，先在本机弹确认，再让那台 Mac 执行。

## 0.0.49

- 侧栏里那台配对的 Mac 也可以新建会话了：项目行上的 + 和 ⌘T 会请那台 Mac 在它当前的项目里开一个，开好后这边直接选中。
- 另一台 Mac 的窗格按它那边屏幕的原始大小排版再缩放进卡片里，不再跟着本地窗口拉伸，那边终端最底下的输入行也能看到了。
- 设置 › 工作树 多了「合并后自动归档」：打开后，拉取请求一合并就直接清掉工作树和窗格，不用再点一次「归档」；关着的时候照旧由你决定。
- 合并、归档这些等待中的工作树按钮，文字本身会像呼吸一样闪动，旁边不再另起一个转圈；侧栏里工作中会话的标记也改成了同样的文字脉动。

## 0.0.48

- 另一台 Mac 现在是侧栏里的一个项目：它的会话和窗格跟本机的并排排列，窗格标题、agent 标记、分支标签（含增删行数）和分屏比例都和那台机器上一样，不再只是单独的一屏远程视图。
- 远程窗格标题栏上的分屏、关闭窗格、关闭会话按钮真的作用在那台 Mac 上；那台机器上改了布局，这边下一轮刷新就跟着变。
- 切换会话或关闭窗格时，两边自动对齐到同焦点：哪边的窗格有键盘，另一边也一样。
- gexi 不再把远程会话写进本机状态，重启后不会出现一个打不开的远程窗口，配对还在，侧栏照常按配对恢复。
- 窗格标题栏里的 checkout 名称改成独立的小标签：gexi 工作树用强调色，和 agent 名字区分开；合并了同名的分支时标签不重复出现。
- 手机浏览器那版同步：窗格标签会跟着那台 Mac 的布局变，底下多了分屏 →、分屏 ↓、关闭窗格三个按钮，终端字号和行距也调成和 app 里一样。

## 0.0.47

- 另一台 Mac 上的会话现在是真正的终端镜像：颜色、光标、全屏程序都和那台机器上一样，按键直接打进去，不再是一秒刷一次的纯文本，也不用再点底部的 esc、方向键那几个按钮。

## 0.0.46

- 配对过的 Mac 出现在侧栏项目下方，带着它自己的会话；点开其中一个，终端直接显示在主窗口里，和本地项目并排，不用再另开窗口。
- 侧栏底部的远程菜单换成显示器图标，设置里的远程标签也跟着换了。

## 0.0.45

- 远程访问回来了，这次只在局域网里：设置 › 远程 打开"允许这台 Mac 被远程控制"，界面显示本机地址和六位配对码；同一 Wi-Fi 下的其他 Mac 或手机输入配对码后，可以查看会话并在窗格里输入。不需要中继、ssh 或公网端口。
- 手机用浏览器打开这个地址就能用：选择会话、查看窗格输出、输入命令，和另一台 Mac 上的 gexi 客户端一样。
- 侧栏底部新增远程菜单，直接打开已添加的其他 Mac；设置里可以按地址和配对码添加、移除 Mac，配对过的设备也能单独移除，配对码可重新生成。
- 窗格缩放改为跟着会话走：每个会话记住自己放大的窗格，切到别的会话再切回来还是原来那个，重启 gexi 后也会恢复。
- 退出工作树时转圈居中在整个工作树按钮上，不再挤在右侧箭头的窄条里。

## 0.0.44

- 窗格标题栏只显示 checkout 的名字：gexi 的工作树用它起的短名，其他项目用文件夹名，完整路径改为鼠标悬停时显示。
- 文件夹名和分支名相同时不再重复显示：分支标签同时代表两者，悬停分支标签能看到完整路径。

## 0.0.43

- 新增主题：设置里按浅色、深色分别挑选 Ghostty 主题，内置主题和本机 Ghostty 主题文件夹里的都在列表里，可以搜索。
- 主题不只改终端配色：侧栏、窗格卡片和窗口框架的底色调都从主题的背景、文字和光标颜色推导，换主题时整个窗口一起变。
- 合并工作树时标题栏逐步显示进度：生成提交信息、推送分支、创建 PR、读取 PR 状态、解决冲突、在 GitHub 上合并、更新本地分支，中英文都有对应文案。
- 移除远程访问：不再有远程 Mac 连接、中继隧道、ssh 配置和远程设置，侧栏的远程菜单一并去掉。

## 0.0.42

- 支持 Pi：侧栏显示 Pi 的标记，恢复会话时用 `--continue` 接着上一个会话。
- Pi 没有钩子配置文件，gexi 改为传入一个小扩展，把开始、结束、等待输入这些状态直接报给侧栏。
- 侧栏里正在工作的 agent 改用转圈加载指示，替代原来的流光效果；关闭按钮的位置始终保留，指示器不会左右跳。
- 窗格卡片、悬停行、选中行和搜索框不再叠一层系统玻璃，改用主题自己的颜色轻扫一层：浅色下不糊成灰，深色下不泛白。
- 窗口玻璃的主题底色变淡，桌面的颜色能透过来，玻璃的镜面感和边缘不被一层不透明底盖住。

## 0.0.41

- 窗口没有焦点时直接画主题自己的底色，不再用系统玻璃：切到别的应用或打开设置窗口，界面不会糊成一层灰雾，切回来玻璃照常。
- 设置窗口不再自己铺一层玻璃，打开时不会先闪一下桌面背景。

## 0.0.40

- 只有当前正在操作的窗口绘制玻璃：打开的设置窗口被系统磨砂后不再发灰，而是直接使用主题本身的底色和卡片颜色，切回主窗口时玻璃照常。

## 0.0.39

- 设置窗口改用主窗口同一层玻璃，设置页直接铺在这层材质上，不再是一块纯色填充。
- 紧凑窗格也落在同一层玻璃上：画布按窗格卡片的底色着色，窗格标题栏不再自己刷一层背景。

## 0.0.38

- 窗格卡片的投影调成一层很淡的分界：浅色 alpha 0.07、深色 0.16，不再是一圈深灰。
- 文件树里的文件按类型显示图标：代码、文本、结构化配置、图片、锁文件、shell 各有标记，其余用普通文件图标。

## 0.0.37

- 窗格卡片加了一层柔和的投影，分屏时像侧栏与画布之间那样有立体感。
- 窗格之间的间距和画布边缘的留白各放大一倍（3 → 6）。

## 0.0.36

- 文件抽屉改回主题背景的不透明面板：浅色下纯白、深色下随主题，左侧保留 1px 分隔线。之前的玻璃在浅色下会发灰。

## 0.0.35

- PR 合并后自动归档 worktree：关闭它的窗格、删掉 checkout（分支保留，可在设置里恢复），在项目根目录新开一个 shell；主 checkout 干净时同时切回合并的目标分支（默认 main）。

## 0.0.34

- 文件抽屉的玻璃改为跟随主题背景着色，不再是一层系统玻璃的灰色：浅色下更接近白玻璃，深色下随主题变深。

## 0.0.33

- Perch 窗格标题栏补上它自己的标记（绿色 P 形鸟标），与 Claude Code 等 agent 一样显示品牌图标。

## 0.0.32

- 窗格标题栏的徽章（W / PR / Merge、分支、subagent）改成圆角矩形，和界面其它控件的圆角一致。
- 图标按钮（分屏、放大、关闭、加号、设置、抽屉返回）的悬浮背景从圆形改成 6pt 圆角矩形。

## 0.0.31

- 窗格标题栏的徽章（W / PR / Merge、分支、subagent）改成直角，和旁边的方形图标统一，不再用圆角。

## 0.0.30

- 窗格里可以跑 Perch 了：它作为 `node .../bin/perch` 启动，进程名和 argv[0] 都是 node，现在按脚本路径认出来，只认 `bin/perch` 和 `perchai-cli`，不会把别的 node 程序当成 agent。
- Perch 按目录保存会话（`cli-cwd-<sha256(目录)>[:16]`），所以重启后直接跑 `perch` 就接上原来的对话，不用带 session id。
- W 键建 worktree 对 Perch 和 Claude Code 一样可用：提交信息、冲突解决走 `perch run --json`，用 gexi 自己的 thread，不打断窗格里的对话。
- 分支改名提示写进 `.perch/rules/gexi-worktree.md`，并加入 `.git/info/exclude`，不会出现在改动里。Perch 没有 hook，规则文件是唯一能送到模型的通道。
- Perch 窗格的忙碌状态从 thread 文件的 `inFlightTurn` 读取，进程已退出的不算忙。侧栏状态灯因此能亮能灭。

## 0.0.29

- 液态玻璃下，文件抽屉的左缘不再叠 1px 边框，分隔交给玻璃自身偏暗的 rim，和窗格卡片一致。
- 没有会话时的工作区卡片改用和窗格相同的液态玻璃，不再是白底加边框。

## 0.0.28

- 窗口改用 macOS 26 的 Liquid Glass：侧栏、画布和窗格卡片共用同一块系统玻璃，窗格卡片的边缘由玻璃自身的受光边承担，不再画 1px 边框。
- 标题栏的 W、PR、分支、subagent 徽章和分屏、缩放、关闭按钮统一成浅灰胶囊，悬停时不再改变布局。
- 文件抽屉改用着色器玻璃：终端内容在抽屉下面被模糊、折射。
- 设置 → 通用 → 界面新增「液态玻璃」开关，关闭后恢复原来的平面样式，无需重启。
- mygo 升级到 v0.8.1，窗口玻璃可以直接染色（VibrancyTint）。
- 侧栏会话行左右各加 2pt 边距，行距从 1pt 加到 3pt。
- PR 的提交信息生成超时从 90 秒放宽到 4 分钟，送给模型的 diff 上限减半，慢模型不再回落到手动输入。

## 0.0.27

- 放大窗格后的台前轨道改为悬浮卡片：透视倾斜、投影，卡片内是终端的实时镜像，图标落在卡片左下角；窗格多到放不下时互相叠放，切换时新窗格放大上台、原窗格缩回卡片。
- 合并冲突时，gexi 先把 agent 解好的文件加入暂存区再检查，不再误报「left 1 file(s) in conflict」而放弃合并。

## 0.0.26

- 运行中的 subagent 数量从侧栏移到窗格标题栏、分支徽章右侧：工作绿圆点 + 数量；回合结束后仍在跑时圆点转琥珀色，悬停显示类型和后台状态。

## 0.0.25

- 放大窗格后，被挡住的窗格在右侧排成台前调度式的卡片：窗格名、目录，以及终端最后几行快照；点哪张卡片，哪个窗格上台。
- 合并 PR 遇到冲突时，gexi 让窗格里的 agent 先解决冲突，提交并推送后再合并。

## 0.0.24

- 侧栏会话行显示运行中的 subagent 数量，跟着会话名的文字流光一起扫过；后台 subagent 在回合结束后仍保持工作状态，直到它自己的结束事件到达。
- 分屏时失焦的窗格重新变淡，指针悬停的窗格恢复全强度。
- gexi 自己创建的工作树在窗格标题栏只显示工作树名字（如「weaver」），用系统强调色浅底标出，悬停看完整路径。
- PR 的提交信息改由窗格里的 agent 以无头模式生成，移除 DeepSeek 配置与密钥。

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
