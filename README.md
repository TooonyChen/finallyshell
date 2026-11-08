<h1 align="center">
  <img src="resources/icon.png" width="64" height="64" alt="" align="center">&nbsp;Finallyshell
</h1>

<p align="center">
  An SSH client with FinalShell's layout and vertical session tabs, written in Go.
</p>

<p align="center">
  <a href="https://github.com/TooonyChen/finallyshell/releases/latest"><img src="https://img.shields.io/github/v/release/TooonyChen/finallyshell?label=release" alt="Latest release"></a>
  <a href="https://github.com/TooonyChen/finallyshell/actions/workflows/ci.yml"><img src="https://github.com/TooonyChen/finallyshell/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey" alt="macOS, Windows, Linux">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-PolyForm%20Noncommercial-blue" alt="License"></a>
</p>

<p align="center">
  <b>English</b> · <a href="README.zh-CN.md">简体中文</a>
  <br>
  <a href="#install">Install</a> · <a href="#features">Features</a> · <a href="#keyboard-shortcuts">Shortcuts</a> · <a href="docs/en/features.md">Full documentation</a>
</p>

<p align="center">
  <img src="docs/screenshots/main-en.png" alt="Finallyshell's main window: server monitor, session list, terminal and file panel">
</p>

If you have used FinalShell, you already know your way around: server stats at the top left, sessions under them, the terminal on the right and the remote files below it. Finallyshell keeps that layout and puts the session tabs in a vertical list you can scroll and drag to reorder.

