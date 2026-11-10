<h1 align="center">
  <img src="resources/icon.png" width="64" height="64" alt="" align="center">&nbsp;Finallyshell
</h1>

<p align="center">
  布局和 FinalShell 一样、会话标签竖着排的 SSH 客户端，用 Go 编写。
</p>

<p align="center">
  <a href="https://github.com/TooonyChen/finallyshell/releases/latest"><img src="https://img.shields.io/github/v/release/TooonyChen/finallyshell?label=release" alt="最新版本"></a>
  <a href="https://github.com/TooonyChen/finallyshell/actions/workflows/ci.yml"><img src="https://github.com/TooonyChen/finallyshell/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey" alt="macOS、Windows、Linux">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-PolyForm%20Noncommercial-blue" alt="许可证"></a>
</p>

<p align="center">
  <a href="README.md">English</a> · <b>简体中文</b>
  <br>
  <a href="#安装">安装</a> · <a href="#功能">功能</a> · <a href="#快捷键">快捷键</a> · <a href="docs/zh-CN/features.md">完整文档</a>
</p>

<p align="center">
  <img src="docs/screenshots/main-zh-CN.png" alt="Finallyshell 主窗口：服务器监控、会话列表、终端和文件面板">
</p>

用过 FinalShell 的话，打开就会用：左上是服务器状态，下面是会话，右边是终端，终端下面是服务器上的文件。Finallyshell 保留了这个布局，把会话标签放进左下角的竖向列表，可以滚动，也可以拖动排序。

