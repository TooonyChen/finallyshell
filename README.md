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

- 中文和英文界面。默认跟随系统语言，左下角“会话”标题旁的语言按钮可以切换，选择会保存。
- 连接管理器：新建、编辑、删除主机，密码或私钥登录。主机列表存在 `<用户配置目录>/finalshelly/hosts.json`，密码不落盘，只存在内存里。
- 第一次连接时弹窗显示主机指纹，信任后记入 `finalshelly/known_hosts`；指纹变化时拒绝连接。
- 左上：当前会话服务器的 IP、运行时间、负载、CPU/内存/交换、内存占用前几的进程、网速和延迟曲线、磁盘。每 2 秒通过 SSH 读取一次 `/proc`，只支持 Linux 服务器。
- 左下：竖向会话列表，状态点（绿：已连接，黄：连接中，红：失败），右键菜单（重新连接、复制会话、断开、关闭），拖动排序。
- 右上：终端。断开后保留最后的画面，顶部出现“重新连接”。
- 右下“文件”页：SFTP 浏览，左边目录树，右边文件表格（可多选），路径栏可以直接输入路径。
- 上传：把文件或文件夹从资源管理器 / Finder 拖到文件表格（传到当前目录）或目录树的某个文件夹上；也可以用工具栏的上传按钮或右键菜单选文件、选文件夹。
- 下载：选中文件或文件夹，点下载按钮（存到系统的“下载”文件夹），或右键“下载到…”自己选位置。
- 右下“传输”页：进度、速度、取消；同时最多跑 3 个，其余排队。目标位置已有同名项目时先询问是否覆盖。下载完成的条目可以直接拖到桌面或其他程序，也可以“在文件夹中显示”。

关于把远程文件直接拖到桌面：MyGo 目前不支持“拖放时才生成文件”的虚拟文件拖拽，所以只能先下载，再从传输列表里拖出去。

快捷键：Ctrl+Shift+T 连接管理器，Ctrl+Tab / Ctrl+Shift+Tab 切换会话，Ctrl+Shift+W 关闭会话。终端内复制粘贴是 Ctrl+Shift+C / Ctrl+Shift+V（macOS 是 Cmd+C / Cmd+V）。

## 代码结构

Go 里一个文件夹就是一个包（package），包之间不能循环引用，所以按职责拆分。`internal/` 下的包只能被本项目使用。

```
main.go              启动 MyGo、开窗口，把系统功能（对话框、下载目录、语言）交给 app
locales/             界面文案：en.json、zh-Hans.json；catalog_gen.go 由 mygo 生成，不要手改
internal/
  config/            主机列表和设置的读写、配置目录
  sshx/              SSH 连接、认证、known_hosts、给终端用的 Conn
  monitor/           服务器状态采集和解析
  remotefs/          SFTP 列目录、上传、下载（带进度和取消）
  icons/             内嵌的 SVG 图标（Lucide，ISC 协议）
  widgets/           可复用的小控件：图标按钮、用量条、折线图、表单行
  app/               界面和状态
    app.go           App 状态、整体布局、快捷键、打开/关闭会话
    session.go       一个会话：连接、终端、监控、SFTP；错误信息的翻译
    sidebar.go       左侧：监控面板、竖向会话列表、语言菜单
    terminal.go      终端区域
    files.go         文件面板：目录树、文件表格、拖入上传
    transfers.go     传输队列和“传输”页
    dialogs.go       连接管理器、密码、主机指纹、覆盖确认
    format.go        大小、速度、延迟的格式化
```

依赖方向是单向的：`app` 用其他所有包，`widgets` 只用 `icons`，其余包互不依赖界面。

### 加一条文案

1. 在 `locales/en.json` 里加 key 和英文（带参数的写在 `@key.placeholders` 里），在 `locales/zh-Hans.json` 里加中文。
2. 运行 `go tool mygo generate`（`mygo dev` 会自动做），代码里用 `locales.Key(...)`。

格式是 ICU MessageFormat，见 MyGo 的 [i18n 文档](https://github.com/egoist/mygo/blob/main/docs/i18n.md)。