It is a native app written in Go with the [MyGo](https://github.com/egoist/mygo) UI toolkit, so there's no Java runtime to install. The terminal is libghostty-vt, the terminal engine of [Ghostty](https://ghostty.org). Passwords go to the system's keychain, never to a file.

## Install

Download the latest build from [Releases](https://github.com/TooonyChen/finallyshell/releases/latest):

| System | File |
|---|---|
| macOS (Apple silicon) | `finallyshell-<version>-arm64.dmg` |
| macOS (Intel) | `finallyshell-<version>-amd64.dmg` |
| Windows (x64) | `finallyshell-setup-<version>-amd64.exe`, or the portable `.zip` |
| Debian, Ubuntu (x64) | `finallyshell_<version>_amd64.deb` |
| Other Linux (x64) | `finallyshell-<version>-linux-amd64.tar.gz`; `sh install.sh` installs it in `~/.local` |

The macOS app isn't notarized by Apple yet, so macOS blocks it the first time you open it. After dragging it to Applications, run:

```bash
xattr -dr com.apple.quarantine /Applications/Finallyshell.app
```

or open System Settings › Privacy & Security and click "Open Anyway". Windows SmartScreen may warn about the unsigned installer: click "More info", then "Run anyway".

On Linux, the tray icon needs `libayatana-appindicator3`, and remembering passwords needs a Secret Service keyring (GNOME Keyring, KWallet, KeePassXC). Without them the app still works; it just keeps passwords in memory.

After that, Finallyshell updates itself, except the `.deb`, which you update by installing the new one. Versions up to 0.2.0 can't: install a newer release by hand once.

## Features

### Connections

Open the connection manager with <kbd>Cmd</kbd>+<kbd>O</kbd> on macOS or <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>O</kbd> elsewhere. Hosts live in a tree of folders, as in FinalShell: drag them between folders, search by name, address, user or notes, duplicate a host, or delete it to a "Show deleted" list from which it can be restored.

<p align="center">
  <img src="docs/screenshots/connection-manager-en.png" width="720" alt="The connection manager with folders of hosts">
</p>

Each host can reach the server directly, through a SOCKS5 or HTTP proxy, or through a jump host (like `ssh -J`), and jump hosts can have proxies and jump hosts of their own. Port forwarding covers local (`-L`), remote (`-R`) and dynamic SOCKS5 (`-D`) tunnels, which start with the session and show their status, connections and traffic in the Tunnels tab.

<table>
  <tr>
    <td><img src="docs/screenshots/connection-proxy-en.png" alt="Connecting through a jump host"></td>
    <td><img src="docs/screenshots/connection-tunnels-en.png" alt="Local, remote and dynamic tunnels"></td>
  </tr>
</table>

Every new tab opens on quick connect: the hosts you connected to last, then the others, newest first, with a search box. Moving from another client? "Import" in the connection manager reads the connections and folders of FinalShell, Xshell, SecureCRT and MobaXterm, the hosts in your `~/.ssh/config`, or a server list in a CSV or Excel file. Passwords the other clients keep encrypted aren't imported; you're asked for each one the first time you connect. A server list's passwords go into the system keychain.

<table>
  <tr>
    <td><img src="docs/screenshots/quick-connect-en.png" alt="Quick connect on a new tab"></td>
    <td><img src="docs/screenshots/import-finalshell-en.png" alt="Importing connections from FinalShell"></td>
  </tr>
</table>

Passwords and key passphrases are kept in macOS Keychain, Windows Credential Manager or the Linux Secret Service, never in `hosts.json`. Host keys are confirmed on the first connection and checked on every one after it.

### Server monitor

While a session is connected, the panel at the top left shows the server's uptime, load, CPU, memory and swap, the processes using the most memory, network speed, latency and disk space, refreshed every two seconds. "System info" shows the OS, kernel, CPU model, memory, IP addresses and disks, and copies them as text. The monitor reads `/proc`, so it needs a Linux server.

<p align="center">
  <img src="docs/screenshots/system-info-en.png" width="560" alt="The system info popover">
</p>

### Files

The file panel under the terminal browses the server over SFTP. Right-click a file, or a folder in the tree beside it, to rename or delete it or to create a file or folder. Drag files or folders from Finder or Explorer to upload them; download to your Downloads folder or anywhere else. Transfers run three at a time, show progress, speed and time left, and can be canceled and retried. A folder of many small files can also go as one archive, packed with tar and gzip as it streams. The count and overall progress also show on the Dock icon or taskbar button.

<p align="center">
  <img src="docs/screenshots/transfers-en.png" width="560" alt="The downloads popover with transfers in progress">
</p>

Double-click a text file to edit it in place. The editor highlights over 30 languages and has find and replace, go to line, multiple cursors and folding. Saving is atomic where the server allows it, warns you when the file changed on the server since you opened it, and keeps the file's line endings and encoding, GBK and GB18030 included.

<p align="center">
  <img src="docs/screenshots/file-editor-en.png" width="720" alt="Editing a remote shell script">
</p>

Files and folders get icons from VS Code icon themes. Material Icon Theme and Seti are built in, themes you installed in VS Code, VSCodium, Cursor or Windsurf are found automatically, and any other can be installed from a `.vsix` file.

<p align="center">
  <img src="docs/screenshots/file-icon-themes-en.png" width="720" alt="Choosing a file icon theme">
</p>

### Terminal

Pick a font, size, line height and weight, with or without ligatures, any of the hundreds of color schemes Ghostty ships, and an optional background image. Changes apply to open terminals at once. The terminal also has find (<kbd>Cmd</kbd>+<kbd>F</kbd> / <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>F</kbd>), full screen, copy on select, a choice of what right-click does, and links you can open with <kbd>Cmd</kbd>/<kbd>Ctrl</kbd>+click.

Servers whose locale isn't UTF-8 work too: the terminal converts GBK, GB18030, Big5, Shift_JIS, EUC-JP, EUC-KR, ISO-8859-1 and Windows-1252, set once in Options or per host. What Backspace and Delete send can be changed the same way.

<table>
  <tr>
    <td><img src="docs/screenshots/terminal-colors-en.png" alt="Color schemes with a live preview"></td>
    <td><img src="docs/screenshots/terminal-background-en.png" alt="A background image under the terminal"></td>
  </tr>
</table>

### And also

- English and Simplified Chinese, following the system language or chosen in Options.
- Light and dark appearance, following the system or chosen in Options.
- Every shortcut can be changed. Keys the shell needs, such as <kbd>Ctrl</kbd>+<kbd>C</kbd> or <kbd>Ctrl</kbd>+<kbd>R</kbd>, are refused with an explanation.
- Minimize to the tray or menu bar, and confirm before closing while sessions are connected.
- Only one copy runs at a time: opening it again brings the running window forward.
- Updates itself: it checks once a day, or when you ask, and installs a signed update in place.
- Drag the dividers between panels to resize them; the sizes are remembered.
- Optional sync of connections and settings between computers, through a GitHub gist, a WebDAV folder (Jianguoyun, Nextcloud...) or S3-compatible storage (Cloudflare R2, Backblaze B2...) of your own, encrypted on your computer with a password only you know. No account with us, no server, nothing to pay.

[docs/en/features.md](docs/en/features.md) describes every feature in detail, and [docs/en/configuration.md](docs/en/configuration.md) the files the app keeps.

## Keyboard shortcuts

The defaults follow FinalShell. Change them in Options › Terminal › Shortcuts.

| Action | macOS | Windows / Linux |
|---|---|---|
| Copy | <kbd>Cmd</kbd>+<kbd>C</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>C</kbd> |
| Paste | <kbd>Cmd</kbd>+<kbd>V</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>V</kbd> |
| Find in terminal | <kbd>Cmd</kbd>+<kbd>F</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>F</kbd> |
| Full screen | <kbd>Cmd</kbd>+<kbd>Enter</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>Enter</kbd> |
| Reconnect a disconnected session | <kbd>Enter</kbd> | <kbd>Enter</kbd> |
| Previous tab | <kbd>Cmd</kbd>+<kbd>[</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>Tab</kbd> |
| Next tab | <kbd>Cmd</kbd>+<kbd>]</kbd> | <kbd>Ctrl</kbd>+<kbd>Tab</kbd> |
| Close tab | <kbd>Cmd</kbd>+<kbd>W</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>W</kbd> |
| Connection manager | <kbd>Cmd</kbd>+<kbd>O</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>O</kbd> |
| New tab | <kbd>Cmd</kbd>+<kbd>T</kbd> | <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>T</kbd> |
| Go to tab 1 to 9 | <kbd>Cmd</kbd>+<kbd>1</kbd>…<kbd>9</kbd> | <kbd>Alt</kbd>+<kbd>1</kbd>…<kbd>9</kbd> |

<p align="center">
  <img src="docs/screenshots/shortcuts-en.png" width="720" alt="The shortcuts page in Options">
</p>

## FAQ

**macOS says the app is damaged or can't be opened.** It isn't notarized yet. Run the `xattr` command under [Install](#install).

**Where are my hosts and passwords stored?** Hosts and settings are JSON files in your user config folder (`~/Library/Application Support/finallyshell` on macOS, `%AppData%\finallyshell` on Windows, `~/.config/finallyshell` on Linux). Passwords are in the system keychain under the service name `finallyshell`. See [docs/en/configuration.md](docs/en/configuration.md).

**The monitor panel stays empty.** It reads `/proc` and works only with Linux servers. Some jump hosts refuse the extra channel it opens: turn off "Enable exec channel (server monitor)" for that host.

**Does it support Telnet, RDP or serial ports?** No, only SSH and SFTP.

## Build from source

You need Go 1.27.3 or later.

```bash
git clone https://github.com/TooonyChen/finallyshell.git
```

```bash
cd finallyshell && go tool mygo dev
```

`go tool mygo dev` runs the app and restarts it when the code changes; `go tool mygo build` packages it in `build/<platform>/`. The first run downloads libghostty-vt into your user cache folder. [docs/en/development.md](docs/en/development.md) covers tests, releases and the code's layout.

## Contributing

Issues and pull requests are welcome. Every change starts as an issue that says how it will be solved; [CONTRIBUTING.md](CONTRIBUTING.md) has the details and the terms for contributions.

## Acknowledgements

Thanks to:

- [FinalShell](https://www.hostbuf.com/), which kept me company from my first year of university to my first job, and which inspired this project.
- The [V2EX](https://www.v2ex.com/) community.
- The [LINUX DO](https://linux.do/) community.

## License

Finallyshell is source-available, not open source as the OSI defines it. You may use it under either of these licenses ([LICENSE](LICENSE) has the full text):

- [PolyForm Noncommercial 1.0.0](https://github.com/polyformproject/polyform-licenses/blob/master/PolyForm-Noncommercial-1.0.0.md): any noncommercial use, including forking, changing and sharing it.
- [PolyForm Internal Use 1.0.0](https://github.com/polyformproject/polyform-licenses/blob/master/PolyForm-Internal-Use-1.0.0.md): use inside your own company, changes included, without distributing it. Using it to manage servers at work falls under this one.

Selling it, bundling it in a paid product or distributing changed versions for commercial purposes needs a separate commercial license: open an issue to get in touch.