它是用 Go 和 [MyGo](https://github.com/egoist/mygo) 界面库写的原生程序，不需要装 Java。终端用的是 [Ghostty](https://ghostty.org) 的终端引擎 libghostty-vt。密码存在系统的钥匙串里，不写进文件。

## 安装

在 [Releases](https://github.com/TooonyChen/finallyshell/releases/latest) 下载最新版本：

| 系统 | 文件 |
|---|---|
| macOS（Apple 芯片） | `finallyshell-<版本>-arm64.dmg` |
| macOS（Intel 芯片） | `finallyshell-<版本>-amd64.dmg` |
| Windows（x64） | 安装程序 `finallyshell-setup-<版本>-amd64.exe`，或免安装的 `.zip` |
| Debian、Ubuntu（x64） | `finallyshell_<版本>_amd64.deb` |
| 其他 Linux（x64） | `finallyshell-<版本>-linux-amd64.tar.gz`，运行 `sh install.sh` 装到 `~/.local` |
| Debian、Ubuntu（ARM64） | `finallyshell_<版本>_arm64.deb` |
| 其他 Linux（ARM64） | `finallyshell-<版本>-linux-arm64.tar.gz`，运行 `sh install.sh` 装到 `~/.local` |

macOS 版还没有经过 Apple 公证，第一次打开会被拦下。拖进“应用程序”后运行：

```bash
xattr -dr com.apple.quarantine /Applications/Finallyshell.app
```

或者在“系统设置 › 隐私与安全性”里点“仍要打开”。Windows 的 SmartScreen 可能会提示安装程序没有签名，点“更多信息”，再点“仍要运行”。

Linux 上托盘图标需要 `libayatana-appindicator3`，记住密码需要 Secret Service 密钥环（GNOME Keyring、KWallet、KeePassXC 等）。没有的话程序照样能用，只是密码只留在内存里。

之后 Finallyshell 会自行更新；`.deb` 除外，装新的 `.deb` 就是更新。0.2.0 及更早的版本不能自行更新：手动安装一次新版本。

## 功能

### 连接

macOS 上按 <kbd>Cmd</kbd>+<kbd>O</kbd>，其他系统按 <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>O</kbd> 打开连接管理器。和 FinalShell 一样，主机按文件夹组织成树：可以拖到别的文件夹，按名称、地址、用户名、备注搜索，复制连接；删除的连接先进“显示已删除”，还能恢复。

同一棵树也一直留在主窗口里：服务器状态下面的面板可以在会话和“连接”之间切换，在“连接”页双击就打开会话，已经打开的主机后面有绿点。<kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>E</kbd>（<kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>E</kbd>）在两页之间切换。

<p align="center">
  <img src="docs/screenshots/connections-zh-CN.png" width="560" alt="主窗口左侧面板的“连接”页：按文件夹排列的主机，打开了会话的主机后面有一个点">
</p>

<p align="center">
  <img src="docs/screenshots/connection-manager-zh-CN.png" width="720" alt="连接管理器，主机按文件夹排列">
</p>

每个主机可以直接连接，也可以经过 SOCKS5、HTTP 代理或跳板机（相当于 `ssh -J`）连接，跳板机自己也可以再走代理或跳板机。端口转发支持本地（`-L`）、远程（`-R`）和动态 SOCKS5（`-D`）隧道，随会话启动，在“隧道”页显示状态、连接数和流量。

<table>
  <tr>
    <td><img src="docs/screenshots/connection-proxy-zh-CN.png" alt="经过跳板机连接"></td>
    <td><img src="docs/screenshots/connection-tunnels-zh-CN.png" alt="本地、远程和动态隧道"></td>
  </tr>
</table>

每个新标签页都是快速连接：先列最近连接过的主机，再列其他主机，新加的在上面，可以搜索。从别的客户端搬过来的话，连接管理器里的“导入”能读出 FinalShell、Xshell、SecureCRT、MobaXterm 的连接和文件夹，`~/.ssh/config` 里的主机，或者 CSV、Excel 文件里的服务器列表。别的客户端加密存着的密码不导入，第一次连接时会问一次；服务器列表里的密码存进系统的密钥库。

<table>
  <tr>
    <td><img src="docs/screenshots/quick-connect-zh-CN.png" alt="新标签页上的快速连接"></td>
    <td><img src="docs/screenshots/import-finalshell-zh-CN.png" alt="从 FinalShell 导入连接"></td>
  </tr>
</table>

密码和私钥密码存在 macOS 钥匙串、Windows 凭据管理器或 Linux 的 Secret Service 里，从不写进 `hosts.json`。第一次连接时确认主机指纹，以后每次连接都会核对。

### 服务器监控

会话连上后，左上角显示服务器的运行时间、负载、CPU、内存和交换、内存占用最多的进程、网速、延迟和磁盘，每两秒刷新一次。“系统信息”列出系统、内核、CPU 型号、内存、IP 地址和硬盘，可以一键复制成文本。“进程”页列出所有进程，可排序、可筛选，打开一个进程能看它的命令行、工作目录和环境变量，确认后发送信号或结束它。“网络”页列出进程监听的端口、谁连着它们、各自的流量，同意下载 ip2region 位置数据后显示对端的归属地，还能在服务器上用 NextTrace 追踪路由。监控读的是 `/proc`，所以只支持 Linux 服务器。

<p align="center">
  <img src="docs/screenshots/system-info-zh-CN.png" width="560" alt="系统信息弹出框">
</p>

### 文件

终端下面的文件面板通过 SFTP 浏览服务器，在终端里 `cd` 时会跟着进入同一个目录。在文件或左边目录树的文件夹上右键，可以重命名、删除、新建文件和文件夹。把文件或文件夹从访达或资源管理器拖进来就是上传；下载可以存到“下载”文件夹，也可以自己选位置。传输同时跑三个，显示进度、速度和剩余时间，可以取消、重试；大量小文件的文件夹还可以打包传输，用 tar 和 gzip 边打包边传；Dock 图标或任务栏按钮上也显示数目和总进度。

<p align="center">
  <img src="docs/screenshots/transfers-zh-CN.png" width="560" alt="下载列表里正在进行的传输">
</p>

双击文本文件直接在服务器上编辑。编辑器支持 30 多种语言的语法高亮、查找替换、跳转到行、多光标和折叠。服务器允许时原子保存；打开之后服务器上的文件被改过的话，保存前会提醒；换行符和编码保持原样，GBK、GB18030 也可以。

<p align="center">
  <img src="docs/screenshots/file-editor-zh-CN.png" width="720" alt="编辑服务器上的脚本">
</p>

文件和文件夹的图标来自 VS Code 的图标主题。内置 Material Icon Theme 和 Seti，自动找到 VS Code、VSCodium、Cursor、Windsurf 里已经装的主题，其他主题可以从 `.vsix` 文件安装。

<p align="center">
  <img src="docs/screenshots/file-icon-themes-zh-CN.png" width="720" alt="选择文件图标主题">
</p>

### 终端

可以选字体、字号、行高、粗细，开关连字，从 Ghostty 自带的几百个配色里挑一个，还可以加背景图片，改了立刻应用到所有打开的终端。另外还有查找（<kbd>Cmd</kbd>+<kbd>F</kbd> / <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>F</kbd>）、全屏、选中即复制、自定义右键行为，<kbd>Cmd</kbd>/<kbd>Ctrl</kbd>+单击打开链接。

会话的终端可以分屏，和 Ghostty、Windows Terminal 一样：<kbd>Cmd</kbd>+<kbd>D</kbd>（<kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>=</kbd>）在获得焦点的分屏旁边再开一个同一台服务器上的 shell，<kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>D</kbd>（<kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>-</kbd>）在它下面开一个，用的是同一个连接，不用重新登录。点一下分屏，或用 <kbd>Cmd</kbd>+<kbd>Option</kbd>+方向键（<kbd>Alt</kbd>+方向键）在分屏之间移动，拖动分隔条调整大小，放大一个分屏占满终端，用 <kbd>Cmd</kbd>+<kbd>W</kbd>（<kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>W</kbd>）关闭它。命令输入框和“命令”页发给获得焦点的分屏，文件面板跟随它的目录。

终端下面是 FinalShell 的命令输入框：输入命令，回车发给 shell。<kbd>Tab</kbd> 分多列列出正在输入的路径所在的服务器文件夹，在第一个词上则列出服务器 PATH 里的命令；<kbd>↑</kbd>、<kbd>Alt</kbd>+<kbd>H</kbd> 或“历史”按钮列出以前发给这台主机的命令，以及它的 shell 历史里的命令，<kbd>←</kbd> <kbd>→</kbd> 可以从一条命令里只取一个词。用不上的话，在“选项 › 终端 › 命令输入框”里关掉它，或者只关掉其中一部分。

常用的命令可以存成“命令”页里的按钮，按你自己的分组放好：点一下就发给会话，多行命令逐行执行，不带回车保存的命令会停在提示符后面等你检查。按钮可以拖动排序，这一页也可以在单独的标签中打开，放下几十个命令。不值得保存的几行命令，可以写在按钮旁边的命令编辑器里，用 <kbd>Cmd</kbd>/<kbd>Ctrl</kbd>+<kbd>Enter</kbd> 发送全部或只发送选中的几行，文本重启后仍在。

<p align="center">
  <img src="docs/screenshots/quick-commands-zh-CN.png" width="720" alt="分组的快捷命令按钮和命令编辑器">
</p>

locale 不是 UTF-8 的服务器也能用：终端可以转换 GBK、GB18030、Big5、Shift_JIS、EUC-JP、EUC-KR、ISO-8859-1 和 Windows-1252，在“选项”里统一设置，也可以按主机单独设置。退格键和删除键发送什么也一样可以改。

<table>
  <tr>
    <td><img src="docs/screenshots/terminal-colors-zh-CN.png" alt="配色和实时预览"></td>
    <td><img src="docs/screenshots/terminal-background-zh-CN.png" alt="终端的背景图片"></td>
  </tr>
</table>

### 其他

- 想在自己电脑上开个 shell？“+”旁边的终端按钮打开一个本地终端标签，终端、命令输入框和快捷命令都和会话一样；运行什么程序（WSL、PowerShell、fish 等）在“选项 › 终端”里选。
- 在运行 Claude Code、Codex、Gemini CLI 等编程智能体？运行期间会话标签上会显示它的图标，连到同一台服务器的几个标签一眼就能分清。
- 中文和英文界面，默认跟随系统，也可以在“选项”里切换。
- 浅色和深色外观，默认跟随系统，也可以在“选项”里切换。
- 所有快捷键都能改。shell 要用的键（比如 <kbd>Ctrl</kbd>+<kbd>C</kbd>、<kbd>Ctrl</kbd>+<kbd>R</kbd>）不让占用，并说明原因。
- 关闭时可以最小化到托盘（macOS 是菜单栏），有会话连着时关闭前先确认。
- 同时只运行一个：再打开一次会把已经运行的窗口提到前面。
- 自行更新：每天检查一次，也可以手动检查，原地安装签过名的更新。
- 拖动面板之间的分隔条调整大小，下次启动还是这个大小。
- 可选的多台电脑同步：连接和设置通过你自己的 GitHub gist、WebDAV 文件夹（坚果云、Nextcloud 等）或 S3 兼容存储（Cloudflare R2、Backblaze B2 等）同步，在本机用只有你知道的密码加密。不用注册账号，没有服务器，不用付费。

每个功能的细节见 [docs/zh-CN/features.md](docs/zh-CN/features.md)，程序保存的文件见 [docs/zh-CN/configuration.md](docs/zh-CN/configuration.md)。

## 快捷键

默认和 FinalShell 一样，分屏的在 macOS 上和 Ghostty 一样、其他系统上和 Windows Terminal 一样，可以在“选项 › 终端 › 快捷键”里修改。

| 操作 | macOS | Windows / Linux |
|---|---|---|
| 复制 | <kbd>Cmd</kbd>+<kbd>C</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>C</kbd> |
| 粘贴 | <kbd>Cmd</kbd>+<kbd>V</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>V</kbd> |
| 在终端里查找 | <kbd>Cmd</kbd>+<kbd>F</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>F</kbd> |
| 全屏 | <kbd>Cmd</kbd>+<kbd>Enter</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>Enter</kbd> |
| 重新连接断开的会话 | <kbd>Enter</kbd> | <kbd>Enter</kbd> |
| 上一个标签 | <kbd>Cmd</kbd>+<kbd>[</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>Tab</kbd> |
| 下一个标签 | <kbd>Cmd</kbd>+<kbd>]</kbd> | <kbd>Ctrl</kbd>+<kbd>Tab</kbd> |
| 关闭获得焦点的分屏，只有一个分屏时关闭标签 | <kbd>Cmd</kbd>+<kbd>W</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>W</kbd> |
| 连接管理器 | <kbd>Cmd</kbd>+<kbd>O</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>O</kbd> |
| 新标签页 | <kbd>Cmd</kbd>+<kbd>T</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>T</kbd> |
| 切换到第 1 到 9 个标签 | <kbd>Cmd</kbd>+<kbd>1</kbd>…<kbd>9</kbd> | <kbd>Alt</kbd>+<kbd>1</kbd>…<kbd>9</kbd> |
| 命令输入框，再按回到终端 | <kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>L</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>L</kbd> |
| 左侧面板切换会话/连接 | <kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>E</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>E</kbd> |
| 显示或隐藏底部面板 | <kbd>Cmd</kbd>+<kbd>J</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>J</kbd> |
| 向右分屏 | <kbd>Cmd</kbd>+<kbd>D</kbd> | <kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>=</kbd> |
| 向下分屏 | <kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>D</kbd> | <kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>-</kbd> |
| 放大获得焦点的分屏，或还原 | <kbd>Cmd</kbd>+<kbd>Shift</kbd>+<kbd>Enter</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>X</kbd> |
| 焦点移到那一边的分屏 | <kbd>Cmd</kbd>+<kbd>Option</kbd>+方向键 | <kbd>Alt</kbd>+方向键 |

<p align="center">
  <img src="docs/screenshots/shortcuts-zh-CN.png" width="720" alt="选项里的快捷键页">
</p>

## 常见问题

**macOS 提示应用已损坏、无法打开。** 还没有公证，运行[安装](#安装)里的 `xattr` 命令。

**主机和密码存在哪里？** 主机和设置是用户配置目录里的 JSON 文件（macOS 是 `~/Library/Application Support/finallyshell`，Windows 是 `%AppData%\finallyshell`，Linux 是 `~/.config/finallyshell`）。密码在系统钥匙串里，服务名是 `finallyshell`。详见 [docs/zh-CN/configuration.md](docs/zh-CN/configuration.md)。

**监控面板一直是空的。** 监控读 `/proc`，只支持 Linux 服务器。有些跳板机不允许它额外开的通道，可以在这个主机的设置里关掉“启用 exec 通道（服务器监控）”。

**支持 Telnet、RDP 或串口吗？** 不支持，只有 SSH 和 SFTP。

## 从源码构建

需要 Go 1.27.3 或更新的版本。

```bash
git clone https://github.com/TooonyChen/finallyshell.git
```

```bash
cd finallyshell && go tool mygo dev
```

`go tool mygo dev` 运行程序，改了代码自动重启；`go tool mygo build` 打包到 `build/<平台>/`。第一次运行会把 libghostty-vt 下载到用户缓存目录。测试、发布和代码结构见 [docs/zh-CN/development.md](docs/zh-CN/development.md)。

## 参与贡献

欢迎提 issue 和 pull request。每个改动先开一个 issue，写清楚打算怎么做；细节和贡献条款见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 致谢

感谢：

- [FinalShell](https://www.hostbuf.com/)，从刚上大学到毕业工作，一路都是它陪着我过来的，这个项目也是受它启发做的。
- [V2EX](https://www.v2ex.com/) 社区。
- [LINUX DO](https://linux.do/) 社区。

## 许可证

Finallyshell 公开源码，但不是 OSI 定义的开源软件。你可以在下面两个许可证里任选一个（全文见 [LICENSE](LICENSE)）：

- [PolyForm Noncommercial 1.0.0](https://polyformproject.org/licenses/noncommercial/1.0.0)：任何非商业用途，包括 fork、修改和分享。
- [PolyForm Internal Use 1.0.0](https://polyformproject.org/licenses/internal-use/1.0.0)：你和你的公司内部使用，可以修改，但不能对外分发。在公司上班时用它连服务器属于这一种。

出售本软件、把它放进收费产品、以商业目的分发修改版等，需要另外取得商业授权，请开一个 issue 联系。
