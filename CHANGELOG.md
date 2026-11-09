# Changelog

Each version's notes, newest first, in English and Simplified Chinese. A release's notes on GitHub and in the app's update window are its section here; see [Releasing](docs/en/development.md#releasing).

## 0.3.0

### English

- New Commands tab in the bottom panel, like FinalShell's quick commands: keep the commands you use often as buttons, sorted into groups, and a click sends one to the current session. A command can have several lines, which run one after another; turn off "Press Enter after each line" to leave the last line at the prompt for you to check. "New command" opens the editor. Search by name or command, drag a button to reorder it or onto a group to move it there, and right-click it to edit, duplicate or delete it, or to put it in the command bar. The commands are the same for every host.
- New Connections page in the bottom-left panel: the switch "Sessions (N) | Connections" at its top shows the connection manager's folders and connections in the main window, next to the terminal. Double-click a connection, or select it and press Enter, to open a session; search by name, address, user or notes; right-click or drag to edit, rename, move or delete, as in the connection manager. A dot after a name shows that it has a session open. Ctrl+Shift+E (Cmd+Shift+E on macOS) switches between the sessions and the connections.
- A session tab shows the logo of the coding agent running in its shell, such as Claude Code, Codex or Gemini CLI, so several tabs to the same server are easy to tell apart; hover over the logo for the agent's name. It works on Linux servers, as it reads `/proc`, and doesn't see an agent inside `tmux` or `screen`. It is on by default; turn it off with "Show the coding agent running in each tab" in Options › Terminal.
- The IP location data (ip2region, IPv4) downloads from jsDelivr when GitHub can't be reached, as often happens in mainland China. When both fail, the bar names each host and what it answered.
- Trace Route with NextTrace shows each hop as it arrives, instead of all of them when the trace ends.
- The dividers of the Processes and Network tabs keep their positions after a restart, as the window's other dividers do.

### 简体中文

- 底部面板新增“命令”页，相当于 FinalShell 的快捷命令：把常用的命令存成按钮，按分组整理，点一下就发给当前会话。命令可以有多行，逐行执行；关掉“每行发送后按回车”时，最后一行停在提示符后面，等你检查后自己按回车。“新建命令”打开编辑器。可以按名称或命令内容搜索，拖动按钮调整顺序，拖到分组上就移进那个分组；右键按钮可以编辑、复制、删除，或者放进命令输入框。所有主机共用同一份命令。
- 左下面板新增“连接”页：顶部的切换按钮“会话 (N) | 连接”在主窗口里、终端旁边显示连接管理器的文件夹和连接。双击连接，或选中后按 Enter，打开会话；可以按名称、地址、用户名或备注搜索；右键菜单和拖动可以像在连接管理器里一样编辑、重命名、移动和删除。开着会话的连接名称后面有一个点。Ctrl+Shift+E（macOS 上是 Cmd+Shift+E）在会话和连接之间切换。
- 会话标签上显示它的 shell 里正在运行的编程智能体的图标，例如 Claude Code、Codex、Gemini CLI，连到同一台服务器的几个标签一眼就能分清；鼠标悬停在图标上显示智能体的名称。它从 `/proc` 读取，所以支持 Linux 服务器，看不到 `tmux` 或 `screen` 里运行的智能体。默认打开，可以在“选项 › 终端”里关掉“在标签上显示其中运行的编程智能体”。
- IP 归属地数据（ip2region，IPv4）在 GitHub 连不上时改从 jsDelivr 下载，中国大陆常常是这种情况。两处都失败时，提示条列出每个主机和它的回应。
- 用 NextTrace 路由追踪时，每一跳随到随显示，不用等追踪结束才一起出来。
- “进程”和“网络”页的分隔条位置在重启后保留，和窗口里其他分隔条一样。

## 0.2.2

### English

