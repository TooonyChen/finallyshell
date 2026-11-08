# 开发

[English](../en/development.md) · 简体中文

怎样编译、测试和发布 Finallyshell，以及代码是怎么组织的。仓库的规则（先开 issue、分支名、提交信息、pull request）见 [CONTRIBUTING.md](../../CONTRIBUTING.md) 和 [CLAUDE.md](../../CLAUDE.md)。

- [运行](#运行)
- [测试](#测试)
- [提交前的检查](#提交前的检查)
- [截图](#截图)
- [发布](#发布)
- [代码结构](#代码结构)
- [子窗口](#子窗口)
- [选项窗口加一页](#选项窗口加一页)
- [关闭和退出](#关闭和退出)
- [单实例](#单实例)
- [加一条文案](#加一条文案)

## 运行

Finallyshell 需要 Go 1.27.3+。界面用 [MyGo](https://github.com/egoist/mygo) 的原生 UI，终端是 MyGo 的 terminal 插件，底层是 libghostty-vt，运行时加载，所以不需要 cgo。

```sh
go tool mygo dev     # 开发模式，改代码自动重启
go tool mygo build   # 打包到 build/<平台>/
go test ./...
```

第一次 `go run` / `go test` 会把 libghostty-vt 下载到用户缓存目录。`mygo build` 打包的应用自带这个库。

## 测试

界面代码用 `ui.NewTester` 在没有窗口的情况下测试，`tt.Image()` 返回窗口的一帧画面。

用真实服务器跑集成测试（连接、终端、监控、拖拽上传、下载）：

```sh
FINALLYSHELL_TEST_SSH=user:password@127.0.0.1:22 go test ./internal/app -run SSH -v
```

隧道的测试（`internal/sshx` 的 `TestTunnels`、`internal/app` 的 `TestSSHTunnels`）也用这个变量。服务器要在本机（远程隧道的端口从本机去连），并允许 TCP 转发（`AllowTcpForwarding yes`）。不设变量时它们用进程内的测试服务器（`internal/sshtest`）。

## 提交前的检查

CI（`.github/workflows/ci.yml`）对每个 pull request 跑这些检查：

```sh
gofmt -l .           # 必须什么也不输出；go fmt ./... 可以改好
go vet ./...
go tool mygo vet
go test -race ./...
```

`go fmt` 用的是 `go.mod` 里那个工具链的 gofmt；`PATH` 上来自别的 Go 版本的 `gofmt` 对齐注释的方式可能不一样。

想看界面测试画出了什么，把 `SHOT_DIR` 设成一个文件夹，测试会把画面存在那里：

```sh
SHOT_DIR=/tmp/shots go test ./internal/app
```

## 截图

`README.md` 和 `README.zh-CN.md` 里的截图在 `docs/screenshots/`，每种语言一份（`-en.png`、`-zh-CN.png`）。重新生成它们：

```sh
go run ./tools/screenshots
```

它把 `SHOT_DIR` 设成一个临时文件夹，运行 `internal/app` 的测试，再把选中的画面复制到 `docs/screenshots/`。测试只画窗口的内容，所以这个工具给它加上 macOS 风格的标题栏（带窗口的标题）、圆角、细边框和阴影。要哪些画面、它们的标题和裁剪写在 `tools/screenshots/main.go` 的 `shots` 里。主窗口的场景来自 `internal/app/readme_test.go` 的 `TestReadmeScreenshots`；子窗口的场景（连接管理器、连接编辑器、选项、导入、文件编辑器）来自这些窗口原有的测试。

## 发布

把 main 推到 `production` 分支就是发布：

```sh
git push origin origin/main:production
```

`.github/workflows/release.yml` 接着做这些事：

1. 算版本号（`go run ./tools/release version`）：看上一个 `v*` tag 之后的提交类型（Conventional Commits）：
   - 有破坏性改动（`feat!:`、`BREAKING CHANGE:`）升主版本，1.0 之前升次版本；
   - 有 `feat` 升次版本；
   - 只有 `fix`、`perf` 升修订号；
   - 只有 `docs`、`chore`、`test`、`ci`、`build`、`style`、`refactor` 不发布，workflow 到这里结束。

   还没有 tag 时用 `mygo.json` 里的版本（第一次发布是 0.2.0）。已经打过 tag 的提交不会再发布，所以重新运行是安全的。
2. 跑和 PR 一样的检查（`.github/workflows/ci.yml`：gofmt、`go vet`、`go tool mygo vet`、`go test -race`）。
3. 编译三个平台：macOS arm64 在 macOS runner 上（ad hoc 签名、dmg），Linux x86_64 和 Windows x86_64 在 Linux runner 上交叉编译（Windows 安装程序用 apt 装的 NSIS）。版本号只在 runner 里写进 `mygo.json`，main 上的不改。`LICENSE` 和第三方许可证（`go run ./tools/release licenses`）放进 `resources/`，随应用一起打包。
4. 打 tag `v<版本>`，发布 GitHub Release，附上安装包、`SHA256SUMS`、`LICENSE.txt`、`THIRD_PARTY_LICENSES.txt`。说明按提交类型分组（`go run ./tools/release notes`）。

`production` 只应该从 main 快进，在仓库设置里给它加分支保护（禁止强推）。

## 代码结构

Go 里一个文件夹就是一个包（package），包之间不能循环引用，所以按职责拆分。`internal/` 下的包只能被本项目使用。

```
main.go              启动 MyGo、开主窗口，把系统功能（子窗口、对话框、下载目录、语言）交给 app
tray.go              托盘图标、确认用的消息框、退出
keychain.go          系统凭据存储（mygo.Secrets）：Platform 的 SecretGet/Set/Delete
locales/             界面文案：en.json、zh-Hans.json；catalog_gen.go 由 mygo 生成，不要手改
tools/release/       发布用的工具：算下一个版本号、生成 release 说明、收集第三方许可证（notices/ 是以二进制打包的库的许可证）
tools/screenshots/   从 internal/app 的测试重新生成 docs/screenshots/ 里的 README 截图
internal/
  config/            主机和文件夹（hosts.json）、设置的读写，背景图片的副本，配置目录；proxy.go 是代理设置和跳板机链（找循环）；tunnels.go 是隧道设置；keys.go 是退格键和删除键发送什么
  importer/          读其他客户端的连接：finalshell.go 找 FinalShell 的目录、读 conn 里的连接和文件夹、判重、按文件夹导入；testdata 是编造的样例
  sshx/              SSH 连接、认证、known_hosts、给终端用的 Conn；proxy.go 是经过 SOCKS5、HTTP CONNECT 和跳板机连接；forward.go 是端口转发（-L/-R/-D）和 SOCKS5 服务端
  termenc/           终端的连接在服务器的编码（GBK、Big5……）和终端读的 UTF-8 之间转换
  sshtest/           测试用的进程内 SSH 服务器：密码登录、空的 shell、端口转发，StartSFTP 还提供 SFTP（本机文件系统）
  monitor/           服务器状态采集和解析
  remotefs/          SFTP 列目录、上传、下载（带进度和取消）；textfile.go 是编辑器的读取（认出二进制文件）和安全保存
  icons/             内嵌的 SVG 图标（Lucide，ISC 协议）
  fileicons/         VS Code 文件图标主题：读 VSIX 和扩展目录、按名字找图标、字体转换、安装和查找已装的主题；bundled/ 是内置的 Material 和 Seti，languages_gen.go 是 VS Code 的语言表，internal/gen 生成它们
  widgets/           可复用的小控件：图标按钮、用量条、折线图、表单行、左侧导航加页面（nav.go）
  app/               界面和状态
    app.go           App 状态、整体布局、打开/关闭会话
    session.go       一个会话：连接、终端、监控、SFTP；错误信息的翻译
    sidebar.go       左侧：监控面板、竖向会话列表
    sysinfo.go       “系统信息”弹出框：读取、加载和错误状态、复制
    terminal.go      终端区域
    findbar.go       终端上的查找栏，驱动插件的 Search
    fullscreen.go    全屏：只显示终端区域，窗口进入系统全屏
    start.go         新标签页：快速连接、搜索、清空最近连接
    splits.go        分隔条：各区域的最小尺寸，大小的保存和恢复
    files.go         文件面板：目录树、文件表格、拖入上传
    fileicons.go     文件面板的图标主题：后台读取、显示图标、安装和删除
    options_fileicons.go “文件管理 › 文件图标主题”页
    transfers.go     传输队列、“传输”页，和两处共用的传输行 transferRow
    downloads.go     “下载列表”弹出框、下载目录、重试和打开、应用图标上的数目和进度（Platform.ShowTransfers）、选项的“传输”页
    fileeditor.go    编辑远程文件：标签、读取、保存、冲突、关闭前询问、用默认程序打开
    fileeditor_view.go、fileeditor_text.go
                     编辑器窗口的界面（标签、工具栏、提示、状态栏、询问框），编码和换行符、按 #! 认语言
    tunnels.go       会话的隧道：连接后启动、断开时停止，“隧道”页
    manager.go       连接管理器窗口：文件夹树、搜索、拖动、删除和恢复，a.deleteHostForever(h)
    import.go        “从 FinalShell 导入”窗口：查找或选择目录、预览和勾选、导入后在连接管理器里显示
    dialogs.go       密码、主机指纹、覆盖确认
    options.go       选项窗口：页面列表和“常规”页
    keyseq.go        选项的“终端”页（默认编码、退格键和删除键），连接编辑器共用的选项，以及发送它们的按键处理
    mouse.go         “终端 › 鼠标”页，以及这些设置怎样变成终端的选项
    shortcuts.go     快捷键：操作和各平台的默认键、哪些键不能用、冲突、窗口快捷键的分派、终端的 Keybinds
    shortcutspage.go “终端 › 快捷键”页和按键录制框
    keycombo.go      组合键：在 settings.json 里的写法和界面上的显示（macOS 用 ⌘⇧ 符号）
    appearance.go    终端的外观：字体、配色、背景图片怎样变成终端的选项，背景的绘制、复制和删除
    bgimage.go       背景图片的读取：大小限制、缩小、EXIF 方向
    options_font.go、options_colors.go、options_background.go
                     “终端 › 字体”“配色”“背景图片”三页和它们的预览
    editor.go        连接编辑器窗口：a.newConnection()、a.editConnection(h)，校验和保存
    editor_proxy.go  连接编辑器的“代理”页
    editor_tunnels.go 连接编辑器的“隧道”页：表格和添加/编辑对话框
    proxy.go         经过代理和跳板机连接：拨号用的选项、连接前要问的密码、各跳错误的翻译
    secrets.go       记住密码：凭据存储的键、后台读写队列、“记住密码”、密码框、彻底删除和复制连接时的密码
    closing.go       关闭主窗口、退出时做什么：隐藏到托盘、先确认
    windows.go       子窗口：按种类打开/聚焦/关闭，重绘所有窗口
    format.go        大小、速度、延迟的格式化
```

依赖方向是单向的：`app` 用其他所有包，`widgets` 只用 `icons`，其余包既不依赖界面，也互不依赖。

## 子窗口

和 FinalShell 一样，设置这类对话框是独立的窗口（MyGo 的子窗口，`WindowOptions.Parent`），不是主窗口里的浮层。目前有选项窗口、连接管理器、连接编辑器、“从 FinalShell 导入”窗口和文件编辑器，都是主窗口的子窗口，例外是从连接管理器打开的连接编辑器和导入窗口：它们是连接管理器的模态子窗口，和 FinalShell 一样。这样做的结果：

- macOS 上编辑器是连接管理器的 sheet，盖在连接管理器上，不挡主窗口。sheet 没有标题栏，所以模态窗口在 macOS 上把标题（“编辑连接”或“新建连接”）显示在窗口顶部（`windows.go` 的 `sheets`）。连接管理器默认 900×640，比编辑器大，sheet 放得下。
- Windows 上只禁用连接管理器，主窗口照常可用。
- Linux（GTK）上模态窗口会挡住本程序所有窗口，编辑器开着时主窗口的终端也不能输入。FinalShell 自己的对话框（Swing 默认的应用级模态）也是这样，编辑一个连接的时间很短，所以接受。
- 从主窗口打开的编辑器（新标签页上的“新建连接”）仍然是主窗口的普通子窗口，不管连接管理器开没开，免得在 macOS 上变成另一个窗口（可能在后面）的 sheet。

子窗口是这样工作的：

- 每种窗口（`WindowKind`）同时只开一个：`a.openWindow(spec, view)` 在已经打开时只把它提到前面。`WindowSpec` 写标题（locales 里的函数，切换语言时重新取）、内容区大小、最小尺寸、父窗口（默认主窗口）和是否模态。
- 窗口里按 Esc（macOS 上还有 Cmd+W）或调用 `a.closeWindow(kind)` 关闭。关闭父窗口会一起关闭它的子窗口；关闭主窗口会关闭所有子窗口。`WindowSpec.OwnKeys` 把 Esc 和 Cmd+W 留给窗口自己（文件编辑器用 Cmd+W 关闭标签）。`CloseRequested` 在用户点关闭按钮时先问窗口，返回 false 就不关（编辑器有没保存的修改时先问），程序自己关窗口不问。`Closed` 在窗口关掉后调用。标题变了（编辑器换了标签）调用 `Platform.RetitleWindow`。
- 所有窗口共用一个 `App`，都在 UI 线程上。一个窗口只会在自己的输入之后重绘。改了别的窗口也显示的状态（保存的主机、设置）之后调用 `a.redraw()`，重绘所有窗口。`App.post` 投递的工作做完也会重绘所有窗口。
- 测试里 `fakePlatform` 记下打开的窗口，`p.show(t, kind)` 用一个单独的 `ui.Tester` 画出它的内容。

各平台的行为（读 MyGo v0.8.1 后端代码确认的，还没在真机上逐一试过）：

| | 普通子窗口 | 模态（`Modal`） |
|---|---|---|
| macOS | 普通 `NSWindow`，总在父窗口上面，父窗口最小化时跟着隐藏 | 父窗口的 sheet（从标题栏下方滑出，没有自己的标题栏，位置由系统决定）；父窗口本身是子窗口时也一样 |
| Windows | 父窗口拥有的窗口（owner），总在父窗口上面，没有自己的任务栏按钮 | 打开期间禁用父窗口（`EnableWindow`），只禁用直接的父窗口 |
| Linux | GTK `transient_for` 父窗口 | `gtk_window_set_modal`：GTK 会挡住本程序其他所有窗口的输入，不只是父窗口。Wayland 下窗口位置由合成器决定 |

子窗口打开时居中在父窗口上方，不能最小化。

## 选项窗口加一页

页面都列在 `internal/app/options.go` 的 `optionPages` 里，按树的顺序，父页面在子页面前面。`view` 为 `nil` 的页面显示“这些选项将在以后的版本中提供。”

给一页加内容：在新文件里写 `func (a *App) fontPage(c *ui.Context)`，把那一项的 `view: nil` 改成 `view: (*App).fontPage`。新页面就加一项 `{id: ..., parent: ..., title: locales.Xxx, view: ...}`。每项占几行，几个人同时改不同的页面不会改到同一行。

页面里改设置用 `a.changeSettings(func() { ... })`，会保存并重绘所有窗口。

## 关闭和退出

| | 点关闭按钮 | 退出（托盘菜单、Cmd+Q、SIGINT） |
|---|---|---|
| 开了托盘（且系统有托盘） | 隐藏到托盘，不问 | 有会话打开且开了确认：先问 |
| 没开托盘 | 有会话打开且开了确认：先问，确定后退出 | 同左 |

“有会话打开”指已连接或正在连接。逻辑在 `closing.go`（`CloseRequested`、`QuitRequested`，用假的 Platform 测试），`main.go` 在主窗口的 `OnClose` 和 `App.OnBeforeQuit` 里调用它们。确认框是 `mygo.Dialog.Message`，在 goroutine 里等用户回答，答“退出”后再调 `mygo.App.Quit()`。

各平台的行为是读 MyGo v0.8.1 代码确认的，没有在真机上试过：

- macOS：托盘图标在菜单栏，是模板图（随菜单栏变色），点击弹出菜单。Dock 图标一直在，点它（`OnActivate`）显示隐藏的窗口。确认框是主窗口的 sheet（窗口隐藏时是独立的框）。
- Windows：左键单击图标显示窗口，右键弹出菜单，模板图在深色任务栏上画成白色。
- Linux：用 AppIndicator，需要 `libayatana-appindicator3`。没有时 `NewTray` 报错，窗口不隐藏。GNOME 默认不显示 AppIndicator 图标（要装扩展），这时图标创建成功但看不见。再启动一次程序就能把窗口找回来（见下）。

## 单实例

程序同时只运行一个（`mygo.App.RequestSingleInstanceLock`）。再次启动时，新进程把命令行交给已经在运行的那个后退出，运行中的程序把主窗口显示出来（在托盘里就从托盘恢复，最小化了就还原）并放到最前面。这样两个进程也不会同时写 `hosts.json`、`settings.json`。

- 锁按应用名区分（打包后加上 identifier），所以 `mygo dev` 的开发版和安装版互不影响。
- 程序退出时释放锁，崩溃留下的锁文件下次启动会清掉。
- Wayland 下靠新进程带来的激活令牌获得焦点。Windows 下新进程把前台权限让给运行中的程序。macOS 从 Finder 或 Dock 打开时系统本来就只开一个；点 Dock 图标走 `OnActivate`，同样会把托盘里的窗口显示出来。

## 加一条文案

1. 在 `locales/en.json` 里加 key 和英文（带参数的写在 `@key.placeholders` 里），在 `locales/zh-Hans.json` 里加中文。
2. 运行 `go tool mygo generate`（`mygo dev` 会自动做），代码里用 `locales.Key(...)`。提交重新生成的 `locales/catalog_gen.go`。

格式是 ICU MessageFormat，见 MyGo 的 [i18n 文档](https://github.com/egoist/mygo/blob/main/docs/i18n.md)。
