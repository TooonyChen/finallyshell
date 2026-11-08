# 配置文件

[English](../en/configuration.md) · 简体中文

Finallyshell 把主机和设置存在普通的 JSON 文件里。密码从不写进去，存在系统的凭据存储里。这一页说明这些文件，每项设置背后的选项见 [features.md](features.md)。

- [配置目录](#配置目录)
- [hosts.json](#hostsjson)
  - [隧道](#隧道)
  - [代理](#代理)
  - [文件夹、已删除的主机和其他字段](#文件夹已删除的主机和其他字段)
  - [安全](#安全)
- [settings.json](#settingsjson)
- [密码存在哪里](#密码存在哪里)

## 配置目录

所有文件都在 `<用户配置目录>/finallyshell/` 里，用户配置目录是 Go 的 `os.UserConfigDir()`：macOS 上是 `~/Library/Application Support`，Windows 上是 `%APPDATA%`，Linux 上是 `$XDG_CONFIG_HOME` 或 `~/.config`。设了环境变量 `FINALLYSHELL_DIR` 时，改用那个目录。

| 文件 | 内容 |
|---|---|
| `hosts.json` | 保存的主机和文件夹（见下面）。 |
| `hosts.v1.json` | 迁移第 1 版的 `hosts.json` 时保留的原文件。 |
| `hosts.json.broken-<日期-时间>` | 读不出来的 `hosts.json` 的副本。 |
| `settings.json` | 选项（见下面）。 |
| `known_hosts` | 信任过的主机指纹。 |
| `background-<时间>.<扩展名>` | 终端背景图片的副本。 |
| `icon-themes/` | 从 VSIX 文件安装的文件图标主题，存成 `<发布者>.<名称>.vsix`。 |

`mygo dev`、`go run` 和安装版用的是同一个目录，所以共用主机和设置。

编辑器下载下来、要用默认程序打开的文件不放在这里，而是放在缓存目录：`<缓存目录>/finallyshell/opened/<时间>/`。超过一天的文件夹在下次这样打开文件时删掉。

## hosts.json

`hosts.json` 从第 2 版起是一个对象，第 1 版是主机数组。第 1 版的文件在第一次读取时自动迁移：原文件另存为 `hosts.v1.json`，下次保存时改写成第 2 版。

```json
{
  "version": 2,
  "folders": [{"id": "f1", "name": "生产"}, {"id": "f2", "name": "数据库", "parent": "f1"}],
  "hosts": [{
    "id": "h1", "name": "web", "addr": "203.0.113.10", "port": 2222, "user": "root",
    "auth": "key", "keyPath": "~/.ssh/id_ed25519",
    "folder": "f2", "notes": "备注", "noMonitor": true,
    "termType": "vt220", "backspace": "ctrl-h", "noRememberPassword": true,
    "proxy": {"kind": "jump", "jumpHost": "h2"},
    "tunnels": [
      {"kind": "local", "listenPort": 15432, "targetHost": "db.internal", "targetPort": 5432},
      {"kind": "remote", "listenHost": "0.0.0.0", "listenPort": 8080, "targetHost": "127.0.0.1", "targetPort": 3000, "disabled": true},
      {"kind": "dynamic", "listenPort": 1080}
    ],
    "lastConnectedAt": "2026-11-07T10:00:00+08:00",
    "createdAt": "2026-10-01T09:00:00+08:00"
  }, {
    "id": "h2", "name": "bastion", "addr": "198.51.100.1", "user": "jump", "auth": "password",
    "proxy": {"kind": "socks5", "addr": "127.0.0.1", "port": 1080, "user": "alice"}
  }]
}
```

### 隧道

`tunnels` 是这台主机的端口转发：

- `kind` 为 `local`（`-L`）、`remote`（`-R`）或 `dynamic`（`-D`，没有 `targetHost`、`targetPort`）。
- 没有 `listenHost` 就是只监听 `127.0.0.1`。
- `disabled` 为 `true` 时，这条隧道不随会话启动。
- 隧道里不认识的字段也原样保留。

远程隧道的监听地址只能是 IP 地址或 `localhost`，见[隧道](features.md#tunnels-page)。

### 代理

`proxy` 是怎么连到这台主机：

- `kind` 为 `socks5` 或 `http`，这两种有 `addr`、`port`，代理需要认证时有 `user`；
- 或者 `kind` 为 `jump`，`jumpHost` 是跳板机的主机 ID。

没有 `proxy` 就是直接连接。代理密码也不写进文件，和主机密码一样存在凭据存储里（见[密码存在哪里](#密码存在哪里)）。

### 文件夹、已删除的主机和其他字段

- `folder` 是主机所在文件夹的 ID，空表示顶层。文件夹用 `parent` 嵌套。
- 有 `deletedAt`（删除时间）的主机是已删除的，只在连接管理器勾选“显示已删除”时列出。
- `createdAt` 是添加时间，快速连接按它排没连接过的主机。以前的版本保存的主机没有这个字段。
- `lastConnectedAt` 是上次连接的时间，供快速连接的“最近连接”列表使用。

空值的字段不写出来。没有某个字段时：

| 字段 | 没有时 | 有时 |
|---|---|---|
| `noMonitor` | 开启服务器监控。 | `true`：不监控，也不开额外的 exec 通道。 |
| `termType` | `xterm-256color` | 发给服务器的 `TERM`。 |
| `backspace` | 跟随全局设置（`termBackspace`）。 | `del`：退格键发送 `^?`；`ctrl-h`：发送 `^H`。 |
| `delete` | 跟随全局设置（`termDelete`）。 | `vt220`：删除键发送 `ESC [3~`；`del`：`^?`；`ctrl-h`：`^H`。 |
| `encoding` | 跟随全局设置（`termEncoding`）。 | `utf-8`、`gbk`、`gb18030`、`big5`、`shift_jis`、`euc-jp`、`euc-kr`、`iso-8859-1` 或 `windows-1252`；不认识的值按 UTF-8。 |
| `noRememberPassword` | 记住密码。 | `true`：关掉了“记住密码”。 |

### 安全

- 不认识的字段（新版本写的）原样保留，保存时不会丢。
- 写入时先写临时文件再改名，不会留下写了一半的文件。
- 如果 `hosts.json` 读不出来（比如被手改坏了），程序先把它原样复制成同目录下的 `hosts.json.broken-<年月日-时分秒>`，再按空列表启动。所以下次保存不会把原来的内容弄丢。

## settings.json

每个选项改了立刻保存在这里。只写和默认值不同的键，所以没有某个键就一定是默认值。

| 键 | 含义 | 默认值（没有这个键时） |
|---|---|---|
| `language` | 界面语言：`en` 或 `zh-Hans`。 | 跟随系统。 |
| `noAutoSelectTab` | `true` 时新会话在后台打开。 | 选中新会话。 |
| `minimizeToTray` | `true` 时关闭主窗口会把它隐藏到托盘。 | 关闭就退出。 |
| `noConfirmClose` | `true` 时有会话打开也不问，直接关闭、退出。 | 先问。 |
| `downloadDir` | 下载存到的文件夹。 | 系统的下载文件夹。 |
| `askDownloadDir` | `true` 时每次下载都问保存位置。 | 用 `downloadDir`。 |
| `copyOnSelect` | `true` 时把鼠标选中的文字复制到剪贴板。 | 关。 |
| `rightClick` | `paste`、`copy`、`copy-or-paste` 或 `ignore`。 | 右键菜单（不认识的值也是）。 |
| `noLinkClick` | `true` 时 Ctrl+单击（Cmd+单击）不打开链接。 | 打开链接。 |
| `wheelLines` | 鼠标滚轮每格滚动行数，1 到 20（超过 20 按 20）。 | 3 |
| `shortcuts` | 只存改过的快捷键，比如 `{"find": "Ctrl+Alt+F", "closeTab": "none"}`。 | 各平台的默认键。 |
| `termEncoding` | 选“默认”的主机用的服务器字符编码：`gbk`、`gb18030`、`big5`、`shift_jis`、`euc-jp`、`euc-kr`、`iso-8859-1` 或 `windows-1252`。 | `utf-8`，不转码（不认识的值也是）。 |
| `termBackspace` | `ctrl-h`：选“默认”的主机退格键发送 `^H`。 | `^?` |
| `termDelete` | `del` 或 `ctrl-h`：选“默认”的主机删除键发送什么。 | `ESC [3~` |
| `optionAsAlt` | `true` 时在 macOS 上把 Option 用作 Alt（Meta）键。 | 关。 |
| `termFontFamily` | 终端字体，逗号隔开的列表。 | JetBrains Mono, Cascadia Mono, Consolas, Menlo, DejaVu Sans Mono, monospace |
| `termFontSize` | 字号，6 到 48。 | 13.5 |
| `termLineHeight` | 行高，字体本身行高的倍数，0.8 到 2。 | 1 |
| `termFontWeight` | 粗细：300、400、500、600 或 700。 | 400 |
| `termNoLigatures` | `true` 时关掉连字。 | 开启连字。 |
| `termTheme` | Ghostty 配色的名称。 | Tomorrow Night |
| `termBackground` | 背景图片副本的文件名。 | 没有图片。 |
| `termBackgroundFit` | `contain` 或 `tile`。 | 铺满。 |
| `termBackgroundOpacity` | 图片的不透明度（百分比），5 到 100。 | 30 |
| `keepManagerOpen` | `true` 时连接后不关闭连接管理器。 | 关闭。 |
| `fileIconTheme` | `minimal`、`bundled:seti` 或 `<发布者>.<名称>/<主题 ID>`。 | Material Icon Theme |
| `sidebarWidth`、`treeWidth` | 左边栏和文件面板目录树的宽度，最后一次拖动的结果。 | 程序的默认值。 |
| `monitorPercent`、`terminalPercent` | 服务器监控占左边栏的比例、终端占主区域的比例，单位是百分比。 | 程序的默认值。 |

每个选项的详细说明在 [features.md](features.md)：见[常规](features.md#常规)、[传输](features.md#传输)、[终端](features.md#terminal-defaults)、[终端 › 鼠标](features.md#终端--鼠标)、[终端 › 快捷键](features.md#终端--快捷键)、[外观](features.md#外观字体配色背景图片)、[文件图标主题](features.md#文件图标主题)、[连接管理器](features.md#连接管理器)和[布局和分隔条](features.md#布局和分隔条)。

## 密码存在哪里

密码和私钥密码用 MyGo 的 `mygo.Secrets` 存在系统的凭据存储里，由系统为当前用户加密。服务名是 `finallyshell`，不是应用的 identifier：这样 `mygo dev`、`go run` 和安装版用的是同一份，和它们共用 `hosts.json` 一样。键是：

- `host/<主机 ID>/password`：密码；
- `host/<主机 ID>/passphrase`：私钥密码；
- `host/<主机 ID>/proxy-password`：这台主机的 SOCKS5 或 HTTP 代理的密码。

“记住密码”按主机设置，管这台主机的这几个密码。

| 系统 | 存在哪里 | 怎么查看 |
|---|---|---|
| macOS | 登录钥匙串里的通用密码，名称（服务）是 `finallyshell`，账户是键 | “钥匙串访问” |
| Windows | 凭据管理器里的普通凭据，目标是 `finallyshell/<键>` | 控制面板 → 凭据管理器 → Windows 凭据 |
| Linux | Secret Service（GNOME Keyring、KWallet、KeePassXC 等，通过 libsecret），属性 `service` 是 `finallyshell`，`account` 是键 | “密码和密钥”（Seahorse），或 `secret-tool lookup service finallyshell account host/<主机 ID>/password` |

密码是这样用的：

- 连接时先用这次运行里已经知道的密码，没有就读一次凭据存储，存储里也没有才弹出密码框。整条路上的密码都这样：主机的、它的代理的、每个跳板机的和跳板机的代理的。密码框里也有“记住密码”。主机自己的私钥密码在连接时读取，读不到就按没有密码的私钥连接。
- 服务器拒绝保存的密码时，会话显示“认证失败”，同时弹出密码框；私钥缺少私钥密码或私钥密码不对时，同样弹出输入私钥密码的框（带“记住私钥密码”）。跳板机拒绝它的密码、代理拒绝认证时，也弹出那一跳的密码框。输入后在同一个会话里重新连接。
- 编辑器里点“确定”立刻保存到凭据存储（这是用户明确保存的）。密码框里输入的密码先只放在内存里，登录成功后才保存，替换原来的；输错的不会保存，原来保存的也保留，直到有一个能登录的替换它。
- 凭据存储报的其他错误（比如用户拒绝访问、取消解锁）按“没有保存”处理，同一个错误只在日志里记一次。
- 关掉“记住密码”会把密码从凭据存储里删掉，只留在内存里，退出程序后需要重新输入。这个选择按主机保存（`hosts.json` 的 `noRememberPassword`），以后密码框也默认不勾。
- 系统没有凭据存储时（Linux 上没有 libsecret、没有会话 D-Bus，或者 D-Bus 上没有 Secret Service，服务器和精简桌面上常见），密码只存在内存里：编辑器和密码框里的“记住密码”变灰，下面写明原因。
- 读写凭据存储时系统可能会询问用户：解锁钥匙串或密钥环；macOS 上重新编译过的程序第一次读以前存的条目时，会问是否允许。这些调用可能要等很久，所以程序从不在 UI 线程上调用它们（见 `internal/app/secrets.go` 开头的说明），而是放在一个后台 goroutine 里按顺序执行，结果通过 `App.post` 交回 UI 线程。