- Command bar under the terminal, as in FinalShell: type a command and Enter sends it to the shell. Tab lists the server's folder for the path you are typing, or the commands on its PATH for the first word. Up, Alt+H or the History button lists the commands you sent to that host before and those in its shell history, and Left and Right pick one word out of a command. Ctrl+Shift+L (Cmd+Shift+L on macOS) moves the keyboard to the bar and back. Turn it, or any part of it, off in Options › Terminal › Command bar.
- The file panel follows the shell: after `cd` in the terminal it opens the same folder. The toggle after the home button in the panel's toolbar turns this off. Shells that report their folder (OSC 7) work anywhere; for the others the folder is read from `/proc`, so on Linux servers.
- New Processes tab at the bottom right: every process of the server, sortable and filterable, refreshed every 2 seconds. Open one to see its command line, executable, working directory and environment, and send it a signal or kill it after confirming. A click on the side panel's process list opens it.
- New Network tab: the ports processes listen on, who is connected to them and how much each uploads and downloads. Agree to download ip2region's location data (about 11 MB) to see where the peers are; online lookups are off unless you turn them on in Options › Network monitor. "Trace Route" traces the route from the server with NextTrace, or `traceroute`/`tracepath` when NextTrace is not installed.
- "Open in its own tab", at the right end of the bottom panel's tabs, shows the bottom panel in a row of its own under the session, using the whole right side, for the tall tables of Processes and Network. It is the same session, not a second connection.
- Rename a session tab for this run with "Rename" in its context menu or F2, to tell apart several tabs open to the same server. The name is not saved, and "Reset name" goes back to the host's name.
- "Download as archive" and "Upload as archive…" in the file panel's context menus send a folder of many small files packed with tar and gzip as it streams, much faster than one file at a time over SFTP. When the server has no `tar`, "Retry over SFTP" sends it the usual way.
- Sync connections and settings between computers through storage of your own: a GitHub gist, a WebDAV folder (Jianguoyun, Nextcloud...) or S3-compatible storage (Cloudflare R2, Backblaze B2...). Everything is encrypted on your computer with a sync password only you know, and saved passwords are synced only if you choose to. It is off until you set it up in Options › Sync.
- The network speed graph's interface name opens a menu to choose which interfaces it shows, one or several summed; the choice is kept per host. By default it shows the busiest one.
- macOS on Intel: there is now an `amd64` build for Intel Macs.
- The empty start page, shown before any connection is added, has a "Connection manager" button, to import connections from another client first.

### 简体中文

- 终端下面多了命令输入框，和 FinalShell 一样：输入命令，回车发给 shell。Tab 列出正在输入的路径所在的服务器文件夹，在第一个词上则列出服务器 PATH 里的命令。↑、Alt+H 或“历史”按钮列出以前发给这台主机的命令和它的 shell 历史，← → 可以从一条命令里只取一个词。Ctrl+Shift+L（macOS 上是 Cmd+Shift+L）在输入框和终端之间切换键盘焦点。用不上的话，在“选项 › 终端 › 命令输入框”里关掉它，或者只关掉其中一部分。
- 文件面板跟随终端目录：在终端里 `cd` 之后，文件面板也进入同一个文件夹。文件面板工具栏里主目录按钮后面的开关可以关掉它。会报告当前目录（OSC 7）的 shell 在哪都能用；其他 shell 要从 `/proc` 读目录，所以只支持 Linux 服务器。
- 右下角新增“进程”页：列出服务器的所有进程，可排序、可筛选，每 2 秒刷新。打开一个进程能看它的命令行、可执行文件、工作目录和环境变量，确认后可以发送信号或结束它。点左侧面板的进程列表也会打开这一页。
- 新增“网络”页：进程监听的端口、谁连着它们、各自上传下载多少。同意下载 ip2region 位置数据（约 11 MB）后显示对端的归属地；在线查询默认关闭，要在“选项 › 网络监控”里打开。“路由追踪”在服务器上用 NextTrace 追踪路由，没装 NextTrace 时改用 `traceroute` 或 `tracepath`。
- 底部面板标签栏右端的“在单独的标签中打开”，会在会话下面多出一行，用整个右侧显示底部面板，方便看“进程”“网络”这类需要高度的表格。它还是同一个会话，不是新的连接。
- 会话标签可以临时改名：右键菜单里的“重命名”或按 F2，方便区分连到同一台服务器的多个标签。名字不会保存，“恢复原名”可以改回主机名。
- 文件面板右键菜单里的“打包下载”和“打包上传…”，用 tar 和 gzip 边打包边传，传大量小文件的文件夹比 SFTP 逐个传快得多。服务器上没有 `tar` 时，可以“改用 SFTP 重试”。
- 连接和设置可以在多台电脑之间同步，用的是你自己的存储：GitHub gist、WebDAV 文件夹（坚果云、Nextcloud 等）或 S3 兼容存储（Cloudflare R2、Backblaze B2 等）。所有内容在本机用只有你知道的同步密码加密，已保存的密码只有你勾选了才会同步。在“选项 › 同步”里设置之前不会启用。
- 网速图右上角的网卡名可以打开菜单，选择显示哪块网卡，也可以多选求和；每台主机分别记住。默认显示流量最大的那块。
- macOS Intel：新增给 Intel 芯片 Mac 用的 `amd64` 版本。
- 还没添加任何连接时，空白的起始页多了“连接管理器”按钮，可以先从其他客户端导入连接。

## 0.2.1

### English

