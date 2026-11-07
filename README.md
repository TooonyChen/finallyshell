# Finalshelly

一个仿 FinalShell 布局的 SSH 客户端，纯 Go 编写。界面用 [MyGo](https://github.com/egoist/mygo) 的原生 UI，终端是 MyGo 的 terminal 插件（底层 libghostty-vt，运行时加载，不需要 cgo）。

和 FinalShell 的区别：会话标签是竖向的，放在左下角，可以滚动、拖动排序。

![screenshot](docs/screenshot.png)

## 运行

需要 Go 1.27.3+。

```sh
go tool mygo dev     # 开发模式，改代码自动重启
go tool mygo build   # 打包到 build/<平台>/
go test ./...
```

第一次 `go run` / `go test` 会把 libghostty-vt 下载到用户缓存目录；`mygo build` 打包的应用自带这个库。

用真实服务器跑集成测试（连接、终端、监控、拖拽上传、下载）：

```sh
FINALSHELLY_TEST_SSH=user:password@127.0.0.1:22 go test ./internal/app -run SSH -v
```

## 功能

- 中文和英文界面。默认跟随系统语言，点左下角“会话”标题旁的齿轮按钮打开“选项”窗口，在“常规”页切换，选择会保存。
- “选项”窗口（和 FinalShell 一样，左边是页面树，右边是页面）。“常规”页：
  - 界面语言：跟随系统 / English / 简体中文。
  - 自动选中标签（默认开）：新连接打开后切换过去。关掉后新会话在后台打开，当前会话保持选中；没有选中的会话时仍然选中新的。
  - 关闭时最小化到托盘（默认关）：点主窗口的关闭按钮时隐藏到托盘图标（macOS 是菜单栏），图标菜单里有“显示主窗口”和“退出”。Windows 上左键单击图标也能显示窗口，macOS 上点 Dock 图标也能显示。系统没有托盘时（Linux 没装 `libayatana-appindicator3`）按没开这一项处理。
  - 关闭窗口前确认（默认开）：有会话已连接或正在连接时，关闭主窗口或退出（托盘菜单、macOS 的 Cmd+Q）之前用系统的消息框问一次。同时开了托盘：关闭按钮直接隐藏，不问；“退出”才问。
  - “终端”下的鼠标、快捷键、配色、背景图片、字体几页还没做，先显示“以后的版本提供”。
  所有选项改了立刻生效，保存在 `settings.json`。
- 连接管理器：新建、编辑、删除主机，密码或私钥登录。主机列表存在 `<用户配置目录>/finalshelly/hosts.json`，密码不落盘，只存在内存里。
- 新标签页（启动时打开一个）：“快速连接”列出最近连接过的主机（名称、主机、用户名），最后连接的在最上面，点一下或按回车就在这个标签页里连接，同一行变成会话。搜索框边输入边过滤，按名称、地址、用户名、备注匹配所有主机（最近连接过的排在前面），上下方向键选择。“清空”清掉最近连接记录，不删主机。“连接管理器”按钮打开连接管理器；在新标签页选中时从连接管理器连接，也连在这个标签页里。一个主机都没有时显示“新建连接”按钮。
- 连接编辑器（新建连接 / 编辑连接）是单独的窗口，左边是页面，下面是“确定”“取消”：
  - “SSH 连接”：名称、主机、端口、备注（多行），认证方法（密码或私钥）、用户名、密码或私钥文件（“浏览…”选文件）和私钥密码，“启用 exec 通道（服务器监控）”。错误提示显示在对应的输入框下面。
  - “终端”：终端类型（发给服务器的 `TERM`，默认 `xterm-256color`）；退格键发送 `^?`（DEL，默认）或 `^H`，同时把服务器终端的 erase 字符设成一样的；字符编码目前只有 UTF-8（libghostty-vt 只解码 UTF-8，程序也不做转码）。
  - “代理”“隧道”：以后的版本提供。
  - 回车等于“确定”（备注里回车是换行），Esc 等于“取消”。同时只开一个编辑器：已经开着时再点新建或编辑，只会把开着的那个提到前面，里面没保存的内容不会丢。修改已经打开会话的主机，下次连接时生效。编辑期间主机在连接管理器里被删除了，“确定”只提示“此连接已被删除”，不会把它存回去。
- 连接成功时记下时间，供“最近连接”使用。主机的 `noMonitor` 为 `true` 时不轮询服务器状态，也就不开额外的 exec 通道（有些跳板机不允许）。
- 第一次连接时弹窗显示主机指纹，信任后记入 `finalshelly/known_hosts`；指纹变化时拒绝连接。
- 左上：当前会话服务器的 IP、运行时间、负载、CPU/内存/交换、内存占用前几的进程、网速和延迟曲线、磁盘。每 2 秒通过 SSH 读取一次 `/proc`，只支持 Linux 服务器。
- IP 下方的“系统信息”按钮：弹出主机名、系统（`/etc/os-release`）、内核、架构、CPU 型号和核数、内存、运行时间、IP 地址、硬盘，“复制”按钮把这些复制成纯文本。每次打开时通过一个 exec 通道读取一次，用一段 POSIX sh 脚本，BusyBox 也能跑，缺 `lscpu`、`nproc`、`ip` 之类的工具时跳过或换用别的。未连接时左上显示 FinalShell 那样的空白占位（“-”、0%）。
- 左下：竖向会话列表，状态点（绿：已连接，黄：连接中，红：失败），右键菜单（重新连接、复制会话、断开、关闭），拖动排序。“+”按钮新开一个新标签页；新标签页和会话一样可以切换、拖动、关闭，标题里的数目不算新标签页。全部关掉后右边仍显示快速连接，在那里连接会新开一个标签页。
- 右上：终端。断开后保留最后的画面，顶部出现“重新连接”。
- 右下“文件”页：SFTP 浏览，左边目录树，右边文件表格（可多选），路径栏可以直接输入路径。
- 上传：把文件或文件夹从资源管理器 / Finder 拖到文件表格（传到当前目录）或目录树的某个文件夹上；也可以用工具栏的上传按钮或右键菜单选文件、选文件夹。
- 下载：选中文件或文件夹，点下载按钮（存到系统的“下载”文件夹），或右键“下载到…”自己选位置。
- 右下“传输”页：进度、速度、取消；同时最多跑 3 个，其余排队。目标位置已有同名项目时先询问是否覆盖。下载完成后可以“在文件夹中显示”。
- 拖动区域之间的分隔条调整大小（获得焦点后也可以用方向键），双击恢复默认大小。每个区域有最小尺寸，不会被拖到看不见；大小保存在 `finalshelly/settings.json`，下次启动恢复。

快捷键：Ctrl+Shift+T 连接管理器（macOS 也可以 Cmd+T），Ctrl+Tab / Ctrl+Shift+Tab 切换会话（包括新标签页），Ctrl+Shift+W 关闭会话或新标签页。终端内复制粘贴是 Ctrl+Shift+C / Ctrl+Shift+V（macOS 是 Cmd+C / Cmd+V）。

## 配置文件

`hosts.json` 从第 2 版起是一个对象（第 1 版是主机数组，第一次读取时自动迁移，原文件另存为 `hosts.v1.json`，下次保存时改写成第 2 版）：

```json
{
  "version": 2,
  "folders": [{"id": "f1", "name": "生产"}, {"id": "f2", "name": "数据库", "parent": "f1"}],
  "hosts": [{
    "id": "h1", "name": "web", "addr": "203.0.113.10", "port": 2222, "user": "root",
    "auth": "key", "keyPath": "~/.ssh/id_ed25519",
    "folder": "f2", "notes": "备注", "noMonitor": true,
    "termType": "vt220", "backspace": "ctrl-h",
    "lastConnectedAt": "2026-11-07T10:00:00+08:00"
  }]
}
```

`folder` 是所在文件夹的 ID，空表示顶层；文件夹用 `parent` 嵌套。有 `deletedAt`（删除时间）的主机是“已删除”的，列表里不显示。空值的字段不写出来，没有 `noMonitor` 就是开启监控，没有 `termType` 就是 `xterm-256color`，没有 `backspace` 就是发送 `^?`（`ctrl-h` 是发送 `^H`）。不认识的字段（新版本写的）原样保留，保存时不会丢。写入时先写临时文件再改名，不会写出半个文件。如果 `hosts.json` 读不出来（比如被手改坏了），启动时先把它原样复制成同目录下的 `hosts.json.broken-<年月日-时分秒>`，再按空列表启动，下次保存不会把原来的内容弄丢。

## 代码结构

Go 里一个文件夹就是一个包（package），包之间不能循环引用，所以按职责拆分。`internal/` 下的包只能被本项目使用。

```
main.go              启动 MyGo、开主窗口，把系统功能（子窗口、对话框、下载目录、语言）交给 app
tray.go              托盘图标、确认用的消息框、退出
locales/             界面文案：en.json、zh-Hans.json；catalog_gen.go 由 mygo 生成，不要手改
internal/
  config/            主机和文件夹（hosts.json）、设置的读写，配置目录
  sshx/              SSH 连接、认证、known_hosts、给终端用的 Conn
  monitor/           服务器状态采集和解析
  remotefs/          SFTP 列目录、上传、下载（带进度和取消）
  icons/             内嵌的 SVG 图标（Lucide，ISC 协议）
  widgets/           可复用的小控件：图标按钮、用量条、折线图、表单行、左侧导航加页面（nav.go）
  app/               界面和状态
    app.go           App 状态、整体布局、快捷键、打开/关闭会话
    session.go       一个会话：连接、终端、监控、SFTP；错误信息的翻译
    sidebar.go       左侧：监控面板、竖向会话列表
    sysinfo.go       “系统信息”弹出框：读取、加载和错误状态、复制
    terminal.go      终端区域
    start.go         新标签页：快速连接、搜索、清空最近连接
    splits.go        分隔条：各区域的最小尺寸，大小的保存和恢复
    files.go         文件面板：目录树、文件表格、拖入上传
    transfers.go     传输队列和“传输”页
    dialogs.go       连接管理器、密码、主机指纹、覆盖确认
    options.go       选项窗口：页面列表和“常规”页
    editor.go        连接编辑器窗口：a.newConnection()、a.editConnection(h)，校验和保存
    closing.go       关闭主窗口、退出时做什么：隐藏到托盘、先确认
    windows.go       子窗口：按种类打开/聚焦/关闭，重绘所有窗口
    format.go        大小、速度、延迟的格式化
```

依赖方向是单向的：`app` 用其他所有包，`widgets` 只用 `icons`，其余包互不依赖界面。

### 子窗口

和 FinalShell 一样，设置这类对话框是独立的窗口（MyGo 的子窗口，`WindowOptions.Parent`），不是主窗口里的浮层。目前有选项窗口和连接编辑器；连接管理器之后也会改成子窗口。连接编辑器现在是主窗口的普通（非模态）子窗口，因为连接管理器还是主窗口里的浮层：模态窗口在 macOS 上会变成盖住主窗口的 sheet，在 Linux 上会挡住所有窗口。连接管理器改成窗口后，可以把编辑器的 `Parent` 设成它、改成模态。

- 每种窗口（`WindowKind`）同时只开一个：`a.openWindow(spec, view)` 在已经打开时只把它提到前面。`WindowSpec` 写标题（locales 里的函数，切换语言时重新取）、内容区大小、最小尺寸、父窗口（默认主窗口）和是否模态。
- 窗口里按 Esc 或调用 `a.closeWindow(kind)` 关闭。关闭父窗口会一起关闭它的子窗口；关闭主窗口会关闭所有子窗口。
- 所有窗口共用一个 `App`，都在 UI 线程上。一个窗口只会在自己的输入之后重绘；改了别的窗口也显示的状态（保存的主机、设置）之后调用 `a.redraw()`，重绘所有窗口。`App.post` 投递的工作做完也会重绘所有窗口。
- 测试里 `fakePlatform` 记下打开的窗口，`p.show(t, kind)` 用一个单独的 `ui.Tester` 画出它的内容。

各平台的行为（读 MyGo v0.8.1 后端代码确认的，还没在真机上逐一试过）：

| | 普通子窗口 | 模态（`Modal`） |
|---|---|---|
| macOS | 普通 `NSWindow`，总在父窗口上面，父窗口最小化时跟着隐藏 | 父窗口的 sheet（从标题栏下方滑出，没有自己的标题栏，位置由系统决定）；父窗口本身是子窗口时也一样 |
| Windows | 父窗口拥有的窗口（owner），总在父窗口上面，没有自己的任务栏按钮 | 打开期间禁用父窗口（`EnableWindow`），只禁用直接的父窗口 |
| Linux | GTK `transient_for` 父窗口 | `gtk_window_set_modal`：GTK 会挡住本程序其他所有窗口的输入，不只是父窗口。Wayland 下窗口位置由合成器决定 |

子窗口打开时居中在父窗口上方，不能最小化。

### 选项窗口加一页

页面都列在 `internal/app/options.go` 的 `optionPages` 里，按树的顺序，父页面在子页面前面。`view` 为 `nil` 的页面显示“以后的版本提供”。给一页加内容：在新文件里写 `func (a *App) fontPage(c *ui.Context)`，把那一项的 `view: nil` 改成 `view: (*App).fontPage`；新页面就加一项 `{id: ..., parent: ..., title: locales.Xxx, view: ...}`。每项占几行，几个人同时改不同的页面不会改到同一行。页面里改设置用 `a.changeSettings(func() { ... })`，会保存并重绘所有窗口。

### 关闭和退出

| | 点关闭按钮 | 退出（托盘菜单、Cmd+Q、SIGINT） |
|---|---|---|
| 开了托盘（且系统有托盘） | 隐藏到托盘，不问 | 有会话打开且开了确认：先问 |
| 没开托盘 | 有会话打开且开了确认：先问，确定后退出 | 同左 |

“有会话打开”指已连接或正在连接。逻辑在 `closing.go`（`CloseRequested`、`QuitRequested`，用假的 Platform 测试），`main.go` 在主窗口的 `OnClose` 和 `App.OnBeforeQuit` 里调用它们。确认框是 `mygo.Dialog.Message`，在 goroutine 里等用户回答，答“退出”后再调 `mygo.App.Quit()`。

各平台的行为是读 MyGo v0.8.1 代码确认的，没有在真机上试过：macOS 托盘图标在菜单栏，是模板图（随菜单栏变色），点击弹出菜单；Dock 图标一直在，点它（`OnActivate`）显示隐藏的窗口；确认框是主窗口的 sheet（窗口隐藏时是独立的框）。Windows 左键单击图标显示窗口，右键弹出菜单，模板图在深色任务栏上画成白色。Linux 用 AppIndicator，需要 `libayatana-appindicator3`，没有时 `NewTray` 报错，窗口不隐藏；GNOME 默认不显示 AppIndicator 图标（要装扩展），这时图标创建成功但看不见：再启动一次程序就能把窗口找回来（见下）。

程序同时只运行一个（`mygo.App.RequestSingleInstanceLock`）：再次启动时，新进程把命令行交给已经在运行的那个后退出，运行中的程序把主窗口显示出来（在托盘里就从托盘恢复，最小化了就还原）并放到最前面。这样两个进程也不会同时写 `hosts.json`、`settings.json`。锁按应用名区分（打包后加上 identifier，`mygo dev` 的开发版和安装版互不影响），程序退出时释放，崩溃留下的锁文件下次启动会清掉。Wayland 下靠新进程带来的激活令牌获得焦点，Windows 下新进程把前台权限让给运行中的程序；macOS 从 Finder 或 Dock 打开时系统本来就只开一个，点 Dock 图标走 `OnActivate`，同样会把托盘里的窗口显示出来。

### 加一条文案

1. 在 `locales/en.json` 里加 key 和英文（带参数的写在 `@key.placeholders` 里），在 `locales/zh-Hans.json` 里加中文。
2. 运行 `go tool mygo generate`（`mygo dev` 会自动做），代码里用 `locales.Key(...)`。

格式是 ICU MessageFormat，见 MyGo 的 [i18n 文档](https://github.com/egoist/mygo/blob/main/docs/i18n.md)。
