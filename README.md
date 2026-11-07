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
  - “终端”下的配色、背景图片、字体几页还没做，先显示“以后的版本提供”。
  所有选项改了立刻保存在 `settings.json`，除了下面“鼠标”页说的两项，也立刻生效。
- 选项的“终端 › 鼠标”页，用的都是 terminal 插件本身的功能：
  - 选中即复制（默认关）：用鼠标选中文字（还有“全选”）后自动复制到剪贴板。
  - 右键：显示右键菜单（默认）/ 粘贴 / 复制选中的文字 / 有选中的文字时复制、没有时粘贴 / 不做任何操作。服务器上的程序接管鼠标时（比如开了 `mouse=a` 的 vim）右键交给程序，按住 Shift 才按这里的设置。
  - Ctrl+单击打开链接（macOS 是 Cmd+单击，默认开）：终端里显示的网址和 OSC 8 超链接用系统浏览器打开。关掉后单击链接什么也不做；插件没有关掉链接识别的开关，所以按住 Ctrl 时链接仍然显示下划线（文件路径也会显示），只是不打开。
  - 鼠标滚轮每格滚动行数（1 到 20，默认 3）：只管鼠标滚轮，不改触控板。Windows 上 3 对应系统鼠标设置里的“一次滚动的行数”，其他值按比例换算。
  - 选中即复制和滚动行数在已打开的终端里立刻生效（插件有 `SetCopyOnSelect`、`SetScrollMultiplier`）。右键和链接插件只在创建终端时读取，没有 setter，所以已打开的会话重新连接后才生效；重建正在用的终端会丢掉屏幕和滚动历史，所以不这么做。
  - 在 `settings.json` 里是 `copyOnSelect`、`rightClick`（`paste`、`copy`、`copy-or-paste`、`ignore`；没有或不认识的值是右键菜单）、`noLinkClick`、`wheelLines`（没有是 3，超过 20 按 20）。
  - 没有中键粘贴：插件只把中键交给接管鼠标的程序，自己不处理，也不公开程序是否接管了鼠标，应用另外处理中键的话，程序会同时收到中键和粘贴的文字。插件也没有 Linux 的 PRIMARY 选区。要做需要插件加一个像 `RightClick` 那样的中键选项。
- 选项的“终端 › 快捷键”页：表格列出每个操作、现在的快捷键和默认的快捷键（见下面的快捷键表）。选中一行点“修改…”，在弹出的框里按下新的组合键，“确定”保存；“使用默认”填回默认键，“恢复默认”恢复选中的一行，“全部恢复默认”恢复所有。
  - shell 需要的键不让用，框里用红字说明原因：不带修饰键（或只带 Shift）的键；只带 Ctrl 的字母、数字、标点、空格和方向键等（Ctrl+C、Ctrl+R、Ctrl+W、Ctrl+←……）；只带 Alt 的这些键（Alt+B、Alt+F 在 shell 里是 Meta，按词移动）；macOS 上只带 Option 的这些键（会输入字符）；Windows 上 Ctrl+Alt 加字母（就是 AltGr，很多键盘布局用它输入字符）；系统占用的键（macOS 的 Cmd+Q、Cmd+H、Cmd+M、Cmd+Tab、Cmd+空格等，Windows/Linux 的 Alt+Tab、Alt+F4，Windows 上的 Win 键组合）。例外：“切换到第 N 个标签”可以用 Alt 加数字（readline 的数字参数很少用，GNOME Terminal 也这样），“重新连接”可以用任何键（它只在会话断开、没有 shell 的时候起作用）。
  - 和别的操作冲突时用黄字提示，“确定”后那个操作变成没有快捷键（显示“无”）。和终端自己的键（Ctrl+Shift+A 全选、Ctrl+= 缩放、Shift+PageUp 滚动、Ctrl+Insert / Shift+Insert 复制粘贴）冲突时也提示，确定后归这个快捷键。
  - 窗口的快捷键立刻生效。终端的复制、粘贴是 terminal 插件的 `Keybinds`，插件只在创建终端时读取，所以改了之后新连接（包括重新连接）的会话才生效。
  - macOS 上这一页还有“将 Option 用作 Alt（Meta）键”（默认关，插件的 `OptionAsAlt`）：开了之后 Option+B 发送 Esc b，而不是输入 ∫。同样对之后连接的会话生效。
  - 在 `settings.json` 里只存改过的键：`"shortcuts": {"find": "Ctrl+Alt+F", "closeTab": "none"}`，键名是 `copy`、`paste`、`find`、`fullScreen`、`reconnect`、`previousTab`、`nextTab`、`closeTab`、`connectionManager`、`newTab`、`switchTab`（值像 `"Alt+1-9"`），`Super` 在 macOS 上是 Cmd，`none` 是没有快捷键；没有或读不出来的就是默认。Option 用作 Alt 是 `optionAsAlt`。