- Finallyshell now checks for new versions and updates itself in place. It checks automatically, which you can turn off in Options › General, and "Check for Updates…" is in Options › General, in the tray icon's menu and, on macOS, in the app menu. The `.deb` package is still updated by installing the new `.deb`.
- Version 0.2.0 cannot update itself, so install 0.2.1 by hand this once. Later versions update themselves.
- Dark mode: choose System, Light or Dark in Options › General › Appearance. System, the default, follows your computer, title bars included. The terminal keeps its own color scheme.
- The "Import connections" window has an "Import from" choice, and besides FinalShell it now reads:
  - Xshell sessions, with their folders. On Windows its Sessions folder is found by itself; to import an `.xts` export, choose the folder that holds it.
  - SecureCRT sessions, with their folders. Its config folder is found where SecureCRT keeps it by default; if you moved it, choose the Config folder, its Sessions folder or any folder inside that.
  - MobaXterm sessions, with their folders, from `MobaXterm.ini` or from an `.mxtsessions` or `.moba` export.
  - OpenSSH's `~/.ssh/config`: each `Host` alias becomes a connection with its `HostName`, `Port`, `User` (your local user name when it has none) and first `IdentityFile`. `Include` is followed, globs included, `Host *` and other patterns apply as they do in `ssh`, and `Match` blocks are skipped. A `ProxyJump` to another host of the file becomes its jump host when that host is imported too or is already in Finallyshell.
  - A server list in a CSV, TSV, text or Excel (`.xlsx`) file, with column headers in English or Chinese. "Save template…" writes a `servers.csv` to fill in. Passwords in the list go to the system keychain, never to `hosts.json`. Old `.xls` workbooks are not read: save them as `.xlsx` or CSV.
- Xshell, SecureCRT and MobaXterm encrypt the passwords they keep, so passwords are not imported: you are asked for each one the first time you connect. A host whose key is kept in the other client's own key store needs its key file chosen after importing.
- In the file panel, right-click the file list or the folder tree to create folders and files, rename (F2) and delete (Delete, or Cmd+Backspace on macOS). A rename never replaces an existing file.
- The README and the documentation are now also in Simplified Chinese.

### 简体中文

- Finallyshell 现在会检查新版本，并直接更新自己。默认自动检查，不需要的话可以在“选项 › 常规”里关掉；“检查更新…”在“选项 › 常规”和托盘图标的菜单里，macOS 上还在应用菜单里。`.deb` 安装包还是要靠安装新的 `.deb` 来更新。
- 0.2.0 自己没法更新，这次请手动安装 0.2.1，以后的版本会自动更新。
- 深色模式：在“选项 › 常规 › 外观”里选“跟随系统”“浅色”或“深色”。默认跟随系统，标题栏也跟着变。终端仍然用它自己的配色。
- “导入连接”窗口多了“导入来源”，除了 FinalShell，现在还能导入：
  - Xshell 的会话，连同文件夹结构。在 Windows 上会自动找到它的 Sessions 文件夹；要导入 `.xts` 导出文件，选它所在的文件夹就行。
  - SecureCRT 的会话，连同文件夹结构。SecureCRT 默认放配置文件夹的地方会自动找；挪过位置的话，选它的 Config 文件夹、里面的 Sessions 文件夹，或者 Sessions 里的任意文件夹。
  - MobaXterm 的会话，连同文件夹结构，从 `MobaXterm.ini`，或者导出的 `.mxtsessions`、`.moba` 文件里读。
  - OpenSSH 的 `~/.ssh/config`：每个 `Host` 别名成为一个连接，带上 `HostName`、`Port`、`User`（没写就用本机的用户名）和第一个 `IdentityFile`。会读取 `Include`（支持通配符），`Host *` 等通配模式的设置和在 `ssh` 里一样生效，`Match` 块会跳过。`ProxyJump` 指向文件里的另一个主机时，只要那个主机也一起导入，或者已经在 Finallyshell 里，就会设成跳板机。
  - CSV、TSV、文本或 Excel（`.xlsx`）文件里的服务器列表，表头写中文或英文都可以。“保存模板…”会导出一个 `servers.csv`，照着填就行。表里的密码存进系统的密钥库，不会写进 `hosts.json`。旧的 `.xls` 文件读不了，请另存为 `.xlsx` 或 CSV。
- Xshell、SecureCRT 和 MobaXterm 都把密码加密保存，所以密码不会导入，第一次连接时会问你。私钥存在这些软件自己密钥库里的主机，导入后要自己选一下私钥文件。
- 文件面板里，在文件列表或目录树上点右键，就能新建文件夹和文件、重命名（F2）、删除（Delete，macOS 上也可以用 Cmd+Backspace）。重命名不会覆盖已有的文件。
- README 和文档有了简体中文版。
