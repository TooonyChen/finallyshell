# 开发

[English](../en/development.md) · 简体中文

怎样编译、测试和发布 Finallyshell，以及代码是怎么组织的。仓库的规则（先开 issue、分支名、提交信息、pull request）见 [CONTRIBUTING.md](../../CONTRIBUTING.md) 和 [CLAUDE.md](../../CLAUDE.md)。

- [运行](#运行)
- [测试](#测试)
- [提交前的检查](#提交前的检查)
- [截图](#截图)
- [发布](#发布)
- [给更新签名](#给更新签名)
- [代码结构](#代码结构)
- [子窗口](#子窗口)
- [选项窗口加一页](#选项窗口加一页)
- [加一个导入来源](#加一个导入来源)
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

跑测试之前，CI 先运行 `go run ./tools/natives`，把 libghostty-vt 下载到用户缓存目录。它最多试五分钟，两次之间依次等 2、4、8、16 秒，之后每次等 30 秒，时间到了还不成功就报最后一次的错误。GitHub 有时对这个下载连续几分钟返回 504，那样所有打开终端的测试都会失败。CI 在两次运行之间保留缓存目录里的 `mygo/natives` 文件夹（Linux runner 上是 `~/.cache/mygo/natives`），按 `go.sum` 区分，并且下载成功后马上保存，即使之后测试失败也保留，所以大多数运行根本不用下载。

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

发布一个版本，就是在 `CHANGELOG.md` 里写好它的一节，合并，再把 main 推到 `production` 分支。

1. 在 `CHANGELOG.md` 最上面、标题下面加这一节，放在发布前最后合并的那个 PR 里，或者单独开一个 PR：

   ```markdown
   ## 0.2.2

   ### English

   - …

   ### 简体中文

   - …
   ```

   标题是版本号，格式是 `MAJOR.MINOR.PATCH`，要比上一个 `v*` tag 高，由所有者决定。两部分都要有：每次发布的说明都有英文和简体中文。写用户看得到、用得到的变化，不要照搬提交标题；`go run ./tools/release draft` 会列出上一个 tag 之后的 `feat`、`fix`、`perf` 和破坏性改动的提交，可以从这里开始写。每条写在一行里，免得换行在中文里多出空格。tools/release 的 `TestChangelogFile` 在每个 PR 里检查这个文件：标题都是版本号、新的在前，最新的一节两种语言都有。这个文件从 0.2.1 开始，更早版本的说明在它们的 GitHub Release 上。
2. 合并以后运行：

   ```sh
   git push origin origin/main:production
   ```

`.github/workflows/release.yml` 接着做这些事：

1. 算版本号（`go run ./tools/release version`）：`CHANGELOG.md` 最新的一节，比上一个 `v*` tag 新的时候就是要发布的版本。这个版本已经打过 tag 时没有要发布的，workflow 到这里结束，所以没有新的一节就推 main，或者重新运行 workflow，都不会发布。这一节比上一个 tag 旧、有标题不是版本号、或者缺英文或中文部分时失败，这时什么都还没编译。还没有 tag 时，最新的一节就是第一次发布。

   上一个 tag 之后的提交类型（Conventional Commits）只用来核对版本号：破坏性改动（`feat!:`、`BREAKING CHANGE:`）应该升主版本（1.0 之前升次版本），`feat` 升次版本，`fix` 和 `perf` 升修订号。版本号比这个低时在运行记录里给出警告，照样发布：0.2.0 之后有 `feat` 提交，0.2.1 就是这种情况。
2. 跑和 PR 一样的检查（`.github/workflows/ci.yml`：gofmt、`go vet`、`go tool mygo vet`、`go test -race`）。
3. 编译四个平台：macOS arm64 和 Intel x86_64 在 macOS runner 上（ad hoc 签名、dmg），Linux x86_64 和 Windows x86_64 在 Linux runner 上交叉编译（Windows 安装程序用 apt 装的 NSIS）。版本号只在 runner 里写进 `mygo.json`，main 上的不改。`LICENSE` 和第三方许可证（`go run ./tools/release licenses`）放进 `resources/`，随应用一起打包。

   编译时有私钥 `MYGO_UPDATER_PRIVATE_KEY`（见[给更新签名](#给更新签名)），所以 `mygo build` 还会写出每个平台的更新：打成压缩包的应用（`finallyshell-<版本>-<平台>.tar.gz`）、从最近三个有更新的发布出发的增量更新（`…-<旧版本>-to-<版本>-….delta`），以及列出它们和签名的清单 `update-<平台>.json`。清单里的说明就是 `CHANGELOG.md` 里这个版本的一节，更新窗口显示的也是它；没有这一节 `mygo build` 会失败。没有这个 secret、没有清单、清单不是这个版本、这个发布的，或者清单里的说明和 `go run ./tools/release notes <版本>` 不一样，任务失败；否则把清单和它列出的文件跟安装包放在一起。

   `mygo build` 从 GitHub 下载每个目标平台的原生库（libghostty-vt），放进缓存目录的 `mygo/natives` 文件夹（macOS runner 上是 `~/Library/Caches/mygo/natives`，Linux runner 上是 `~/.cache/mygo/natives`），遇到超时就放弃。这个任务在两次运行之间保留这个文件夹，按 runner 的系统、目标平台和 `go.sum` 区分，编译成功后保存。编译由 `.github/scripts/build-with-retries.sh` 运行：下载失败时（输出以 `error: downloading …` 结尾）它会重新编译，最多试十分钟，两次之间依次等 15、30 秒，之后每次等 60 秒，时间到了就报最后一次的错误；失败之前下载好的文件留在缓存里，下一次只下载还缺的。其他错误立即让编译失败。`.github/scripts/build-with-retries-test.sh` 用一个假的 `mygo` 测试这个脚本。一个平台失败不会取消其他平台（`fail-fast: false`）；四个平台都编译成功才会发布。
4. 检查四个平台的清单都在，然后打 tag `v<版本>`，发布 GitHub Release，附上安装包、更新文件、`SHA256SUMS`、`LICENSE.txt`、`THIRD_PARTY_LICENSES.txt`。说明是这个版本的一节（`go run ./tools/release notes <版本>`），后面接上 `.github/release-install.md`，即中英文的安装说明。发布是正式发布，不是草稿，因为已安装的应用读取的是最新发布的清单；`gh release create` 上传文件期间让它保持草稿，所以应用不会看到压缩包还没上传的清单。

`production` 只应该从 main 快进，在仓库设置里给它加分支保护（禁止强推）。

## 给更新签名

已安装的应用只接受用项目的 Ed25519 密钥签名的更新（MyGo 的[自动更新](https://github.com/egoist/mygo/blob/main/docs/updates.md)）。这个密钥是所有者 2026-11-08 用 `mygo keygen` 生成的：

- 私钥是仓库的 secret `MYGO_UPDATER_PRIVATE_KEY`，所有者另外保存一份。它从不提交，也不写进 `updates.privateKey`。丢了它，所有已安装的副本都停在当前版本；拿到它的人可以给用户推送代码。
- 公钥是 `mygo.json` 里的 `updates.publicKey`；main 包的 `TestUpdatesConfig` 检查它还在。
- 没有私钥的编译，比如在自己电脑上 `go tool mygo build`，照样编译应用，只是不写更新文件。PR 的 CI 不编译。

应用这边是 `updates.go`：用 updater 插件的原生 UI 窗口（`native.New`），不设 `Language`，这样窗口用 `App.Locale`，它跟随“界面语言”选项；再通过 `Platform` 的 `Updates`、`CheckForUpdates`、`SetAutomaticUpdateChecks` 把选项那一行要的状态交给 `internal/app`。`updater.OnChange` 在 UI 线程上运行，只重画窗口。

不发布也能试更新：用一次性的密钥编译两个版本，从本机提供下载：

1. `go tool mygo keygen -o /tmp/keys`，在 `mygo.json` 的副本里把 `updates` 设成这个公钥和 `"url": "https://localhost:8443/u"`。有密钥的编译需要 `CHANGELOG.md` 里有这个版本的一节，所以在副本里加上 `## 0.2.0` 和 `## 0.3.0` 两节，或者让 `updates.changelog` 指向有这两节的文件。
2. 设 `MYGO_UPDATER_PRIVATE_KEY="$(cat /tmp/keys/mygo-update.key)"`，编译 0.2.0（`go tool mygo build -o dist-old`），用给 `localhost` 的自签名证书通过 HTTPS 在 `u/` 下提供它的压缩包和清单；再编译 0.3.0，它会生成从这个 0.2.0 出发的增量更新。提供它的压缩包、增量更新和清单。
3. 把 0.2.0 的压缩包解压到 `~/.local/finallyshell.app`（用单独的 `HOME`），启动时让 `SSL_CERT_FILE` 指向包含这个证书的证书包，并设 `NO_PROXY=localhost`。10 秒后出现更新窗口。在 `updater.json` 里写 `{"automaticDownloads": true}` 就不问直接安装。

试完删掉一次性的密钥。

## 代码结构

Go 里一个文件夹就是一个包（package），包之间不能循环引用，所以按职责拆分。`internal/` 下的包只能被本项目使用。

```
main.go              启动 MyGo、开主窗口，把系统功能（子窗口、对话框、下载目录、语言）交给 app
tray.go              托盘图标、确认用的消息框、退出
updates.go           更新窗口（MyGo 的 updater 插件，原生 UI）、“更新”那一行的状态、带“检查更新…”的 macOS 应用菜单
keychain.go          系统凭据存储（mygo.Secrets）：Platform 的 SecretGet/Set/Delete
locales/             界面文案：en.json、zh-Hans.json；catalog_gen.go 由 mygo 生成，不要手改
tools/natives/       CI 跑测试前先下载 libghostty-vt，最多重试五分钟（见“测试”）
tools/release/       发布用的工具：从 CHANGELOG.md 读出下一个版本号和更新说明，按提交列出草稿，收集第三方许可证（notices/ 是以二进制打包的库的许可证）
tools/screenshots/   从 internal/app 的测试重新生成 docs/screenshots/ 里的 README 截图
internal/
  config/            主机和文件夹（hosts.json）、设置的读写，背景图片的副本，配置目录；proxy.go 是代理设置和跳板机链（找循环）；tunnels.go 是隧道设置；keys.go 是退格键和删除键发送什么；sync.go 是“同步”页的设置、哪些设置要同步、sync-state.json 和同步前 hosts.json 的副本；history.go 是命令输入框的历史文件，以及读取 bash 和 zsh 的历史文件；quickcommands.go 是“命令”页的 quickcommands.json
  cloudsync/         通过用户自己的存储同步：crypto.go 加密文件（Argon2id、AES-256-GCM），merge.go 做三方合并，sync.go 读取、合并、写回（冲突时重试）；gist.go、webdav.go、s3.go 是各个服务（s3.go 手写 SigV4 签名），remotehttp.go 是它们共用的 HTTP 代码
  importer/          读其他客户端的连接：importer.go 是来源列表（readers）、读出的连接和问题、文本解码（UTF-8、UTF-16、GB18030）、判重、按文件夹导入；finalshell.go 找 FinalShell 的目录、读 conn 里的连接和文件夹；xshell.go 找 Xshell 的 Sessions 文件夹、读 .xsh 会话文件和 .xts 导出文件；securecrt.go 找 SecureCRT 的配置目录、读它 Sessions 文件夹里的 .ini 文件；mobaxterm.go 找 MobaXterm.ini、读里面的书签段，以及 .mxtsessions 和 .moba 导出文件；sshconfig.go 读 OpenSSH 的 ~/.ssh/config（Host 块和通配模式、Include、ProxyJump）；table.go 读 CSV 文件或 .xlsx 第一个工作表里的服务器列表，.xlsx 由自己写的 xlsx.go 读（archive/zip 和 encoding/xml，有防 zip 炸弹的限制）；testdata 是编造的样例
  sshx/              SSH 连接、认证、known_hosts、给终端用的 Conn；proxy.go 是经过 SOCKS5、HTTP CONNECT 和跳板机连接；forward.go 是端口转发（-L/-R/-D）和 SOCKS5 服务端
  termenc/           终端的连接在服务器的编码（GBK、Big5……）和终端读的 UTF-8 之间转换
  sshtest/           测试用的进程内 SSH 服务器：密码登录、空的 shell、端口转发，StartSFTP 还提供 SFTP（本机文件系统）
  monitor/           服务器状态采集和解析；net.go 是每个网卡的网速和历史，以及选中网卡的合计；netstat.go 是网络页的 socket（ss，或 /proc/net 和 /proc/<pid>/fd）、速度和行；trace.go 是通过 exec 通道的路由追踪（NextTrace 的 RAW 输出、traceroute、tracepath）
  geo/               IP 归属地：通过官方 Go 绑定读取 ip2region 数据（xdb.go），从 GitHub 或 jsDelivr 按校验和下载数据（download.go），ip-api.com 和 ip.sb 的在线查询（online.go）及其缓存（cache.go）；geotest/ 为测试生成小的 .xdb 文件
  remotefs/          SFTP 列目录、上传、下载（带进度和取消），重命名、新建、删除（ops.go）；textfile.go 是编辑器的读取（认出二进制文件）和安全保存；archive.go 是打包传输：打包和安全解包 tar.gz 数据流，在 exec 通道上运行 tar
  icons/             内嵌的 SVG 图标（Lucide，ISC 协议）
  fileicons/         VS Code 文件图标主题：读 VSIX 和扩展目录、按名字找图标、字体转换、安装和查找已装的主题；bundled/ 是内置的 Material 和 Seti，languages_gen.go 是 VS Code 的语言表，internal/gen 生成它们
  widgets/           可复用的小控件：图标按钮、用量条、折线图、表单行、左侧导航加页面（nav.go）
  app/               界面和状态
    app.go           App 状态、整体布局、打开/关闭会话
    session.go       一个会话：连接、终端、监控、SFTP；错误信息的翻译
    sidebar.go       左侧：监控面板、竖向会话列表
    conntree.go      左侧面板的“连接”页：两页之间的切换、连接管理器的树（只有一列）、记住展开的文件夹
    paneltab.go      面板行：会话的底部面板单独成列表里的一行，共用会话的连接（connSession）
    netifaces.go     网速曲线的网卡菜单：标签、选择网卡、按主机保存选择
    sysinfo.go       “系统信息”弹出框：读取、加载和错误状态、复制
    terminal.go      终端区域
    findbar.go       终端上的查找栏，驱动插件的 Search
    commandbar.go    终端下方的命令输入框：按键，路径、命令和历史弹出列表，词和字段
    commandbar_data.go 弹出列表读取的内容：通过 SFTP 读文件夹，通过 exec 通道（或 SFTP）读命令名，主机的历史，服务器的 shell 历史
    options_commandbar.go “终端 › 命令输入框”页
    fullscreen.go    全屏：只显示终端区域，窗口进入系统全屏
    start.go         新标签页：快速连接、搜索、清空最近连接
    splits.go        分隔条：各区域的最小尺寸，大小的保存和恢复
    files.go         文件面板：目录树、文件表格、拖入上传
    fileops.go       重命名、删除、新建文件夹和文件：对话框和目录树的右键菜单
    fileicons.go     文件面板的图标主题：后台读取、显示图标、安装和删除
    options_fileicons.go “文件管理 › 文件图标主题”页
    transfers.go     传输队列、“传输”页，和两处共用的传输行 transferRow
    archive.go       文件面板的打包传输：菜单项、传输行、“改用 SFTP 重试”
    downloads.go     “下载列表”弹出框、下载目录、重试和打开、应用图标上的数目和进度（Platform.ShowTransfers）、选项的“传输”页
    fileeditor.go    编辑远程文件：标签、读取、保存、冲突、关闭前询问、用默认程序打开
    fileeditor_view.go、fileeditor_text.go
                     编辑器窗口的界面（标签、工具栏、提示、状态栏、询问框），编码和换行符、按 #! 认语言
    tunnels.go       会话的隧道：连接后启动、断开时停止，“隧道”页
    network.go       “网络”页：监听 socket 和进程的表格，它们的连接标签
    network_trace.go 网络页的路由追踪：标签、没有 NextTrace 时的提示、输入地址的对话框
    quickcmds.go     “命令”页：分组、快捷命令按钮、通过命令输入框的 sendLine 发送、拖动、右键菜单
    quickcmds_editor.go 快捷命令编辑器窗口
    geo.go           IP 归属地：下载提示条、在后台加载数据、“归属地”列的文字、在线查询队列
    options_network.go 选项的“网络监控”页
    manager.go       连接管理器窗口：文件夹树、搜索、拖动、删除和恢复，a.deleteHostForever(h)；树的状态（managerState）和操作，“连接”页也用它们
    import.go        “导入连接”窗口：选择来源、查找或选择它的文件或目录、预览和勾选、导入后在连接管理器里显示、列表里的密码存进密钥库
    import_sources.go 导入窗口里每个来源的文案（importTexts），以及来源给出的原因和说明的文案
    import_table.go  导入窗口里服务器列表的部分：“保存模板…”按界面语言写的模板、列表特有错误的文案
    dialogs.go       密码、主机指纹、覆盖确认
    options.go       选项窗口：页面列表和“常规”页
    options_appearance.go “常规”页的“外观”：浅色、深色或跟随系统
    palette.go       应用的浅色和深色主题、主题之外的两套颜色，以及每个窗口用来按这一帧的外观选用它们的 useTheme
    options_updates.go “常规”页的“更新”一行：版本、检查、副本为什么不能更新
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
    sync.go          同步：主机和设置组成的数据，在密码队列上读取已保存的密码，由主窗口的帧驱动自动同步，应用同步带回的结果（同步期间的改动再合并一次）
    options_sync.go  选项里的“同步”页
    secrets.go       记住密码：凭据存储的键、后台读写队列、“记住密码”、密码框、彻底删除和复制连接时的密码
    closing.go       关闭主窗口、退出时做什么：隐藏到托盘、先确认
    windows.go       子窗口：按种类打开/聚焦/关闭，重绘所有窗口
    format.go        大小、速度、延迟的格式化
```

依赖方向是单向的：`app` 用其他所有包，`widgets` 只用 `icons`，其余包既不依赖界面，也互不依赖。

## 子窗口

和 FinalShell 一样，设置这类对话框是独立的窗口（MyGo 的子窗口，`WindowOptions.Parent`），不是主窗口里的浮层。目前有选项窗口、连接管理器、连接编辑器、“导入连接”窗口和文件编辑器，都是主窗口的子窗口，例外是从连接管理器打开的连接编辑器和导入窗口：它们是连接管理器的模态子窗口，和 FinalShell 一样。这样做的结果：

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

## 加一个导入来源

导入窗口列出 `internal/importer` 的来源，顺序是 `importer.go` 里 `readers` 的顺序。一个来源是一个 `Kind` 加一个 `Reader`：

```go
const Xshell Kind = "xshell"

var xshellReader = &Reader{
	Kind:   Xshell,
	Folder: true,             // 用户选目录；否则选文件，
	                          // 扩展名是 Extensions: []string{"csv"}（不写就是任意文件）
	Find:   FindXshell,       // 默认的文件或目录，没有就是 ""；根本没有默认位置就是 nil
	Read:   ReadXshell,       // func(path string) (*Source, error)
}
```

步骤：

1. `internal/importer/<来源>.go`：`Kind` 常量、`Reader`、它的 `Find` 和 `Read`，以及一段注释写清楚对这个格式知道什么、从哪里知道的。`Read` 返回的 `Source` 带着 `Kind` 和读的 `Path`；每个连接是一个 `Entry`：`Host`（没有 ID 和文件夹）、`Folder`（顶层是 nil）、`File`（某一行用 `At(file, line)`）、`KeyFile` 是来源写明的私钥文件、`NeedsKey` 是存在客户端自己密钥库里的私钥、`Jump` 是它的跳板机那一条、`Note` 是没有导入的设置、`Password` 只给用户自己写的列表用（存进密钥库，不会写进 `hosts.json`）。读不了的是 `Problem`（`NewProblem`、`ReadProblem`）；一个连接都没有就返回 `ErrNoConnections`。来源自己的 `Reason` 或 `Note` 是这个文件里的常量；故意跳过（不算失败）的原因在这个文件的 `init` 里加进 `skipped`：`func init() { skipped[Telnet] = true }`。`ReadText` 按文件实际的编码读文本文件，`sortSource` 按 FinalShell 那样排序。测试写在 `<来源>_test.go`，编造的样例放在 `testdata/<来源>/`。
2. 在 `importer.go` 的 `readers` 里加一行，位置就是窗口里列出的位置。
3. 在 `internal/app/import_sources.go` 的 `importTexts` 里加一项：列表里的名字、选择对话框的标题、文件类型的名字（有扩展名的文件来源）、找不到时说什么、表格上面的提示（加密密码的客户端用 `locales.ImportPasswordsNote(name)`）。新的 `Reason` 或 `Note` 在同一个文件的 `importProblemText` 或 `importNoteText` 里加文案。可选的 `errorText` 用应用自己的话说来源特有的错误（否则窗口显示“无法读取”和错误本身）；`template` 是“保存模板…”要写的文件，给用户自己写的来源用，比如服务器列表（`import_table.go`）。文案两种语言都要加（见[加一条文案](#加一条文案)）。

来源没有文案时 `TestImportSourcesListed` 会失败。加来源不用改 `import.go` 的其他地方。

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