- 终端查找（Ctrl+Shift+F，macOS 是 Cmd+F）：终端右上角出现查找栏（MyGo 的 `ui.FindBar`），边输入边用插件的 `Search` 搜屏幕和滚动历史，高亮所有匹配，显示“第几项，共几项”。回车到下一个（往上，更早的输出），Shift+回车到上一个，F3 / Shift+F3（macOS 是 Cmd+G / Shift+Cmd+G）也行；Esc 或“完成”关闭，结束搜索，键盘回到终端。再按一次快捷键时全选查找框里的文字。每个会话的查找栏分开。不区分 ASCII 字母大小写（插件就是这样）。
- 全屏（Ctrl+Shift+Enter，macOS 是 Cmd+Enter）：隐藏左边栏和下面的文件面板，只留终端，窗口也进入系统的全屏；再按一次退出。Esc 只在没有 shell 接收它时退出全屏（新标签页、断开的会话），连接着的终端里 Esc 照常发给 shell（vi 要用）。窗口本来就是系统全屏的，退出时保持系统全屏；用系统的办法（macOS 的绿色按钮）退出系统全屏，也一起退出程序的全屏。
- 连接管理器：新建、编辑、删除主机，密码或私钥登录。主机列表存在 `<用户配置目录>/finalshelly/hosts.json`，密码不落盘，只存在内存里。
- 新标签页（启动时打开一个）：“快速连接”列出最近连接过的主机（名称、主机、用户名），最后连接的在最上面，点一下或按回车就在这个标签页里连接，同一行变成会话。搜索框边输入边过滤，按名称、地址、用户名、备注匹配所有主机（最近连接过的排在前面），上下方向键选择。“清空”清掉最近连接记录，不删主机。“连接管理器”按钮打开连接管理器；在新标签页选中时从连接管理器连接，也连在这个标签页里。一个主机都没有时显示“新建连接”按钮。
- 连接编辑器（新建连接 / 编辑连接）是单独的窗口，左边是页面，下面是“确定”“取消”：
  - “SSH 连接”：名称、主机、端口、备注（多行），认证方法（密码或私钥）、用户名、密码或私钥文件（“浏览…”选文件）和私钥密码，“启用 exec 通道（服务器监控）”。错误提示显示在对应的输入框下面。
  - “终端”：终端类型（发给服务器的 `TERM`，默认 `xterm-256color`）；退格键发送 `^?`（DEL，默认）或 `^H`，同时把服务器终端的 erase 字符设成一样的；字符编码目前只有 UTF-8（libghostty-vt 只解码 UTF-8，程序也不做转码）。
  - “代理”：类型选“不使用”（直接连接）、SOCKS5、HTTP 或“跳板机”。
    - SOCKS5 和 HTTP（`CONNECT`）填代理的主机、端口（默认 1080 / 8080），代理需要认证时填用户名和密码（HTTP 是 Basic 认证）。SOCKS5 由代理解析主机名，所以主机可以填只有代理那边认识的名字。
    - “跳板机”从已保存的其他连接里选一个（不能选自己），相当于 `ssh -J`：先登录跳板机，再由跳板机的 SSH 服务器转发到这台主机（跳板机要允许 TCP 转发）。跳板机自己也可以设代理或跳板机，按顺序一跳一跳连过去；绕回来形成循环时，编辑器和连接时都会提示 “跳板机形成了循环：a → b → a”。
    - 每一跳都和直接连接它一样：第一次连接时确认它的主机指纹，用它自己的登录方式和密码。连接时，还不知道的密码按顺序一个一个问：先问主机自己的，再问它的代理的，再问各个跳板机的（标题写“输入跳板机密码”或“输入代理密码”，下面写是哪个主机、为了连接谁）。主机改成走跳板机后重新连接，也会先问新跳板机的密码。
    - 代理密码和主机密码一样只存在内存里，不写进 `hosts.json`。
    - 出错时说明是哪一跳：“代理 1.2.3.4:1080：连接被拒绝：端口没有开放代理服务”“跳板机 web-1：认证失败：…”“跳板机 web-1：无法连接到 10.0.0.5:22：…”。
    - 关闭或断开会话时，跳板机的连接一起关闭。
  - “隧道”：以后的版本提供。
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

快捷键（默认，和 FinalShell 一样，都可以在“终端 › 快捷键”页修改）：

| 操作 | macOS | Windows / Linux |
|---|---|---|
| 终端：复制 | Cmd+C | Ctrl+Shift+C |
| 终端：粘贴 | Cmd+V | Ctrl+Shift+V |
| 终端：查找 | Cmd+F | Ctrl+Shift+F |
| 终端：全屏 | Cmd+Enter | Ctrl+Shift+Enter |
| 终端：重新连接（会话断开时，焦点在终端区域） | Enter | Enter |
| 上一个标签 | Cmd+[ | Ctrl+Shift+Tab |
| 下一个标签 | Cmd+] | Ctrl+Tab |
| 关闭标签 | Cmd+W | Ctrl+Shift+W |
| 连接管理器 | Cmd+O | Ctrl+Shift+O |
| 新标签页 | Cmd+T | Ctrl+Shift+T |
| 切换到第 1–9 个标签 | Cmd+1…9 | Alt+1…9 |

- 以前 Ctrl+Shift+T 打开连接管理器，现在和别的终端程序一样是新标签页；新标签页上就有最近的连接和“连接管理器”按钮。连接管理器改成 Ctrl+Shift+O。
- 关闭标签在 macOS 上是 Cmd+W，不是 FinalShell 的 Cmd+Q（那是退出）。为了让 Cmd+W 到达窗口，macOS 上的菜单栏去掉了 MyGo 默认的“文件”菜单（里面的“关闭窗口”占着 Cmd+W）。主窗口里 Cmd+W 关闭标签；子窗口（选项、连接编辑器）里 Cmd+W 关闭这个子窗口，和别的 Mac 程序一样（“关闭标签”改成别的键后，那个键也关闭子窗口），Esc 也照样关闭。Windows 和 Linux 上子窗口用 Esc 和系统的关闭按钮。
- macOS 上切换标签只用 Cmd+数字：Option+数字会输入字符（¡™£……）。
- Windows 上 terminal 插件默认还把 Ctrl+C（有选中时复制）和 Ctrl+V（剪贴板有内容时粘贴）绑成复制粘贴，这里去掉了，和 Linux 一样留给 shell（中断、原样输入下一个键）。复制粘贴用 Ctrl+Shift+C / Ctrl+Shift+V，或 Ctrl+Insert / Shift+Insert。
- 只带 Ctrl 的字母键都是 shell 的（Ctrl+A……Ctrl+Z 等），在 Windows/Linux 上不当快捷键用。MyGo 的 legacy 键盘模式下 Ctrl+[、Ctrl+I、Ctrl+M 什么也不发送，是上游的问题（#21），这里没有绕开。

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
    "proxy": {"kind": "jump", "jumpHost": "h2"},
    "lastConnectedAt": "2026-11-07T10:00:00+08:00"
  }, {
    "id": "h2", "name": "bastion", "addr": "198.51.100.1", "user": "jump", "auth": "password",
    "proxy": {"kind": "socks5", "addr": "127.0.0.1", "port": 1080, "user": "alice"}
  }]
}
```

`proxy` 是怎么连到这台主机：`kind` 为 `socks5`、`http`（这两种有 `addr`、`port`，需要认证时有 `user`）或 `jump`（`jumpHost` 是跳板机的主机 ID）；没有 `proxy` 就是直接连接。代理密码不写进文件。

`folder` 是所在文件夹的 ID，空表示顶层；文件夹用 `parent` 嵌套。有 `deletedAt`（删除时间）的主机是“已删除”的，列表里不显示。空值的字段不写出来，没有 `noMonitor` 就是开启监控，没有 `termType` 就是 `xterm-256color`，没有 `backspace` 就是发送 `^?`（`ctrl-h` 是发送 `^H`）。不认识的字段（新版本写的）原样保留，保存时不会丢。写入时先写临时文件再改名，不会写出半个文件。如果 `hosts.json` 读不出来（比如被手改坏了），启动时先把它原样复制成同目录下的 `hosts.json.broken-<年月日-时分秒>`，再按空列表启动，下次保存不会把原来的内容弄丢。

## 代码结构

Go 里一个文件夹就是一个包（package），包之间不能循环引用，所以按职责拆分。`internal/` 下的包只能被本项目使用。

```
main.go              启动 MyGo、开主窗口，把系统功能（子窗口、对话框、下载目录、语言）交给 app
tray.go              托盘图标、确认用的消息框、退出
locales/             界面文案：en.json、zh-Hans.json；catalog_gen.go 由 mygo 生成，不要手改
internal/
  config/            主机和文件夹（hosts.json）、设置的读写，配置目录；proxy.go 是代理设置和跳板机链（找循环）
  sshx/              SSH 连接、认证、known_hosts、给终端用的 Conn；proxy.go 是经过 SOCKS5、HTTP CONNECT 和跳板机连接
  monitor/           服务器状态采集和解析
  remotefs/          SFTP 列目录、上传、下载（带进度和取消）
  icons/             内嵌的 SVG 图标（Lucide，ISC 协议）
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
    transfers.go     传输队列和“传输”页
    dialogs.go       连接管理器、密码、主机指纹、覆盖确认
    options.go       选项窗口：页面列表和“常规”页
    mouse.go         “终端 › 鼠标”页，以及这些设置怎样变成终端的选项
    shortcuts.go     快捷键：操作和各平台的默认键、哪些键不能用、冲突、窗口快捷键的分派、终端的 Keybinds
    shortcutspage.go “终端 › 快捷键”页和按键录制框
    keycombo.go      组合键：在 settings.json 里的写法和界面上的显示（macOS 用 ⌘⇧ 符号）
    editor.go        连接编辑器窗口：a.newConnection()、a.editConnection(h)，校验和保存
    editor_proxy.go  连接编辑器的“代理”页
    proxy.go         经过代理和跳板机连接：拨号用的选项、连接前要问的密码、各跳错误的翻译
    closing.go       关闭主窗口、退出时做什么：隐藏到托盘、先确认
    windows.go       子窗口：按种类打开/聚焦/关闭，重绘所有窗口
    format.go        大小、速度、延迟的格式化
```

依赖方向是单向的：`app` 用其他所有包，`widgets` 只用 `icons`，其余包互不依赖界面。

### 子窗口

和 FinalShell 一样，设置这类对话框是独立的窗口（MyGo 的子窗口，`WindowOptions.Parent`），不是主窗口里的浮层。目前有选项窗口和连接编辑器；连接管理器之后也会改成子窗口。连接编辑器现在是主窗口的普通（非模态）子窗口，因为连接管理器还是主窗口里的浮层：模态窗口在 macOS 上会变成盖住主窗口的 sheet，在 Linux 上会挡住所有窗口。连接管理器改成窗口后，可以把编辑器的 `Parent` 设成它、改成模态。

- 每种窗口（`WindowKind`）同时只开一个：`a.openWindow(spec, view)` 在已经打开时只把它提到前面。`WindowSpec` 写标题（locales 里的函数，切换语言时重新取）、内容区大小、最小尺寸、父窗口（默认主窗口）和是否模态。
- 窗口里按 Esc（macOS 上还有 Cmd+W）或调用 `a.closeWindow(kind)` 关闭。关闭父窗口会一起关闭它的子窗口；关闭主窗口会关闭所有子窗口。
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
