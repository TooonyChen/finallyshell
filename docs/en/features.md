# Features in detail

English · [简体中文](../zh-CN/features.md)

This page describes what Finallyshell does, screen by screen, including
the `settings.json` keys behind each option and the limits of what is
supported. [configuration.md](configuration.md) covers the files on disk;
[development.md](development.md) covers building and the code.

- [Window layout](#window-layout)
- [Sessions and tabs](#sessions-and-tabs)
  - [Bottom panel in a tab of its own](#bottom-panel-in-a-tab-of-its-own)
  - [Coding agents in tabs](#coding-agents-in-tabs)
- [Connections page](#connections-page)
- [New tab and quick connect](#new-tab-and-quick-connect)
- [Connection manager](#connection-manager)
- [Import connections](#import-connections)
  - [From FinalShell](#from-finalshell)
  - [From Xshell](#from-xshell)
  - [From SecureCRT](#from-securecrt)
  - [From MobaXterm](#from-mobaxterm)
  - [From an SSH config](#from-an-ssh-config)
  - [From a CSV or Excel file](#from-a-csv-or-excel-file)
- [Connection editor](#connection-editor)
  - [SSH connection](#ssh-connection)
  - [Terminal](#terminal-page)
  - [Proxy and jump hosts](#proxy-and-jump-hosts)
  - [Tunnels](#tunnels-page)
  - [Keys and other behavior](#keys-and-other-behavior)
- [Server monitor and system info](#server-monitor-and-system-info)
- [Terminal](#terminal-section)
  - [Find](#find)
  - [Full screen](#full-screen)
  - [Command bar](#command-bar)
  - [Appearance: font, color scheme, background image](#appearance-font-color-scheme-background-image)
- [File panel](#file-panel)
  - [Following the terminal's folder](#following-the-terminals-folder)
  - [File icon themes](#file-icon-themes)
  - [Symbolic links](#symbolic-links)
  - [File operations](#file-operations)
  - [Upload and download](#upload-and-download)
  - [Archive transfer](#archive-transfer)
  - [Transfers tab](#transfers-tab)
  - [Downloads popover](#downloads-popover)
  - [Progress on the app icon](#progress-on-the-app-icon)
- [Remote file editor](#remote-file-editor)
- [Tunnels tab](#tunnels-tab)
- [Processes tab](#processes-tab)
- [Network tab](#network-tab)
  - [IP locations](#ip-locations)
  - [Trace route](#trace-route)
- [Commands tab](#commands-tab)
- [Options window](#options-window)
  - [General](#general)
  - [Transfers](#transfers)
  - [Terminal](#terminal-defaults)
  - [Terminal › Mouse](#terminal--mouse)
  - [Terminal › Shortcuts](#terminal--shortcuts)
  - [Terminal › Command bar](#terminal--command-bar)
  - [Network monitor](#network-monitor)
  - [Sync](#sync)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Layout and split dividers](#layout-and-split-dividers)
- [Language](#language)
- [Updates](#updates)

## Window layout

The main window follows FinalShell's layout, except that the session tabs
are vertical:

- Top left: the server monitor of the current session.
- Bottom left: the vertical session list, or, on its other page, the
  saved connections in their folders (see
  [Connections page](#connections-page)).
- Top right: the terminal.
- Bottom right: the Files, Transfers, Tunnels, Processes, Network and
  Commands tabs, which can also open in a tab of their own (see below).

The bottom panel can collapse to its row of tabs, which stays at the
bottom, and the terminal takes the rest of the height. The chevron at the
right end of the tab row, before "Open in its own tab", hides the panel
(chevron down, "Hide panel") and shows it again (chevron up, "Show
panel"); so do "Hide panel" and "Show panel" in the tab row's context menu
and the shortcut "Show or hide the bottom panel" (Cmd+J on macOS,
Ctrl+Shift+J on Windows and Linux). While the panel is collapsed, a click
on a tab's label opens it on that tab, at the height it had before, and
so does anything that opens one of its tabs, such as a click on the
server monitor's process list, which opens Processes. The choice is kept
in `settings.json` and restored at the next start. A panel opened in a
tab of its own is the panel itself and is not affected, and full screen
shows the terminal alone either way.

The bottom panel can also go on the right of the terminal: "Move panel to
right side" in the tab row's context menu puts it in a column on the
right, full height, with the terminal and its command bar on the left;
"Move panel to bottom" puts it back. The column is 460 DIPs wide by
default and keeps its width when the window is resized, the terminal
taking the difference. Its tabs scroll sideways where they do not fit
(with the mouse wheel over them, a touchpad, or the keyboard), and so do
the Files table's columns. Hidden while on the right, the column goes away
and a thin strip at the terminal's right edge keeps the "Show panel"
button (chevron left; the hide button in the tab row is a chevron right);
the shortcut, the strip's context menu and anything that opens one of the
panel's tabs show it again, as at the bottom. The side is kept in
`settings.json`. A panel opened in a tab of its own ignores it, and full
screen is unchanged.

The server monitor can go on the right too: "Move to right side" at the
top of its context menu puts it in the same column, and "Move to left
side" puts it back. The session list then fills the left column, and the
monitor's old height there comes back with it. With the bottom panel
under the terminal, the monitor has the column alone, full height, next
to the terminal and the panel. With the panel on the right as well, the
monitor is on top and the panel below, with a divider between them.
Hiding the panel there collapses it to its row of tabs at the column's
bottom, with the "Show panel" button (chevron up) at its end, and the
monitor takes the height; a click on a tab opens the panel on it. The
monitor follows the current session wherever it is, and a panel opened
in a tab of its own has it beside it. The side is kept in
`settings.json`; full screen shows the terminal alone.

## Sessions and tabs

The bottom left of the window is a vertical list of sessions, on the
"Sessions (N)" page of the switch at its top. Each has a status dot:
green for connected, yellow for
connecting, red for failed. The list scrolls, and sessions can be
reordered by dragging. The context menu of a session has Reconnect,
Duplicate session, Disconnect, Rename and Close.

A session tab can have a name of its own for this run, to tell apart
several tabs open to the same server. "Rename" in its context menu, or F2
while the session list has the keyboard focus, turns the tab's name into
a text field in place, with the name selected. Enter keeps the new name;
Esc, clicking elsewhere or moving the focus away keeps the old one.
Names are trimmed and kept to one line, and an empty name, or the host's
own title, goes back to the host's name, as "Reset name" in the context
menu does. A renamed tab's tooltip shows the host and its address. The
name is shown wherever the tab is named: the session list and finding a
row by typing in it, the transfers, and the file editor's tabs opened
after the rename. It is never saved: it stays through Disconnect and
Reconnect, and goes when the tab closes. "Duplicate session" opens a tab
with the host's name. New tabs, which have no host yet, cannot be
renamed. In the terminal, F2 goes to the program running there.

The "+" button opens a new tab (see below). New tabs can be selected,
dragged and closed like sessions, but the count in the title does not
include them. When every tab is closed, the right side still shows quick
connect, and connecting there opens a new tab.

When a connection succeeds, its time is recorded for the "Recent" list of
quick connect. When a host's `noMonitor` is `true`, the server status is
not polled, so no extra exec channel is opened (some jump hosts do not
allow one).

Each connection sends an SSH keepalive (`keepalive@openssh.com`) every
30 seconds, as OpenSSH's `ServerAliveInterval` does, so that routers,
firewalls and proxies don't drop a session that is quiet, or whose
program prints nothing for a while. It does not need the server monitor
or its exec channel, and the connection to each jump host on the way
sends its own; through a SOCKS5 or HTTP proxy, the host's keepalive
crosses the proxy. Any answer counts. When three in a row get no answer
(15 seconds each, or the interval when shorter), the connection is
closed and the session shows "Connection lost", ready to reconnect, rather
than looking connected until something is typed. The interval is set in
Options › General; 0 turns keepalives off.

On the first connection to a host, a dialog shows the host key
fingerprint. Once trusted, it is written to `finallyshell/known_hosts` in
the config directory. A connection whose host key has changed is refused.

### Bottom panel in a tab of its own

The button at the right end of the bottom panel's tabs ("Open in its own
tab", two arrows pointing out) opens a panel row: a row in the session
list, indented right below its session, that shows the bottom panel in
the whole right side, without the terminal, for the tables that want
height, such as Processes and Network. The row shows the icon and the
name of the tab it shows, such as "Network". Its tooltip, and the text
the list finds as you type, is "server1 | Network", with the session's
current name; a panel row is not renamed itself. The session's own row
keeps its terminal and its bottom panel as before.

- The panel row is another view of the same session, not a second
  connection: it uses the session's connection, files, tunnels,
  processes and network lists. A folder opened in the panel row is the
  one the session's row shows too.
- It has its own selected tab, which starts at the one selected when it
  opened; the row's icon and name follow it. A click on the side panel's
  process list shows its Processes tab.
- A session has at most one panel row: the button selects it when it is
  already open.
- In the panel row the button is "Back to terminal" (two arrows pointing
  in), which selects the session's row. The panel row stays until it is
  closed with its × or its context menu (Back to terminal, Close).
- Dragging the session moves its panel row with it; the panel row cannot
  be dragged alone. Closing the session closes its panel row too, and
  reconnecting keeps it, showing the new connection. Closing the panel
  row ends nothing.
- A panel row is not a session: the count in "Sessions (N)" and the
  question when quitting leave it out.
- The terminal's actions do nothing in a panel row: full screen, find,
  the command bar.
- A new tab has no button. A session that failed or was disconnected
  still opens its panel row, whose pages show the session's state.

### Coding agents in tabs

While a coding agent runs in a session's shell, the session's row shows
the agent's logo after its name, on the name's line, so that several tabs
to the same server, each running an agent, can be told apart. The logo's
tooltip is the agent's name ("Claude Code"). It is drawn in one color,
the row's text color, as the other icons are. A long name is cut before
the logo. The name itself doesn't change: a renamed tab shows its own
name with the logo, and the transfers, finding a row by typing and the
file editor's tabs read the name alone. A panel row shows no logo; its
session's row does.

| Agent | Its program |
|---|---|
| Claude Code | `claude`, `@anthropic-ai/claude-code` |
| Codex | `codex`, `@openai/codex` |
| Gemini CLI | `gemini`, `@google/gemini-cli` |
| pi | `pi`, `@mariozechner/pi-coding-agent` |
| opencode | `opencode`, `opencode-ai` |
| Amp | `amp`, `@sourcegraph/amp` |
| Cursor CLI | `cursor-agent` |
| GitHub Copilot CLI | `copilot`, `@github/copilot` |
| Qwen Code | `qwen`, `@qwen-code/qwen-code` |
| Goose | `goose` |
| Kimi CLI | `kimi` |
| Kiro CLI | `kiro-cli` |
| Aider | `aider`, `aider-chat` |
| Crush | `crush` |
| Droid (Factory) | `droid` |
| Auggie | `auggie`, `@augmentcode/auggie` |

Aider, Crush, Droid and Auggie show a generic robot, having no logo in
the set the app uses.

How it is told:

- The program is the one in the foreground of the shell's terminal, read
  from the server's `/proc` over an exec channel of the session's own
  connection, with the shell's folder (see [Following the terminal's
  folder](#following-the-terminals-folder)), so two tabs to the same
  server are told apart. It is an agent when its name, or its first
  argument's, is one of those above, whole: `pip` is not `pi`, and
  `claude-monitor` is not `claude`. When the program is a runtime or a
  launcher (`node`, `bun`, `deno`, `python`, `npx`, `bunx`, `pnpm`,
  `uvx` and the like, and `sudo`), the script or package it runs counts,
  so `node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js` and
  `npx @openai/codex` are agents.
- It is read after Enter in the terminal and after the command bar sends
  a command, about 250 ms and 1.5 s later, and after Ctrl+C, Ctrl+D and
  Ctrl+Z, which end or stop an agent without an Enter. While a tab shows
  an agent, it is also read every 10 seconds, so that the logo goes when
  the agent ends by itself. Nothing is read otherwise, and a tab never
  has more than one read in flight. A read that fails, as one that times
  out, changes nothing: the logo stays, and so do the reads every 10
  seconds, until a read that answers.
- Not seen: an agent inside `tmux` or `screen`, or in an `ssh` to another
  host; servers without `/proc` (macOS, BSD); hosts whose exec channel is
  off (`noMonitor`). An agent run with `sudo` is seen, unless `/proc` is
  mounted with `hidepid`.
- Options › Terminal › "Show the coding agent running in each tab", on by
  default, turns it off: no logo, and no reads for it (the reads for the
  file panel still happen while it follows the terminal).

The logos are from [LobeHub Icons](https://github.com/lobehub/lobe-icons)
(MIT license, credited in the third-party licenses). They are their
owners' trademarks, shown only to name the program that is running.

## Connections page

The switch at the top of the bottom-left panel, "Sessions (N) |
Connections", shows the session tabs or the saved connections. The
Connections page is the connection manager's tree of folders and
connections, kept in the main window as MobaXterm's session tree and
Xshell's Sessions pane are, without taking width from the terminal. It
shows the names only, in the order the connection manager shows them
unsorted, and never the deleted connections. A connection's tooltip
shows `user@host:port` and the first line of its notes. A connection
with a session open has a dot after its name, in the color of that
session's dot in the session list: green when connected, yellow while
connecting.

- Double-click a connection, or select it and press Enter, to open a
  session, in the selected tab if it is a new tab, as quick connect does.
  The session shows on the right, and the panel stays on Connections, so
  several connections can be opened in a row; "Sessions (N)" counts the
  new ones. Double-click or Enter on a folder opens or closes it, as do
  its arrow and the Left and Right keys.
- The search box at the top filters as the connection manager's does:
  every word must be in the name, address, user or notes, ignoring case.
  The folders holding the results open and the first result is
  selected, so Enter connects to it; Down moves to the tree; Esc clears
  the search and closes again the folders it opened. Its search is its
  own, apart from the connection manager's.
- The context menus are the connection manager's: Connect, Edit,
  Duplicate, Rename (F2), Move to folder, Copy host and Delete on a
  connection; Connect all, New connection, New subfolder, Rename, Move
  to folder and Delete on a folder; New connection and New folder on the empty space
  under the rows. F2 and Delete work on the selected row. Rows are
  dragged onto a folder, or onto a connection for its folder, to move
  them, as in the connection manager.
- Deleting asks first, for a connection too: it moves to the deleted
  connections, which the page does not list, and which "Show deleted" in
  the connection manager lists to restore.
- The "+" in the header is New connection while the page is shown: in
  the selected folder, or the folder of the selected connection. The
  editor it opens, and Edit, belong to the main window (not modal); once
  saved, the page selects the connection.
- Both places read the same connections, so a rename, move or delete in
  either shows in the other at once.
- The key "Switch sessions / connections" (Cmd+Shift+E on macOS,
  Ctrl+Shift+E elsewhere, changed on the Shortcuts page) switches the
  pages: to Connections with the keyboard in the search, leaving full
  screen first, and back to the sessions with the keyboard in the
  terminal.

The page shown is `leftPanelPage` in `settings.json`, and the folders
open are `connectionTreeExpanded`, by folder ID, so a folder keeps being
open as it is renamed or moved. Until a folder is opened or closed on the
page, every folder is open. Neither is synced: they are where this
computer's window was left, as the pane sizes are.

## New tab and quick connect

One new tab is opened at startup. Its "Quick connect" list shows every
host that is not deleted (name, host, user):

- First the hosts connected recently (at most 30), the last one connected
  at the top.
- Then the other hosts, the last one added at the top. The time a host was
  added (`createdAt`) is recorded when it is created, duplicated or
  imported. Older hosts without that time come last, in their saved
  order.

When both parts are present they are headed "Recent" and "Other
connections". A host that was just added therefore appears here right
away, without having to be connected once first.

- Clicking a host or pressing Enter connects in this tab, and the same row
  turns into the session.
- The search box filters as you type, matching name, address, user and
  notes. Results keep the same order as without a search. The up and down
  arrow keys move the selection and skip the "Recent" and "Other
  connections" rows.
- "Clear" clears the list of recent connections. It does not delete
  hosts: they are all still listed under "Other connections".
- The "Connection manager" button opens the connection manager. While a
  new tab is selected, connecting from the connection manager also
  connects in that tab.
- When there are no hosts at all, the page shows a "New connection"
  button and, under it, a "Connection manager" button, so a new user can
  import their connections from another client (see below) before adding
  any.

## Connection manager

The connection manager opens with Cmd+O on macOS and Ctrl+Shift+O on
other systems (the key can be changed on the Shortcuts page). It is a
separate window titled "Connections". As in FinalShell, it is a tree of
folders and connections, with the columns Name, Host, Port, User and
Notes (only the first line of the notes is shown). Clicking a column
header sorts by it, and dragging the edge of a header resizes the column.

The host list is stored in `<user config dir>/finallyshell/hosts.json`.
Passwords are never written to it; they are kept in the system's
credential store (see
[Where passwords are stored](configuration.md#where-passwords-are-stored)).

Toolbar:

- New connection.
- New folder: created in the selected folder, or, when a connection is
  selected, in the folder that connection is in.
- Collapse all, Expand all.
- Import: import connections from another client (see below).
- Show deleted.
- A folder filter: "All", or only one folder.
- A search box.

Below the tree is "Close window after connecting", checked by default. It
is stored as `keepManagerOpen` in `settings.json`; without that key the
window closes.

Search filters as you type, matching name, address, user and notes. Every
word must match, ignoring case. Folders that contain results are expanded
and the first result is selected, so Enter connects to it. Clearing the
search restores the folders that were expanded before.

Double-clicking a connection, or pressing Enter, opens a session in the
main window (in the selected tab if the main window has a new tab
selected), brings the main window to the front, and closes the connection
manager if "Close window after connecting" is checked.

The context menu of a connection has:

- Connect.
- Edit.
- Duplicate: the copy is named "<name> (copy)". Proxy or jump host
  settings, tunnels, and the remembered password and proxy password are
  copied too; the entries in the credential store are copied to the new
  connection as well.
- Rename (F2).
- Move to folder.
- Copy host: copies the host address.
- Delete (the Delete key).

The context menu of a folder has Connect all, New connection, New
subfolder, Rename, Move to folder and Delete. Deleting a folder asks first. Its connections
and subfolders move to its parent folder (or to the top level for a
top-level folder), and the question says where they go.

Connect all opens a session to every connection in the folder and in its
subfolders, in the order the tree shows them, each as a double-click
opens it: the first in the selected tab if it is a new tab, the others in
tabs of their own, and the connection manager closes if "Close window
after connecting" is checked. Deleted connections are left out, and on a folder with no
connection the item is disabled. With more than 10 connections it asks
first: "Connect to N servers?", naming the folder. A connection whose
password is not known asks for it, and the next ones wait, so the
prompts come one at a time; Cancel skips that connection and goes on
with the next. The Connections page has the same item.

Dragging a connection or folder onto a folder row moves it into that
folder. Dropping it on a connection row moves it into the folder of that
connection, and dropping it on empty space moves it to the top level. A
folder cannot be dragged into itself or into one of its own subfolders.

Deleting a connection only moves it to the deleted items (`deletedAt`).
With "Show deleted" checked, deleted connections are shown in grey in
their original folders, and their context menu has Restore and Delete
permanently (which asks first). Every permanent delete goes through
`deleteHostForever`, which also clears the password and proxy password
remembered for the host, both in memory and in the credential store.

Deleting (or permanently deleting) a connection that other connections use
as their jump host asks first, and the question lists the connections that
will no longer be able to connect. The jump host drop-down only lists
connections that are not deleted.

A connection editor opened from the connection manager is a modal window
of the connection manager (see
[Child windows](development.md#child-windows)). After it saves, the
connection manager shows and selects the connection right away.

The main window's [Connections page](#connections-page) shows the same
tree, and what changes in one shows in the other at once.

## Import connections

Import, in the connection manager's toolbar, opens the "Import connections"
window, a modal window of the connection manager. "Import from" at its top
chooses where the connections come from: the sources below, each in
its own section. The window opens
on the source chosen last since Finallyshell started (FinalShell the first
time).

Choosing a source looks for its file or folder in the places that source
keeps it (the files are read in a goroutine and shown through `App.post`
when done). If none is found, or the source has no place of its own, a
file or folder chooser opens (showing hidden files and folders). Canceling
it says what to choose, with a button to choose it. Once a file or folder
is read, its path is shown at the top (the home folder as `~`, except on
Windows), next to "Choose file…" or "Choose folder…", which reads
another at any time.

The window lists every SSH connection read (Name, Host, User, Folder,
Note), each with a checkbox. All are checked by default. "Select all" and
"Select none" change them all, and Enter, double-click or Space toggles
the selected row.

- A connection with the same host address (ignoring case), port and user
  as an existing host is shown in grey as "Already present as ...". It
  cannot be checked and is not imported. Of two identical connections in
  the source, only the first is imported.
- Files, or lines of a file, that cannot be read (not in the source's
  format, too large, a read error, no host address) count as failed.
  Connections of other kinds (RDP, VNC and such) and connections deleted
  in the other client count as skipped. Both are listed below the table,
  each with its file, or its file and line (`file:line`).
- The Note column says what is not imported of a connection, such as a
  proxy, or that its key file must be chosen after importing, and the jump
  host it will connect through.

"Import N" puts the connections into folders that follow the source's
folder structure. Folders are matched by name level by level from the top:
an existing one is reused, a missing one is created, and only folders that
receive imported connections are created. A source without folders
imports at the top level. The window then closes, the connection manager
expands those folders and selects the first imported connection, and the
bottom of the connection manager shows how many were imported, skipped and
failed, and how many need a key file chosen.

For every source:

- The time added (`createdAt`) is the time of the import, so imported
  connections come first under "Other connections" in quick connect.
- Passwords that other clients store are not imported: they encrypt them
  with keys of their own, and Finallyshell does not decrypt them. Each
  password is asked for at the first connection (remembered by default,
  in the system keychain). A list of servers that has passwords in it
  puts each in the system keychain, as "Remember password" does, never in
  `hosts.json`.
- A private key file the source names becomes the host's key file. A key
  the other client keeps in a store of its own is not imported: the host
  has "Private key" authentication without a key file, and the Note
  column and the result say to choose it.

### From FinalShell

FinalShell keeps its connections in the `conn` folder of its data folder,
which Finallyshell looks for in order:

- Windows: `%LOCALAPPDATA%\finalshell`, `%APPDATA%\finalshell`,
  `%ProgramFiles%\FinalShell`, `%ProgramFiles(x86)%\FinalShell`,
  `C:\finalshell`. If FinalShell is installed somewhere else and keeps its
  data in its install folder, choose the folder yourself.
- macOS: `~/Library/FinalShell`, `~/Library/Application Support/finalshell`.
- Linux: `~/.finalshell`, `~/.local/share/finalshell`.

You can choose the data folder, its `conn` folder, or a folder exported
from FinalShell.

Fields that are imported: name, host, port, user, notes (`description`),
folder, authentication method, and whether the exec channel is enabled (a
connection with it turned off is imported with the server monitor turned
off). The time added is not FinalShell's `create_time`.

- Passwords are not imported. FinalShell stores them encrypted in its
  files; Finallyshell does not decrypt them and does not read that field.
- A connection that uses a private key is imported with "Private key"
  authentication but no key file. FinalShell stores private keys in its
  own `config.json` and the connection only records the key's ID; that
  file is not read, so edit the connection and choose the key file.
- Proxies (`proxy_id` points into FinalShell's own proxy list), port
  forwarding, terminal encoding and backspace settings are not imported
  (the format FinalShell stores those values in is not known yet). A
  connection that used a proxy says so in the Note column.

FinalShell's file format has no official documentation. The import follows
the FinalShell import code of other open source clients (Hatoba,
LiteShell, MeatShell, OpenFinalShell), summarized in the comments of
`internal/importer/finalshell.go`. Parsing is lenient: unknown fields are
ignored, missing ones get defaults (port 22, password authentication), and
a port written as a string is accepted.

### From Xshell

Xshell keeps each session in a file of its own, `<name>.xsh`, in its
`Sessions` folder; subfolders are its folders. On Windows, Finallyshell
looks for that folder in order:

- `Documents\NetSarang Computer\<version>\Xshell\Sessions` (Xshell 6 and
  later), the newest version first, in the profile's Documents folder and
  in OneDrive's.
- `Documents\NetSarang\Xshell\Sessions` (Xshell 5).
- `%APPDATA%\NetSarang\Xshell\Sessions` (older versions).

On macOS and Linux, and when Xshell keeps its sessions somewhere else (set
in its options), choose the folder yourself. You can choose the
`Sessions` folder, the `Xshell` folder it is in, or any folder with
`.xsh` files or an `.xts` export (File, Export in Xshell) in it: an
export is read as it is, without extracting it, and its folders are
imported under the folder it is in. The chooser picks folders only, so to
import an `.xts` file, choose the folder that holds it.

Fields that are imported: the name (the file's, without `.xsh`), host,
port, user, notes (the session's Description), folder and authentication
method. Only SSH sessions are imported; Telnet, Rlogin, serial and other
sessions are skipped.

- Passwords are not imported. Xshell stores them encrypted in the session
  file; Finallyshell does not decrypt them and does not read that field.
- A session that uses a public key is imported with "Private key"
  authentication but no key file: Xshell keeps its keys in its own key
  store, and the session only names the key. Edit the connection and
  choose the key file (exported from Xshell's User Keys in OpenSSH format
  if needed).
- Proxies (a name from Xshell's own proxy list), port forwarding and
  terminal settings are not imported. A session that used a proxy says so
  in the Note column.

Xshell's session files have no official documentation. The import follows
session files published by their users and other tools that read them,
summarized in the comments of `internal/importer/xshell.go`. Files written
by Xshell 6 and later (UTF-16) and by older versions (the ANSI code page,
GBK on Chinese Windows) are both read. Parsing is lenient: unknown
sections and keys are ignored, a missing protocol is SSH, and a missing
port is 22.

### From SecureCRT

SecureCRT keeps one `.ini` file per session in the `Sessions` folder of
its config folder, with a subfolder for each of its folders. Finallyshell
looks for the config folder at:

- Windows: `%APPDATA%\VanDyke\Config`.
- macOS: `~/Library/Application Support/VanDyke/SecureCRT/Config`.
- Linux: `~/.vandyke/SecureCRT/Config`.

SecureCRT can move its config folder (Options › Global Options › General ›
Configuration Paths shows where it is). Choose the config folder, its
`Sessions` folder, or a folder in it (its sessions are imported with the
folders under it). `Default.ini` at the top of `Sessions` holds
SecureCRT's default settings and `__FolderData__.ini` the order of a
folder's sessions: neither is a session. At most 10,000 session files
are read; the first one past that is listed as failed.

Fields that are imported: name (the file name without `.ini`), host,
port, user, notes (the session's description) and folder. SSH2 and SSH1
sessions are imported; Telnet, Serial, RLogin, RDP and other sessions
count as skipped.

- Passwords are not imported. SecureCRT stores them encrypted in the
  session files; Finallyshell does not decrypt them and does not read
  those lines.
- A session with a key file of its own (public key options) is imported
  with "Private key" authentication and that key file: the private key,
  when SecureCRT names its `.pub` file. `${VDS_CONFIG_PATH}` in the path
  is the config folder. A session that uses SecureCRT's global key gets
  the global key file, from `SSH2.ini` in the config folder, when its
  authentication methods try a public key before a password. A key path
  that cannot be resolved (another SecureCRT variable, a Windows path on
  macOS or Linux, or the global key when no `SSH2.ini` is found next to
  the `Sessions` folder) leaves the key file to choose, as the Note column
  says.
- Firewalls are not imported: a session through a proxy says "Proxy not
  imported" in the Note column, and one through another session, a jump
  host, says "Jump host not imported". Port forwarding, logon actions,
  terminal and appearance settings are not imported either.

SecureCRT's file format has no official documentation. The import follows
VanDyke's scripting examples and the SecureCRT readers of other open
source projects, summarized in the comments of
`internal/importer/securecrt.go`. Parsing is lenient: lines in other
formats and options it does not use are ignored, missing ones get
defaults (SSH2, port 22, password authentication), and multi-line values
(binary values, and lists other than the description) are skipped.

### From MobaXterm

MobaXterm keeps its sessions in `MobaXterm.ini`. On Windows, Finallyshell
looks for it in `%APPDATA%\MobaXterm` (the installed version), then in
`Documents\MobaXterm`. The portable version keeps it next to
`MobaXterm.exe`: choose it there. You can also choose a file of sessions
exported from MobaXterm: "Export all sessions to file" or "Export
sessions from this folder" write a `.mxtsessions` file, and "Save session
to file" a `.moba` file of one session. Files saved on Chinese Windows
(GBK) are read too.

Only SSH sessions are imported; RDP, VNC, SFTP, Telnet and the others are
skipped. Fields that are imported: name, host, port, user (none for
MobaXterm's `<default>`), the comment as the notes, the folder (the
session folders, nested as in MobaXterm; a folder exported on its own is
a folder of its name), and the private key file, as the host's key file
(`_CurrentDrive_` in the path is the drive letter `C`). On macOS and
Linux, a key path of Windows is not a file there: the host has "Private key" authentication
without a key file, to choose after importing.

- Passwords are not imported. MobaXterm stores them encrypted (behind its
  master password), apart from the sessions; Finallyshell does not read
  them.
- SSH gateways (jump hosts) and proxies are not imported: the host
  connects directly, and the Note column says so. Its jump host can be
  set in the connection editor afterwards.
- Terminal settings, colors and fonts, the command run at login, X11
  forwarding and MobaXterm's SSH tunnels are not imported.

MobaXterm's file format has no official documentation. The import follows
the description of the format reverse-engineered for MobaXterm 26.3 in
the gist ".mxtsessions file format" by Ruzgfpegk, and the readers of
other open source tools (mobaConverterGo, mobaxterm2ssh, mobaxterm-mcp),
summarized in the comments of `internal/importer/mobaxterm.go`. It has
not been checked against users' files yet, so parsing is lenient:
unknown sections and fields are ignored, missing fields get defaults
(port 22, password authentication), and a line that cannot be read is
listed as failed with its line number.

### From an SSH config

OpenSSH's client configuration, `~/.ssh/config` (on Windows,
`%USERPROFILE%\.ssh\config`), is read when it exists; otherwise, or with
"Choose file…", any file can be chosen (of any name). It is read as `ssh`
reads it:

- Each name of a `Host` line is a connection named after it: `Host web db`
  gives two. Patterns (names with `*`, `?` or `!`) are not connections, but
  their settings go to the names they match, and lines before the first
  `Host` go to every name. Each setting comes from the first line that
  applies, in file order, as in `ssh`: `Host *` with `User deploy` at the
  end gives every host that user unless it has one of its own.
- Fields that are imported: `HostName` (`%h` is the name; without it, the
  host is the name), `Port`, `User` (the local user's name when there is
  none, as `ssh` uses), the first `IdentityFile` as the private key file,
  and `ProxyJump`. Keywords are read in any case, as `Key value` or
  `Key=value`, with values in quotes and `#` comments.
- In the key file's path, `~` stays as the home folder, and `%d` (the home
  folder), `%u` (the local user), `%h` (the host), `%r` (the user), `%n`
  (the name), `%p` (the port) and `%%` are expanded. Other tokens are left
  as they are, and the Note column says so.
- A `ProxyJump` to another name of the file makes that connection the jump
  host, once both are imported (or the jump host is present already); the
  Note column shows it. A chain (`a,b`) uses its last hop. A jump host that
  is not a name of the file (`user@host:port`, or a name with no `Host`
  line), or not imported, is not set: the host connects directly, and the
  Note column says so. The note follows the checkboxes: unchecking the
  jump host changes it at once on the hosts that go through it.
  `ProxyCommand` is not imported either, and says so as a proxy.
- `Include` reads other files in place, with globs; a relative path is in
  the folder of the file chosen (`~/.ssh`, as `ssh` has it). At most 16
  levels and 256 files are read.
- Connections are imported at the top level, with no notes. A host with a
  key file uses "Private key" authentication with that file; the others
  use a password, asked for at the first connection (the file has none).

Listed below the table: a `Host` line with patterns only and a `Match`
block (not supported) as skipped; a `Port` that is not a number from 1 to
65535 (ignored), a line that does not parse (a quote not closed), and an
`Include` that cannot be read or goes past the limits as failed. Other
keywords (`LocalForward`, `CertificateFile` and such) are ignored.

### From a CSV or Excel file

"CSV or Excel file" imports a list of servers kept in a spreadsheet: a
`.csv`, `.tsv` or `.txt` file, or the first visible sheet of an Excel
workbook (`.xlsx`). It has no place of its own, so the file chooser opens
right away. "Save template…" at the bottom of the window saves
`servers.csv` (the downloads folder is suggested): the headers in the
app's language and one example row, to fill in and import.

The first row is the header (a title row or a few above it are skipped:
the first of the first ten rows with a host column is the header).
Columns are found by their header, ignoring case, spaces, `_` and `-`;
other columns are ignored:

| Field | Headers |
| --- | --- |
| Name | name, label, title, 名称, 名字, 别名 |
| Host (required) | host, hostname, address, ip, ip address, 主机, 主机名, 主机地址, 地址, ip地址 |
| Port | port, 端口 |
| User | user, username, login, 用户, 用户名, 账号 |
| Password | password, 密码 |
| Private key | key, private key, key file, identity file, 私钥, 私钥文件 |
| Folder | folder, group, 文件夹, 分组 |
| Notes | notes, note, description, comment, comments, 备注, 说明 |

- A file without a host column is not imported: the window lists the
  headers it found.
- Rows are listed in the file's order. Empty rows are skipped. A row
  without a host, or with a port that is not 1 to 65535, is listed below
  the table with its row number, as failed.
- A host written `host:port` (`[IPv6]:port` for an IPv6 address) is
  imported as the host alone; its port is used when the Port cell is
  empty (a filled Port cell wins). A bare IPv6 address is not split.
  `user@host` gives the user when the User cell is empty.
- Spaces around a cell's value are ignored, except in the Password cell:
  a password is imported exactly as written. A Password cell of only
  spaces counts as empty.
- A folder `Production/Databases` (or with `\`) is a nested folder. A
  list without a Folder column imports at the top level.
- A private key file makes the host log in with that key; its Password
  cell is then not used. The others log in with a password.
- Passwords go into the system keychain, as "Remember password" does,
  never into `hosts.json`. The table has a Password column that shows
  `••••` for the rows with one, never the password.

Text files: the encoding is the one Excel saved them in: "CSV UTF-8"
(with a byte order mark), plain "CSV" (the system's code page, GBK on
Chinese Windows) or "Unicode text" (UTF-16 with tabs). The delimiter is
the one of tab, `;` (Excel's CSV in many European languages) and `,`
under which one of the first ten rows is a header with a host column, so
a title row above the header does not decide it (with several, the one
whose header has the most known columns). A file without a host column
uses its first line's: a tab, `;` when there are more of them than of
`,`, else `,`. A first line `sep=;` says it instead. Quotes are read as
Excel writes them.

Excel files: cells are read as text, numbers without a trailing `.0`, and
a formula's cell as the value Excel or LibreOffice saved with it. Number
formats (dates, currency) are not applied. Excel 97-2003 workbooks
(`.xls`), workbooks protected with a password, and OpenDocument
spreadsheets are not read: the window asks to save the file as `.xlsx`
or CSV. A text file named `.xls`, as some systems export, is read as
text.

Limits: the file is at most 10 MB, and rows after the first 10,000 are
not read (the first one not read is listed). A workbook's parts are
unzipped to at most 32 MB each, and its sheet to at most about a million
cells.

## Connection editor

New connection and Edit connection open a separate window, with its pages
on the left and OK and Cancel at the bottom.

### SSH connection

- Name, Host, Port, Notes (several lines).
- Method (Password or Private key), User, then either Password, or the
  Private key file ("Browse…" chooses it) and its Passphrase.
- "Remember password" ("Remember passphrase" for a private key), on by
  default.
- "Enable exec channel (server monitor)".

Errors are shown under the field they belong to.

<a id="terminal-page"></a>

### Terminal

- Terminal type: the `TERM` sent to the server, `xterm-256color` by
  default.
- Backspace sends, Delete sends and Character encoding: the first choice
  of each is "Default: ...", which follows the
  [Terminal page of Options](#terminal-defaults); each can also be set for
  this host alone. The choices are the same as on that page.

In `hosts.json` these are `backspace` (`del`, `ctrl-h`), `delete`
(`vt220`, `del`, `ctrl-h`) and `encoding` (the names are listed under
[Terminal](#terminal-defaults)); a missing field means "Default". Hosts
saved by older versions have no `backspace`, so they follow the global
setting, whose default is still `^?`: their behavior does not change.

### Proxy and jump hosts

Type is one of None (connect directly), SOCKS5, HTTP or Jump host.

- SOCKS5 and HTTP (`CONNECT`) take the proxy's host and port (1080 and 8080
  by default), and a user and password when the proxy needs
  authentication (HTTP uses Basic authentication). With SOCKS5 the proxy
  resolves the host name, so the host can be a name only the proxy's side
  knows.
- Jump host picks another saved connection (not the connection itself).
  It works like `ssh -J`: log in to the jump host first, then have its SSH
  server forward the connection to this host (the jump host must allow TCP
  forwarding). A jump host can have its own proxy or jump host, and the
  hops are connected one after another. If they loop back, both the editor
  and connecting report "The jump hosts form a loop: a → b → a".
- Every hop is treated as if it were connected directly: its host key is
  confirmed on the first connection, and it uses its own login method and
  password. When connecting, passwords that are not known yet are asked
  for one at a time, in order: the host's own, then its proxy's, then each
  jump host's. The prompt is titled "Jump host password" or "Proxy
  password" and says which host it is for and which connection needs it.
  After a host is changed to use a jump host, reconnecting asks for the new
  jump host's password first.
- Proxy passwords are kept in the system's credential store like host
  passwords (following "Remember password" on the SSH connection page), not
  in `hosts.json`.
- Errors name the hop that failed: "Proxy 1.2.3.4:1080: Connection
  refused: no proxy on that port", "Jump host web-1: Authentication
  failed: ...", "Jump host web-1: Cannot connect to 10.0.0.5:22: ...".
- Closing or disconnecting a session also closes its connections to the
  jump hosts.

<a id="tunnels-page"></a>

### Tunnels

SSH port forwarding. A table lists Type, Listen on, Forward to and
Enabled, with Add…, Edit… and Remove below it (double-clicking a row also
edits it). Adding and editing use the same dialog.

- Local (`-L`): listens on this computer, and forwards each connection
  through the server to the target (the target address is resolved on the
  server's side).
- Remote (`-R`): listens on the server, and sends each connection back to
  this computer, which connects to the target.
- Dynamic (`-D`): a SOCKS5 proxy on this computer; the server connects to
  the addresses that clients ask for. Only CONNECT is supported, without
  authentication. Addresses can be IPv4, IPv6 or domain names (resolved
  by the server). The SOCKS5 server is a small piece of code of its own
  (`sshx/forward.go`), as none of the existing dependencies had one.

By default a tunnel listens on `127.0.0.1` only (for a remote tunnel, the
server's `127.0.0.1`), so only the same machine can connect. Checking "Let
other computers connect" allows another listen address (`0.0.0.0` by
default) and shows a warning: anyone can use the tunnel without logging
in. A remote tunnel also needs the server's `GatewayPorts` to allow it.

The listen address of a remote tunnel must be an IP address (IPv4 or IPv6)
or `localhost` (meaning `127.0.0.1`). The SSH library would resolve a host
name on this computer rather than on the server, so host names are not
accepted. If `hosts.json` was edited by hand to contain one, that tunnel
reports an error explaining why when it starts, and the table shows a
warning icon on it.

Ports must be from 1 to 65535, and the target host is required. When
another tunnel on the same machine already listens on the same port, the
dialog says "Another tunnel already listens on this port". A tunnel with
"Start with the session" turned off does not start automatically, but can
be started by hand on the Tunnels tab.

### Keys and other behavior

- Enter is OK (except in Notes, where it starts a new line), and Esc is
  Cancel.
- Only one editor is open at a time. Clicking New connection or Edit while
  one is open brings the open one to the front, without losing what is
  unsaved in it.
- Changes to the host of a session that is already open take effect at the
  next connection.
- If the host is deleted in the connection manager while it is being
  edited, OK only says "This connection was deleted" and does not save it
  back.

## Server monitor and system info

The top left, or the top of the column on the terminal's right (see
[Window layout](#window-layout)), shows the current session's server: IP, uptime, load, CPU,
memory and swap, the processes using the most memory, network speed and
latency graphs, and disks. It reads `/proc` over SSH every 2 seconds, so
only Linux servers are supported. A reading that takes more than 10
seconds, as when `df` hangs on a stale network mount, is given up, and
the next one starts on time. When not connected it shows an empty
placeholder like FinalShell's ("-", 0%). A click on the process list opens
the [Processes tab](#processes-tab).

Right-click anywhere on the monitor to move it to the other side ("Move
to right side" or "Move to left side") or to choose which parts show: a
check box each for "Uptime and load", "CPU, memory, swap", "Processes",
"Network", "Latency" and "Disks". The IP line and the System info button
always show. The choice is the same for every host, and the placeholder
hides the same parts. When every part is hidden, the panel shows the IP
line and "Right-click to choose what shows". The stats keep being read
while a part is hidden, so showing it again shows its graph's history at
once. The interface name above the network graph keeps its own menu (see
below). The choice is kept in `monitorHidden` in `settings.json`, on this
computer only.

The network graph shows the busiest interface by default: the one with
the most bytes since the server started, never `lo`. The interface name
at the graph's top right opens a menu of "Automatic (busiest)" and every
interface of `/proc/net/dev` with its current speeds: the others first,
then Docker's `veth*`, then `lo`. Choosing an interface checks or
unchecks it, and with several checked the graph shows their sum and the
label says "eth0 + wg0". Each interface keeps its own history, so a
choice shows the last two minutes at once, and an interface that comes
up later (a VPN) is in the menu from the next reading. The choice is kept
per host (`monitorIfaces` in `hosts.json`, synced with the host). A
chosen interface the server no longer has is left out; when none of the
chosen is left, the graph shows the busiest again and its label says
"eth0 (auto)", with the missing names in its tooltip. Those stay checked
in the menu, marked "not found", until unchecked.

The "System info" button below the IP opens a popover with the host name,
OS (`/etc/os-release`), kernel, architecture, CPU model and cores, memory,
uptime, IP addresses and disks. "Copy" copies them as plain text. They are
read once each time the popover opens, over an exec channel, by a POSIX sh
script that also runs on BusyBox. When tools such as `lscpu`, `nproc` or
`ip` are missing, it skips them or uses something else.

<a id="terminal-section"></a>

## Terminal

The top right is the terminal. When a session disconnects, the last screen
stays, and a "Reconnect" bar appears at the top.

### Find

Ctrl+Shift+F (Cmd+F on macOS) opens a find bar at the top right of the
terminal (MyGo's `ui.FindBar`).

- As you type, the plugin's `Search` searches the screen and scrollback,
  highlights every match, and shows which match is current out of how
  many.
- Enter goes to the next match (upwards, to earlier output), Shift+Enter
  to the previous one. F3 and Shift+F3 (Cmd+G and Shift+Cmd+G on macOS)
  work too.
- Esc or "Done" closes the bar, ends the search and returns the keyboard to
  the terminal.
- Pressing the shortcut again selects all the text in the find box.
- Each session has its own find bar.
- Matching ignores the case of ASCII letters (that is how the plugin
  works).

### Full screen

Ctrl+Shift+Enter (Cmd+Enter on macOS) hides the left side bar and the file
panel below, leaving only the terminal, and puts the window into the
system's full screen. Pressing it again leaves full screen.

- Esc leaves full screen only when there is no shell to receive it (a new
  tab, a disconnected session). In a connected terminal, Esc goes to the
  shell as usual (vi needs it).
- If the window was already in the system's full screen, it stays there
  when the app leaves its full screen.
- Leaving the system's full screen by the system's means (the green button
  on macOS) also leaves the app's full screen.
- On a [panel row](#bottom-panel-in-a-tab-of-its-own), which has no
  terminal, the key does nothing. Going to a panel row while in full
  screen shows its panel alone.

### Command bar

Under the terminal is a one-line command bar, as FinalShell's command
input box: a terminal icon, the input, without placeholder text, and a
"History" button at its end. It is on by default; Options › Terminal ›
Command bar turns it off, or any of its popups, and lists its keys (see
[Terminal › Command bar](#terminal--command-bar)).

- Enter sends what the bar holds to the shell, followed by a carriage
  return, as if it had been typed in the terminal (through the session's
  encoding), and clears the bar. The terminal keeps all its keys: the bar
  only has the keyboard while you are typing in it.
- Tab completes the word at the caret:
  - a word that looks like a path (it has a `/`, or starts with `~` or
    `.`) opens the path picker, the server's folder in columns: folders
    first, ending in `/`, then files, with the icons of the file panel's
    icon theme, hidden ones last. Typing filters it (ignoring case),
    Backspace in an empty filter goes up a folder, the arrow keys move,
    and Enter or Tab inserts the entry. A folder inserted lists its own
    entries; a file ends the word with a space. Names with blanks or
    other characters the shell would read are escaped with `\`. At most
    2000 entries are listed, with "and N more" after them. A name with a
    control character (a newline, an escape sequence) is shown dimmed,
    with the character escaped, and cannot be inserted: sent, a newline
    would run what follows it as a command of its own. Command names and
    lines of the server's history with one are left out.
  - the first word otherwise opens command completion: the commands on
    the server's PATH that start with it. They are read once per
    connection, over one exec channel, with bash's `compgen -c` (which
    includes builtins), or a listing of each `$PATH` folder on servers
    without bash. A host whose "Enable exec channel" is off lists
    `/usr/local/sbin`, `/usr/local/bin`, `/usr/sbin`, `/usr/bin`, `/sbin`
    and `/bin` over SFTP instead.
  - any other word opens the path picker for the current folder.
- Relative paths are read from the shell's working directory when the
  shell reports it (OSC 7) or the app read it from the server (see
  [Following the terminal's folder](#following-the-terminals-folder)),
  else from the file panel's folder; `~` is the
  home folder. Folders are listed over the session's SFTP, so a server
  without SFTP shows the error in the popup, while sending and the history
  still work.
- Up, Alt+H or the "History" button opens the history picker (the
  button keeps the keyboard in the bar, and closes the picker when it is
  open; it is hidden while the history picker is off): the commands sent
  before that
  start with what is typed, the newest at the bottom, next to the bar.
  Up and Down move; Left and Right highlight one field (word) of the
  highlighted command, so `epel-release` can be taken out of
  `yum -y install epel-release` alone. Enter inserts the field in place of
  the word at the caret, or, with no field highlighted, puts the whole
  command in the bar. "Clear list" deletes the host's history.
- The history is the app's own, per host: every command sent from the
  bar, each once, newest last, with no limit on how many (the file is cut
  from the oldest past 10 MB; see
  [configuration.md](configuration.md#command-history)). Below it come
  the commands of the server's `~/.bash_history` and `~/.zsh_history`,
  read over SFTP once per connection, so commands typed in the terminal
  show up too (bash writes its history file when the shell exits).
- The button between the input and "History" chooses where Enter
  sends: the current session (the button shows only its icon), all
  sessions ("All (5)") or the sessions chosen ("3 sessions"). It is the
  same send target as the Commands tab's "Send to" (see
  [Commands tab](#commands-tab)), and the line goes to each session as
  it would to one: its encoding, its folder following, its host's
  history.
- Esc closes the popup; Esc again gives the keyboard back to the
  terminal. "Focus command bar" (Ctrl+Shift+L, Cmd+Shift+L on macOS)
  moves the keyboard to the bar and back.
- New tabs show no bar. In a session that is connecting or disconnected,
  the bar is shown disabled.

Commands you send often can be kept as buttons in the
[Commands tab](#commands-tab).

### Appearance: font, color scheme, background image

The Options pages Terminal › Font, Terminal › Color scheme and Terminal ›
Background image set how terminals look. A change applies at once to
every open terminal (`SetFont`, `SetTheme`), and new terminals use it too.
A change made while a session is connecting is applied once it connects.
Each page has "Restore defaults" at the bottom and a preview at the top: a
sample prompt, a colored `ls` and code with ligatures, drawn with UI
widgets in the same font, colors and background.

Font:

- Font family: as you type, the monospaced fonts installed on the system
  are suggested (from `ui.FontFamilies`). It can be a comma-separated list,
  of which the first installed font is used. Empty means the default:
  JetBrains Mono, Cascadia Mono, Consolas, Menlo, DejaVu Sans Mono,
  monospace.
- Size: 6 to 48, 13.5 by default.
- Line height: 0.8 to 2 times the font's own line height, 1 by default.
- Weight: Light 300, Regular 400, Medium 500, Semibold 600, Bold 700. The
  plugin always draws bold 300 heavier than this, and at least 700; there
  is no separate weight for bold.
- Ligatures: on by default. Turning them off sets `-calt` and `-liga`.

Changing the font ends a Ctrl+= zoom in the terminal; changing only the
color scheme does not.

Color scheme:

- The several hundred themes that Ghostty ships
  (`terminal.GhosttyThemes()`), including the user's own in Ghostty's
  themes folder.
- The search box filters by name. Each row shows the background color and
  a few colors as a sample, and the 16 ANSI colors are shown under the
  preview.
- The default is Tomorrow Night. If the theme file goes missing, the
  default is used.
- The same scheme shows in the light and the dark appearance of the app
  (Options › General › Appearance): there is a single choice, set as both
  the plugin's `Theme` and its `DarkTheme`.

Background image:

- "Choose…" opens the system's open dialog for a PNG, JPEG, GIF, WebP or
  BMP image (a file of at most 64 MB and at most 100 million pixels). The
  image is decoded first to make sure it can be shown, then copied into
  the config directory (`finallyshell/background-<time>.<extension>`), so
  moving or deleting the original does not matter. Choosing another image,
  or "Remove", deletes the old copy.
- Fit: Cover (cropped), Contain or Tile. Opacity: 5% to 100%, 30% by
  default.
- A file that is not an image, or cannot be read, only shows an error. It
  is not copied and does not replace the current image.
- If the copy goes missing (or the name in `settings.json` is not one of
  the app's copies), the page shows "The background image is missing:
  choose it again, or remove it." You can choose an image again or remove
  it.
- Reading and decoding the image never happen on the UI thread. Nothing is
  read at startup. The first time the image is needed, a goroutine reads
  it and shows it through `App.post`; until then the terminal shows only
  the color scheme's background color. If the image is changed or removed
  while it is being read, the old result is dropped.
- An image larger than 3840 pixels on its long side is scaled down before
  it is kept in memory (the copy on disk is still the original file).
  JPEG images are rotated according to their EXIF orientation.

How it is drawn: every terminal is created with `Transparent: true`, and
the app draws the background under it. First the color scheme's
background color, then the image, then a layer of the background color at
(1 − opacity), so text always sits on the color scheme's background.
Background colors set by programs (OSC 11, cell backgrounds) are drawn as
usual.

In `settings.json` these are `termFontFamily`, `termFontSize`,
`termLineHeight`, `termFontWeight`, `termNoLigatures`, `termTheme`,
`termBackground` (the file name of the copy), `termBackgroundFit`
(`contain` or `tile`; missing means cover) and `termBackgroundOpacity`
(percent). A missing key means the default.

## File panel

The "Files" tab at the bottom right browses the server over SFTP: a folder
tree on the left, a file table on the right (with multiple selection), and
a path bar where a path can be typed directly.

### Following the terminal's folder

When the shell's working directory changes, as after `cd /var/log`, the
file panel goes there too. The toggle button after the home button in the
panel's toolbar turns this off and on, for every session; turned back on,
the panel goes to the shell's folder at once. It is on by default.

- The panel moves only when the shell's folder changes. A folder the user
  browsed in the panel stays after a command such as `ls`, until the next
  `cd`.
- A shell that reports its folder (OSC 7: fish, and zsh or bash set up to)
  tells it. Otherwise, on a Linux server, the app reads it from `/proc`
  over a channel of its own on the same connection: the working directory
  of the program in the foreground of the shell's terminal, else the
  shell's. This needs the host's server monitor to be on (it uses the
  same exec channels), no root and no tools beyond the shell.
- It reads as the session connects, for where the shell starts, without
  moving the panel; then after Enter in the terminal, or a line the
  command bar sends: about a quarter of a second later and again after a
  second and a half. Nothing else is read while nothing is typed. A shell
  whose login scripts `cd` elsewhere leaves the panel at the home folder
  until the next `cd`.
- Not followed: macOS and other servers without `/proc` whose shell
  doesn't report OSC 7, root shells from `sudo -i` or `su` (the user can't
  read root's processes), and shells inside tmux or screen. A folder the
  panel can't open, such as one on another host the shell went to with
  ssh, leaves the panel where it was.

### File icon themes

The file panel shows icons by file name: `docker-compose.yml` gets the
Docker icon, `main.go` the Go icon, `src/` the src folder icon, and an
expanded folder switches to its open icon. The icons come from VS Code's
file icon theme format, so any VS Code icon theme works. The theme is
chosen on the Options page File manager › File icon theme, which has a
preview at the top, and takes effect at once.

- Built in: Material Icon Theme (the default), Seti (the one VS Code ships
  with), and Minimal (one icon for files and one for folders, as the app
  used to look).
- "Install from VSIX…" takes a `.vsix` file (a VS Code extension package,
  for example one downloaded from [Open VSX](https://open-vsx.org)). After
  checking that it contains an icon theme that can be read, it copies the
  file as is to `icon-themes/<publisher>.<name>.vsix` in the config
  directory. A newer version of the same extension replaces the older one.
  The installed theme is selected automatically. The limit is 50 MB. The
  remove button in the list deletes the copy.
- Icon themes already installed in VS Code, VS Code Insiders, VSCodium,
  Cursor and Windsurf are listed automatically (`~/.vscode/extensions` and
  so on; on Windows, under `%USERPROFILE%`). They are read in place, not
  copied. Uninstalled ones (`.obsolete`) are skipped, and only the newest
  version of an extension is listed. "Refresh" looks again.
- If the selected theme later disappears or cannot be read, the panel
  shows Material Icon Theme and the options page says why.

Only the theme's JSON and the SVG, PNG and font files it refers to are
read; no code from the extension runs. Paths that point outside the
extension are ignored. Themes that use a font for their icons (Seti uses
WOFF, file-icons uses WOFF2) are converted to TrueType before they are
registered with the UI. Icons are matched with VS Code's rules: file name
first, then extension (the longest first, so `a.test.ts` looks for
`test.ts` first), then language (the file name to language table comes
from the language extensions that VS Code ships), and finally the default
file icon. As in VS Code, the theme's `light` section is used only while
the app is light; in the dark appearance the icons are the theme's own.
Switching the appearance switches the icons at once.

In `settings.json` this is `fileIconTheme`: missing means Material,
`minimal` is Minimal, `bundled:seti` is Seti, and anything else is
`<publisher>.<name>/<theme ID>`.

Both built-in themes are MIT licensed; their licenses are inside their
VSIX files (`internal/fileicons/bundled/`). To update them and the language
table, change the versions in `internal/fileicons/internal/gen` and run
`go generate ./internal/fileicons` (this needs git and network access).

### Symbolic links

In the file table, the icon of each symbolic link has a small arrow in its
bottom left corner, and the name is followed, in light text, by where it
points (`bin → usr/bin`). As with `ls -l`, this is the link's text as
written, relative or absolute, not resolved.

- A link to a folder is shown as a folder, sorted with the folders, and
  double-clicking it opens it.
- A link to a file has the type "Link", the size of the file it points to,
  and the link's own permissions.
- A link whose target does not exist (or that loops) is a broken link: its
  name is red, its type is "Broken link", it has no size, and
  double-clicking does not open it. A link whose target cannot be examined
  for lack of permission is not considered broken.
- When listing a folder, up to 16 links are resolved at the same time
  (each needs two round trips, `readlink` and `stat`), so folders with many
  links (such as `/usr/lib64`) do not wait for them one by one.
- The folder tree on the left shows no arrows.

### File operations

Files and folders can be renamed, deleted and created from the context
menus of the file table and of the folder tree:

- The file table's menu has New folder, New file, Rename (one item
  chosen) and Delete (one or more chosen). With the table focused, F2
  renames the chosen item and Delete (or Cmd+Backspace on macOS) deletes
  the chosen items.
- The folder tree's menu acts on the folder right-clicked, without
  opening it: Open, Upload files, Upload folder (into that folder), New
  folder, New file, Rename, Delete, Refresh and Copy path. `/` has no
  Rename or Delete.

Rename, New folder and New file ask for a name. Rename starts with the
name chosen up to its extension, as Finder and Explorer do. A name that is
empty, `.`, `..` or has a `/` in it is refused, and so is a name already
taken in the folder: a rename never replaces another file, and a new file
never empties one that is there. Errors from the server (such as
permission denied) show in the dialog, which stays open.

Delete always asks first. A folder is deleted with everything in it. Links
are never followed: deleting a link to a folder removes the link, not the
folder it points to. Up to 16 files of a folder are removed at the same
time, so large folders don't wait for each round trip. Canceling the
dialog stops a delete that is still running; what was already removed
stays removed.

After a change the folder is listed again and a renamed or new item is
chosen in the table. When the current folder, or one it is in, is
renamed, the panel follows it to its new path; when it is deleted, the
panel goes to the folder it was in.

### Upload and download

Upload: drag files or folders from Explorer or Finder onto the file table
(they go to the current folder) or onto a folder in the folder tree. The
toolbar's upload button and the context menu can also choose files or a
folder.

Download: select files or folders and click the download button. They are
saved in the download folder, which is the system's Downloads folder
(`~/Downloads`) by default and can be changed on the Transfers page of the
options. "Download to…" in the context menu chooses a location instead.
With "Ask where to save each download" checked on the Transfers page, the
download button always asks for the location first with the system's
folder chooser.

### Archive transfer

A folder of many small files is slow over SFTP, which takes several round
trips for every file. "Download as archive" and "Upload as archive…" send
a selection packed with tar and gzip instead, as FinalShell's packed
transfer does:

- "Download as archive" is in the file table's context menu (for a folder
  or several items, not for a single file), in the folder tree's context
  menu (for any folder but `/`) and in the context menu of the toolbar's
  download button (right-click it; a click still downloads). The server runs `tar -czf -` in the items' folder; the app reads
  the stream and unpacks it into the download folder file by file.
- "Upload as archive…" is in the file table's and the folder tree's
  context menus and in the context menus of the toolbar's two upload
  buttons (right-click them; a click still chooses files or a folder). It chooses
  folders; the app packs them into a stream that the server unpacks with
  `tar -xzf -` in the folder, which it makes first if needed. The files
  belong to the user who logs in, whatever the owners on this computer.

Both directions stream: there is no temporary archive on either side.
Each is one transfer in the queue, like the others: the Transfers tab
shows "Download as archive" or "Upload as archive", the progress of the
files' bytes (counted while the transfer already runs), and the file
being packed or unpacked. Cancel stops tar on the server and closes its
channel; a file half written on this computer is removed. Existing items
of the same name are asked about once before starting, then replaced.
Folders, empty ones too, symbolic links, modes and modification times are
kept; owners, devices and sockets are not.

When `tar` is missing on the server, or fails, the transfer fails with
what it printed, and "Retry over SFTP" sends the same items the usual
way, one transfer each.

Downloads are unpacked safely: names that are absolute, have `..` or are
outside the items asked for stop the transfer; nothing is written
through a symbolic link (a link where a folder goes is replaced by the
folder, as tar does); a hard link may only point to a file the same
download wrote; setuid, setgid and sticky bits are dropped. The gzip
stream is read to its end, and when its checksum or size does not match,
the transfer fails and says that the files unpacked from it may be
wrong. On Windows, names that Windows does not allow (such as `a:b` or `CON`) fail
the transfer with the name; they are not renamed.

Some symbolic links are not created, and the rest of the download is:

- Links that point outside the download folder: by their target, or
  through other links once everything is unpacked (`a -> b/../..` where
  `b -> ..`), on the disk as it is.
- On Windows, all links when Windows does not allow the user to create
  them (it does in Developer Mode or for administrators).

The transfer then ends as done, with a warning line such as "2 links were
not created: they point outside the folder"; its tooltip lists the links.
"Retry over SFTP" is not offered for it.

### Transfers tab

The "Transfers" tab at the bottom right shows progress, speed, time left
and a cancel button. At most 3 transfers run at the same time; the rest
wait in a queue.

- When the destination already has an item with the same name, the app
  asks whether to replace it.
- A failed or canceled transfer can be retried ("Retry"). It is queued
  again on the same session with its current connection; a session that is
  closed or not connected cannot retry.
- A finished download can be opened ("Open", with the system's default
  app; if no app can open it, it is shown in its folder) or shown in its
  folder ("Show in folder").

### Downloads popover

The "Downloads" button at the far right of the file panel's toolbar works
like FinalShell's. While transfers are running, the button shows their
count. It opens a popover:

- The first row is the download folder (with the middle left out when it is
  too long), "Change…", and "Show in Finder" ("Show in Explorer" on
  Windows, "Show in folder" on Linux; the folder is created first if it
  does not exist).
- Below are the recent transfers, uploads and downloads, newest at the top,
  each row the same as on the Transfers tab.
- At the bottom is "Clear finished".

The popover and the Transfers tab show the same queue: the Transfers tab in
the order transfers started, the popover the other way round.

### Progress on the app icon

While transfers are running:

- On macOS, the Dock icon shows their count and the overall progress.
- On Windows, the taskbar button shows the overall progress (MyGo has no
  icon badge on Windows).
- Linux docks that support the Unity launcher API show both.

The overall progress counts the bytes of the transfers that are running
and have a known size. Everything is cleared when all transfers end.
Progress is updated at most 4 times a second, and the count is updated as
soon as it changes.

## Remote file editor

As in FinalShell, double-clicking a file in the file table (or selecting
it and pressing Enter), or choosing "Edit" in the context menu, opens it
in the editor window. Double-clicking a folder still opens the folder.

The editor is MyGo's code editor (`ui/editor`): syntax highlighting for
more than 30 languages, find and replace, go to line, undo, multiple
cursors and folding. The language is chosen by file name; a script without
an extension is recognized by its `#!` line. Languages it does not have,
such as nginx or systemd files, are shown as plain text, and `.conf` is
treated as TOML by MyGo.

### Window

There is one editor window (a child window of the main window, 1000×700,
resizable), with a tab for each file. Each tab remembers the session it
came from. The window title is `host:path` of the selected tab, and a tab
with unsaved changes shows a dot. Opening a file that is already open
switches to its tab.

The toolbar has Save, Undo, Redo, Find, Replace, Go to line, Soft wrap and
Reload from the server. The status bar at the bottom shows "Saved" or the
error from saving, the language, line and column, the encoding and the
line endings (click the encoding or line endings to change them).

### Keys

- Ctrl+S (Cmd+S on macOS) saves.
- Ctrl+W (Cmd+W on macOS, or the key "Close tab" was changed to) closes the
  tab. Closing the last tab closes the window.
- Ctrl+Tab and Ctrl+Shift+Tab switch tabs.
- Esc only goes to the editor (it closes the find bar, the go to line bar
  and completion, and returns multiple cursors to one). Unlike other child
  windows, Esc never closes the editor window: Esc is pressed often while
  editing, and one stray press should not close the window. Close the
  window with its close button, or by closing the last tab.
- The editor's own keys (Ctrl+F find, Ctrl+H replace, Ctrl+G go to line,
  Ctrl+D select next, and so on; Zed's keys on macOS) are described in
  MyGo's documentation.
- The main window's shortcuts do not work in the editor window.

### Reading

The file is read entirely into memory over SFTP in a goroutine (no
temporary file), with progress shown for large files, and shown through
`App.post` when done.

- A file over 10 MB asks first ("Open Anyway").
- A file with a NUL byte in its first 8000 bytes is treated as binary and is
  not opened. "Open with the Default App" downloads it to the app's cache
  folder (`<cache dir>/finallyshell/opened/<time>/`; folders older than a
  day are deleted the next time one is opened) and opens it with the
  system's default app. Changes to that copy are not sent back to the
  server.

### Saving

The editor first writes a hidden temporary file in the same folder
(`.<file name>.finallyshell-<random>.tmp`), gives it the original file's
owner (when the owner differs) and permissions (including setuid and
similar bits), then replaces the original with `posix-rename@openssh.com`.
If the server does not have that extension (or it fails), the original is
deleted first and the temporary file renamed (renaming in SFTP version 3
cannot overwrite an existing file, so for a short moment the file does not
exist). This way programs that read the file only ever see the old or the
new version, and the original is safe if the connection drops midway.

Replacing creates a new file, which loses the original's extended
attributes, ACLs, SELinux label and hard links, and only root can give a
file to another user. So in these cases the editor writes in place instead
(truncate, then write):

- the file is a symbolic link (the target is written, and the link stays a
  link);
- the folder is not writable;
- the owner or permissions cannot be set;
- the rename is refused (for example in a sticky folder such as `/tmp`).

Writing in place keeps everything about the file but is not atomic: if it
fails halfway, the file is left truncated (the text is still in the editor,
so it can be saved again).

If creating the temporary file fails for another reason (for example a full
disk), the editor reports the error and leaves the original alone. If the
file has disappeared by the time it is saved, it is created again.

### Conflicts

The editor records the file's modification time and size on the server
when it is opened (or last saved), and checks them again before saving. If
they changed (or the file was deleted), it asks whether to "Overwrite" or
"Reload". Reloading counts as an edit, so Undo brings your text back.

### Line endings and encoding

- The file's line endings are kept (CRLF or LF; a file with both is saved
  with whichever it has more of).
- UTF-8 is the default, and a leading BOM is kept.
- For a file that is not valid UTF-8, a notice at the top says it may be in
  GB18030 (or GBK) and offers "Reopen as GB18030". Saving it unchanged
  writes back the bytes that are not UTF-8 as they were.
- The status bar can switch to GBK or GB18030 (`golang.org/x/text`). Before
  any change, the original bytes are decoded again with the new encoding;
  after a change, only the encoding used for saving changes. A character
  that GBK does not have (an emoji, for example) makes saving fail with the
  line it is on.

### Unsaved changes

- Closing a tab, or closing the window with its close button, asks "Don't
  Save / Cancel / Save" first. For the window it is "Save All": the tabs
  that are saved close, and the window closes once all of them are saved.
- When quitting the app, the confirmation also says how many files have
  unsaved changes. This question is asked whether or not "Confirm before
  closing the window" is on.
- When the main window is hidden to the tray, an editor window with
  unsaved changes is not closed.

### Disconnected sessions

When the session disconnects, the text in the editor is kept, a notice says
there is no connection, and Save is disabled. After the session reconnects,
saving uses its new SFTP connection. If the session's tab was closed, the
file is saved through another connected session to the same host.

Not supported: saving with sudo. A file without write permission fails to
save with "permission denied".

## Tunnels tab

The "Tunnels" tab at the bottom right lists the tunnels of the current
session. Each shows:

- its status: Starting…, Listening, Stopped, or the reason it failed, such
  as "Port 127.0.0.1:8080 is already in use" or "The server did not listen
  on …: the port is in use there, or forwarding is not allowed";
- the number of open connections and of connections in all;
- bytes sent and received;
- why the last connection that could not reach the target failed.

Each row has Start or Stop. The number on the tab is the number of tunnels
listening.

- When a session connects, its enabled tunnels start. A tunnel that fails
  (for example because its port is in use) only affects itself; the
  session connects as usual.
- Disconnecting, losing the connection or closing the session stops all of
  them and frees their ports. Reconnecting starts them again with the
  host's current settings.
- When the host is reached through a proxy or jump host, tunnels use the
  connection to the host itself.
- "Edit tunnels…" opens the Tunnels page of the connection editor.

## Processes tab

The "Processes" tab at the bottom right is the process manager of the
current session, as FinalShell's "advanced process management". A click
on the side panel's process list opens it too.

- The table lists every process with its PID, user, memory (RSS), CPU %,
  name, command line and executable path. A click on a header sorts by
  that column, a second click reverses it; CPU, highest first, is the
  default. The filter above it matches the name, the command line and
  the user, ignoring case, and the count beside it says how many are
  shown.
- The list is read again every 2 seconds while the tab is shown, and not
  at all while another tab or session is shown. The chosen row stays on
  its process as the rows move. Refresh reads it at once.
- CPU % is the share of one CPU the process used since the last reading,
  from the CPU times in `/proc/<pid>/stat`, as `top` shows it: a busy
  process with several threads can pass 100%. The first reading shows
  `ps`'s average over the process's life (0 on BusyBox, whose `ps` has
  none).
- The path of another user's process needs root to read and shows "-".
- A double click, Enter or "Open Details" in the context menu opens the
  process in a tab below the list, named like `7267-java`, with its PID,
  name, user, full command line, executable path and working directory
  on the left and its environment variables on the right. The tab's
  buttons copy all of it as text and read it again; the environment's
  context menu copies a value, a `NAME=value` line, or all of them.
  Fields that need root show "-" with "Needs root". A process gone since
  says so. Tabs close with their ×; the divider between the list and the
  tabs, and the one between the fields and the environment, can be
  dragged.
- The context menu of a row also has Copy PID, Copy Command Line, Send
  Signal (SIGTERM, SIGKILL, SIGHUP, SIGINT, SIGSTOP, SIGCONT) and Kill….
  Every signal asks first. Kill sends SIGTERM; when the process is still
  running 3 seconds later, the dialog offers "Force Kill" with SIGKILL.
  When the server refuses, the dialog shows its words, such as "The
  server refused: Operation not permitted". A process that ended while
  the dialog was open gets no signal, even when another process has its
  PID by then: the dialog says "The process has already ended. No signal
  was sent."
- When the session disconnects, the last list stays, greyed, under the
  same reconnect bar as the terminal's. Servers that are not Linux show
  "Only Linux servers are supported", and hosts whose server monitor is
  off show that it is off: the tab uses the same exec channels.

Each reading is one POSIX sh script over an exec channel, as the server
monitor's: `ps` for the users, memory and command lines, `cat` of every
`/proc/<pid>/stat` for the names and CPU times, and one `ls -l` of the
`/proc/<pid>/exe` links for the paths. It runs with BusyBox's `ps` and
`ls`, and leaves out its own processes. Details read
`/proc/<pid>/cmdline`, `exe`, `cwd` and `environ` once when the tab
opens. Signals are sent with `kill`, by a script that first checks the
PID's start time in `/proc/<pid>/stat` against the one the list read.

## Network tab

The "Network" tab at the bottom right is the network monitor of the
current session, as FinalShell's "advanced network monitor": which
processes listen on which ports, who is connected to them, and how much
each moves.

- The table has a row for each listening socket (TCP in LISTEN, and UDP
  sockets waiting for packets) and a row for each process with other
  connections, those it opened: PID, name, listen IP, port, protocol,
  remote IPs, connections, upload and download per second, and the bytes
  uploaded and downloaded. The connections to a listening socket count
  under it, whichever process holds them (a forked `sshd`, nginx's
  workers); a socket several processes share is the lowest PID's. A
  click on a header sorts by that column. The filter matches the name,
  the PID, the listen address and port, and the remote addresses of a
  row's connections.
- The table is read again every 2 seconds while the tab is shown, and not
  at all while another tab or session is shown; Refresh reads it at once.
  The chosen row stays on its socket or process as the rows move.
- Upload is what the server sent, download what it received. The rates
  are the difference between two readings of each TCP connection's byte
  counts (`bytes_acked` and `bytes_received` of `ss -i`): a new connection
  shows 0 until the next reading. UDP has no connections in the TCP
  sense: its sockets show "-" for the counts.
- Without root, the processes of other users' sockets can't be read:
  those connections are under a row "-", and the count says "Needs root
  for other users' processes". Connections in TIME-WAIT belong to no
  process.
- A double click, Enter or "Open Connections" in the context menu opens
  the row in a tab below the table, named like `0.0.0.0:80` or
  `7267-java`, with each connection: location (only with the location
  data, below), IP, port, state, upload and download per second, and the
  totals. A busy server's tab shows the first 5000 connections, and says
  so. The context menu of a connection copies its IP or address, traces
  the route to it, and, when online lookups are on, looks it up online.
- When the session disconnects, the last reading stays, greyed, under the
  reconnect bar. Servers that are not Linux, and hosts whose server
  monitor is off, show it, as the Processes tab does.

Each reading is one POSIX sh script over an exec channel: `ss -tunapiH`
(iproute2), with `awk` keeping only the byte counts of its TCP
information. Without `ss`, as on BusyBox, it reads `/proc/net/tcp`,
`tcp6`, `udp` and `udp6`, finds each socket's process by the socket links
in `/proc/<pid>/fd`, and its name in `/proc/<pid>/stat`; `/proc` has no
byte counts, so rates and totals show "-".

### IP locations

Nothing for IP locations is in the app, and without the data nothing is
looked up anywhere: the tables show IP addresses only, with no Location
column.

- The first time the Network tab shows, a bar above the table asks "Show
  where IP addresses are? This downloads about 11 MB of location data
  (ip2region)." with Download, Not Now and Don't Ask Again.
  - Download fetches `ip2region_v4.xdb` of [ip2region](https://github.com/lionsoul2014/ip2region)'s
    release v3.18.0 into `geo/` in the config directory, with its
    progress in the bar and Cancel. The file is written under a
    temporary name and kept only when its SHA-256 is the one the app
    keeps for it; a failed or canceled download leaves nothing behind,
    and the bar offers Retry. The data is read into memory off the UI
    thread, its checksum checked again each time.
  - The file comes from GitHub (`raw.githubusercontent.com`), and, when
    GitHub cannot be reached, sends nothing within about 15 seconds,
    answers with an error or serves a file of the wrong size or
    checksum, from jsDelivr (`cdn.jsdelivr.net`), which mirrors the same
    release and is often reachable where GitHub is not, as in mainland
    China. Both serve the same file: the checksum is the same whichever
    serves it. The progress starts from 0 again when the download moves
    to jsDelivr; Cancel stops it without trying jsDelivr. When both fail,
    the bar names each host and what it answered. The IPv6 file is too
    large for jsDelivr (it serves files of up to 20 MB) and comes from
    GitHub only.
  - Not Now hides the bar until the app starts again.
  - Don't Ask Again turns locations off (`geoLocations` `"off"`) until
    they are turned on in the options.
- With the data, the connections show the country, region, city and ISP
  of each address, from the data, which has Chinese names in China and
  English ones elsewhere. Private, loopback, link-local, CGNAT
  (100.64.0.0/10) and multicast addresses show "LAN" and are never looked
  up.
- IPv6 addresses need a data file of their own, about 37 MB more: the bar
  asks for it the first time an IPv6 peer shows (`geoLocationsV6`).
- Looking up online is off by default and turned on in the options (see
  [Network monitor](#network-monitor)). Then the addresses of the rows
  shown for which the data has no city or ISP are sent to ip-api.com,
  100 in a request, in the app's language (English or Chinese names),
  keeping to its limit of 15 requests a minute with the counts it sends
  back; while it answers 429, ip.sb is asked instead, one address at a
  time. The answers fill in what the data lacks, and are kept for 30
  days in `geo/cache.json`. "Look Up Online" in a connection's menu asks
  again, and prefers the online answer.

The data is ip2region's (Apache-2.0 or MIT). ip-api.com's free service is
unencrypted (HTTP) and for non-commercial use only.

### Trace route

"Trace Route" in a connection's menu, or the route button of the
toolbar for an address or host name typed in, traces the route from the
server in a tab below the table named like "Trace 8.8.8.8": hop, IP,
round trip (the average of the probes that answered), location, ASN and
host name.

- The tab runs `nexttrace --traceroute --raw -M -g <en|cn> <target>` on
  the server over an exec channel:
  [NextTrace](https://github.com/nxtrace/NTrace-core) runs as its own
  program, and locates the hops with its own service, as it is made to;
  `-M` keeps it from uploading the route for a map, and `-g` asks for the
  locations in the app's language (`cn` for Simplified Chinese, `en`
  otherwise). The app never calls NextTrace's service itself. Its RAW
  output is a line per probe as it goes, so each hop shows as it
  arrives; a hop that answers from more than one router shows the first.
  `--traceroute` keeps NextTrace in its traditional mode once its default
  becomes MTR (no earlier than 2027, its README says); a NextTrace too
  old to know the flag, as its `--help` shows, runs without it.
- When `nexttrace` is not on the server's PATH (with `/usr/local/bin`,
  `/usr/sbin` and `/sbin` added), the tab says so, with the install
  command of NextTrace's README, `curl -sL https://nxtrace.org/nt | bash`,
  and a Copy button: the app never installs anything on the server. It
  traces with `traceroute -n -w 2` or `tracepath -n` instead when the
  server has one, the hops showing as they arrive, located with the
  ip2region data when it is downloaded; with neither, it says so.
- NextTrace and traceroute need root, or the capability to send raw
  packets, on many servers: their error shows in the tab.
- The tab's buttons copy the route as text and trace it again.

## Commands tab

The "Commands" tab at the bottom right holds quick commands, as
FinalShell's quick command panel: commands you keep, as buttons, to send
to the current session with a click. They are the same for every host,
and kept in `quickcommands.json` (see
[configuration.md](configuration.md#quickcommandsjson)).

- The groups are on the left: "All", which shows every command, then
  your groups in their order, each with how many commands it has, and
  "New group", which names a new group in place (Enter keeps the name,
  Esc gives it up). A group's context menu renames it or deletes it,
  after asking; its commands are kept, in no group, under "All".
- The commands of the group chosen are buttons on the right, in rows
  that wrap. A button shows the command's name and, in gray, the command
  when it is one line, or how many lines it has; its tooltip is the
  whole command. The search box above them filters by name and by
  command, ignoring case.
- A click sends the command to the session, through the same path as
  the command bar's Enter: in the session's encoding, the file panel
  following the shell's folder as it does after Enter, and each line that
  runs kept in the host's command bar history. Each line is followed by a
  carriage return, so a command of several lines runs line by line. With
  "Press Enter after each line" off, the last line is sent without one:
  it waits at the prompt for you to check it and press Enter; the lines
  before it still run. In the session's row, the terminal then has the
  keyboard.
- "Send to", in the toolbar before the search box, chooses where the
  buttons send: "Current session" (the default), "All sessions", every
  session that is connected, or "Choose sessions…", a popover with a
  check box for each session, with its status dot and name. The command
  bar's target button sets the same target. While it is not the current
  session, both are in the warning color.
  - Each session gets the command once (a panel row is its session, not
    another one). New tabs and sessions that are not connected are
    skipped; the others still get it.
  - The first send after the target changes to anything but the current
    session asks "Send to N sessions?", listing them, with Send and
    Cancel. Later sends to the same target don't ask again.
  - The target is not saved: every start sends to the current session.
    A session closed drops out of the sessions chosen; with none left,
    the target is the current session again.
  - The terminal of the current session keeps the keyboard after a
    send, and "Put in the command bar" still fills the current session's
    bar only.
- The buttons are dimmed and send nothing while the session is not
  connected, in a new tab, and in a panel row whose session is not
  connected (with another target: while none of its sessions is
  connected); the commands can still be added, edited and arranged.
- Right-click a button for Send, Put in the command bar (a one-line
  command only: it fills the bar without sending it, and gives the bar
  the keyboard), Edit…, Duplicate (right after it, as "name (copy)"),
  Move to group, and Delete, which asks first.
- Drag a button onto another to move it there, onto the empty space
  after the buttons to move it to the end, or onto a group to move it
  into that group.
- "New command" opens the editor in a window of its own: the name, the
  group (the one shown, for a new command), the command, which can have
  several lines, and "Press Enter after each line" (on by default). An
  empty name shows the command's first line instead. Save keeps it; Esc
  or Cancel gives up the changes.
- The Commands tab can open in a tab of its own (see
  [above](#bottom-panel-in-a-tab-of-its-own)), for a board of commands
  as high as the window; its buttons send to its session.

Not supported: commands with parameters to fill in, commands of one host only, syncing the
commands, and a shortcut per command.

## Options window

The gear button next to the "Sessions" title at the bottom left opens the
Options window. As in FinalShell, it has a tree of pages on the left and
the page on the right. Every option is saved to `settings.json` as soon as
it changes, and takes effect at once, except for the two Mouse options
noted below and the keepalive interval, which applies to connections made
after the change.

The terminal appearance pages are described under
[Appearance](#appearance-font-color-scheme-background-image), and the
file icon theme page under [File icon themes](#file-icon-themes).

### General

- Language: System language, English or 简体中文.
- Appearance: System (the default), Light or Dark. System follows the
  system's light or dark appearance, also when it changes while the app
  runs. Light and Dark force it. Every window switches at once, native
  title bars included, and the app starts in the saved appearance. The
  terminal keeps its color scheme in both. Saved as `appearance`.
- Select new tabs automatically (on by default): switch to a new
  connection once it opens. When off, new sessions open in the background
  and the current session stays selected; if no session is selected, the
  new one is still selected.
- Minimize to tray when closed (off by default): the main window's close
  button hides the window to a tray icon (the menu bar on macOS). The
  icon's menu has "Show Finallyshell", "Check for Updates…" and "Quit". On Windows, left-clicking
  the icon also shows the window; on macOS, so does clicking the Dock icon.
  When the system has no tray (Linux without
  `libayatana-appindicator3`), the option behaves as if it were off.
- Confirm before closing the window (on by default): while a session is
  connected or connecting, closing the main window or quitting (the tray
  menu, Cmd+Q on macOS) asks once first, with the system's message box.
  With the tray option also on, the close button hides the window without
  asking, and only "Quit" asks.
- Connections: "Send a keepalive every N seconds", 30 by default, 0 to
  turn keepalives off, up to 3600. See
  [Sessions and tabs](#sessions-and-tabs). Saved as `keepAliveSeconds`.
- Updates: the version running, "Check for Updates…", "Check for updates
  automatically" (on by default) and the newer version a check found, or
  why this copy cannot update itself; see [Updates](#updates).

See [Closing and quitting](development.md#closing-and-quitting) for the
full behavior.

### Transfers

- Download folder, with "Change…" and "Use default".
- "Ask where to save each download" (off by default).

In `settings.json` these are `downloadDir` (missing means the system's
Downloads folder; choosing the system's Downloads folder again is also
stored as missing) and `askDownloadDir`. If the download folder has been
deleted, it is created again when downloading.

<a id="terminal-defaults"></a>

### Terminal

The Terminal page holds the global defaults for the character encoding
and the key sequences. Hosts set to "Default" in the connection editor use
them.

- Character encoding: UTF-8 (the default), GBK, GB18030, Big5, Shift_JIS,
  EUC-JP, EUC-KR, ISO-8859-1 or Windows-1252. It has to match the server's
  locale: older Chinese servers use a GBK locale, and with UTF-8 their
  Chinese text is garbled.
  - libghostty-vt decodes UTF-8 only, so the app converts between the SSH
    connection and the terminal (`internal/termenc`, with
    `golang.org/x/text`). The server's output is decoded into UTF-8 before
    it reaches the terminal, and a character split across two packets is
    put back together; bytes that cannot be decoded show as U+FFFD.
    Keyboard input, pastes and the terminal's replies are encoded before
    they are sent to the server; a character the encoding lacks is sent as
    `?` (not x/text's default 0x1A, which is Ctrl+Z in a shell). With UTF-8
    nothing is converted.
  - File names in the file panel and the built-in editor are still handled
    as UTF-8, and the server monitor reads ASCII output only, so neither is
    affected.
  - In `settings.json` this is `termEncoding`, in `hosts.json` `encoding`,
    with the values `utf-8`, `gbk`, `gb18030`, `big5`, `shift_jis`,
    `euc-jp`, `euc-kr`, `iso-8859-1` and `windows-1252`. Unknown values
    mean UTF-8.
- Backspace sends: `^?` (ASCII DEL, the default) or `^H` (Control-H, which
  some old systems and serial consoles need). On connecting, the erase
  character of the server's terminal is set to match. Alt+Backspace sends
  Esc followed by the same character. FinalShell's third choice, "VT220 -
  Delete" (`ESC [3~`), deletes forward in a shell, so it is not offered.
- Delete sends: `ESC [3~` (VT220, the default), `^?` or `^H`. Only Delete
  without modifiers changes; Ctrl+Delete and the like still send the
  plugin's `ESC [3;5~`, which programs read as Delete with modifiers.
- Changes apply the next time a session connects, since the conversion and
  the erase character are set when connecting. The page has "Restore
  defaults". In `settings.json` these are `termBackspace` (`ctrl-h`;
  missing means `^?`) and `termDelete` (`del`, `ctrl-h`; missing means
  `ESC [3~`). Unknown values mean the default.

### Terminal › Mouse

All of these use features of the terminal plugin itself.

- Copy on select (off by default): text selected with the mouse (and with
  Select All) is copied to the clipboard.
- Right click: Show the context menu (the default), Paste, Copy the
  selection, Copy if text is selected, else paste, or Do nothing. When a
  program on the server takes over the mouse (vim with `mouse=a`, for
  example), the right click goes to the program, and this setting applies
  only with Shift held.
- Ctrl+click opens links (Cmd+click on macOS, on by default): web addresses
  and OSC 8 hyperlinks shown in the terminal open in the system's browser.
  When off, clicking a link does nothing. The plugin has no switch to turn
  off link detection, so links (and file paths) are still underlined while
  Ctrl is held; they just do not open.
- Lines per mouse wheel notch (1 to 20, 3 by default): affects the mouse
  wheel only, not trackpads. On Windows, 3 means the "lines to scroll at a
  time" from the system's mouse settings, and other values scale from it.

Copy on select and the scroll lines take effect at once in open terminals
(the plugin has `SetCopyOnSelect` and `SetScrollMultiplier`). The plugin
reads the right click and link options only when a terminal is created and
has no setter for them, so open sessions pick them up after they
reconnect. Recreating a terminal in use would lose its screen and
scrollback, so the app does not do that.

In `settings.json` these are `copyOnSelect`, `rightClick` (`paste`,
`copy`, `copy-or-paste` or `ignore`; a missing or unknown value means the
context menu), `noLinkClick` and `wheelLines` (missing means 3, more than 20
means 20).

There is no middle-click paste. The plugin passes the middle button only
to programs that take over the mouse, does not handle it itself, and does
not expose whether a program has taken over the mouse; if the app handled
the middle button on its own, such a program would receive both the
middle click and the pasted text. The plugin also has no Linux PRIMARY
selection. Supporting it would need the plugin to add a middle-click
option like `RightClick`.

### Terminal › Shortcuts

A table lists each action with its current key and its default key (see
[Keyboard shortcuts](#keyboard-shortcuts)). Select a row and click
"Change…", press the new key combination in the dialog, and click OK to
save it. "Use default" fills in the default key, "Reset" restores the
selected row, and "Reset all" restores every row.

Keys the shell needs cannot be used, and the dialog says why in red:

- keys without modifiers (or with only Shift);
- letters, digits, punctuation, Space, the arrow keys and similar keys with
  only Ctrl (Ctrl+C, Ctrl+R, Ctrl+W, Ctrl+←, ...);
- the same keys with only Alt (Alt+B and Alt+F are Meta in the shell and
  move by words);
- on macOS, the same keys with only Option (they type characters);
- on Windows, Ctrl+Alt with a letter (that is AltGr, which many keyboard
  layouts use to type characters);
- keys the system uses (Cmd+Q, Cmd+H, Cmd+M, Cmd+Tab, Cmd+Space and others
  on macOS; Alt+Tab and Alt+F4 on Windows and Linux; Win key combinations
  on Windows).

Exceptions: "Switch to tab 1–9" may use Alt with the digits (readline's
numeric arguments are rarely used, and GNOME Terminal does the same), and
"Terminal: reconnect" may use any key (it only acts while the session is
disconnected and there is no shell).

A key that another action already uses is shown with a warning in yellow;
after OK, that other action has no key (shown as "None"). A key the
terminal itself uses (Ctrl+Shift+A select all, Ctrl+= zoom, Shift+PageUp
scrolling, Ctrl+Insert and Shift+Insert copy and paste) also shows a
warning, and after OK the key belongs to the shortcut.

Window shortcuts take effect at once. The terminal's copy and paste keys
are the terminal plugin's `Keybinds`, which the plugin reads only when a
terminal is created, so a change applies to sessions connected (or
reconnected) afterwards.

On macOS this page also has "Use Option as Alt (Meta)" (off by default,
the plugin's `OptionAsAlt`). When on, Option+B sends Esc b instead of
typing ∫. It also applies to sessions connected afterwards.

In `settings.json`, only changed keys are stored:
`"shortcuts": {"find": "Ctrl+Alt+F", "closeTab": "none"}`. The key names
are `copy`, `paste`, `find`, `fullScreen`, `reconnect`, `previousTab`,
`nextTab`, `closeTab`, `connectionManager`, `newTab`, `switchTab` (whose
value looks like `"Alt+1-9"`), `focusCommandBar`, `switchLeftPanel` and
`toggleBottomPanel`. `Super` is Cmd on macOS, and `none` means no
key. A key that is missing or cannot be read has its default. Option as Alt
is `optionAsAlt`.

### Terminal › Command bar

Under "Show the command bar under the terminal", a "Keys" section lists
the bar's keys: Enter, Tab, Up or Alt+H, Left and Right in the history
picker, Esc, and the Focus command bar shortcut as it is bound on
Terminal › Shortcuts. The bar itself shows no key help.

The switches of the [command bar](#command-bar), each on by default and
applied at once, to every session:

| Option | Key | |
|---|---|---|
| Show the command bar under the terminal | `noCommandBar` | Off hides the bar in every session: no popups, no history kept or read, no exec channel opened for it. The other switches can't change while it is off. |
| Path picker | `noCommandBarPaths` | Tab on a path, or on a word that isn't the first. |
| History picker | `noCommandBarHistory` | Up, Alt+H and the History button, which is hidden while it is off. Off also stops keeping the commands sent. |
| Command completion | `noCommandBarCommands` | Tab on the first word; off, Tab there opens the path picker. |
| Read the server's shell history | `noServerHistory` | `~/.bash_history` and `~/.zsh_history` in the history picker. |

The keys are named for turning each part off, so `true` means off.
"Clear history of all hosts…" asks first, then deletes the history the
app keeps of every host (the `history` folder); the servers' own history
files are not changed.

### Network monitor

The page of the [Network tab](#network-tab)'s IP locations and trace
route. The explanations are here, so that the tab stays quiet.

| Option | Key | |
|---|---|---|
| Show IP locations (downloads ip2region data, about 11 MB) | `geoLocations` | `"on"` shows the Location column; turning it on downloads the data when it is not there. `"off"` hides the column and keeps the files; it is also "Don't Ask Again". Missing until the user answers the bar. |
| Include IPv6 addresses (about 37 MB more) | `geoLocationsV6` | The same for the IPv6 data. |
| Look up IP locations online (ip-api.com) | `geoOnline` | `true` looks up online the addresses the data has no city or ISP for. Off by default, and only while locations are shown. |

The page shows how much the downloaded data takes on disk, and "Delete
Downloaded Location Data" asks first, then deletes `geo/` (the data and
the online lookups' cache) and turns locations and online lookups off.
The note under the online switch says that the addresses go to a third
party, and that ip-api.com's free service is unencrypted and for
non-commercial use. A last section says what Trace Route runs on the
server. None of these keys is synced: agreeing to a download, or to
sending the server's peers to a service, is each computer's own.

### Sync

Options › Sync keeps connections and settings the same on several
computers, through storage you already have. Finallyshell runs no server
for it, and it is off until you choose a service:

| Service | What you enter | Free for personal use |
|---|---|---|
| GitHub Gist | A personal access token with the `gist` scope ("Create a token on GitHub" opens the page with the scope chosen). The Gist ID can stay empty: the first sync finds a gist of yours holding `finallyshell-sync.json`, or creates a secret one, and keeps its ID. | Any GitHub account. |
| WebDAV | The folder's URL, a user and a password (an app password where the service has them). A missing folder is created. | Jianguoyun (坚果云), Nextcloud, Synology, Box and others. |
| S3-compatible storage | The endpoint, region (`auto` for Cloudflare R2; `us-east-1` when empty), bucket, file name (`finallyshell-sync.json` when empty), access key ID and secret access key. Requests are path-style and signed with AWS Signature Version 4. | Cloudflare R2 and Backblaze B2 have free tiers; MinIO runs anywhere. |

The service keeps one file. Before it leaves the computer, its content is
encrypted with AES-256-GCM, with a key derived by Argon2id (3 passes,
64 MiB, 4 lanes, a random salt) from the **sync password**, which you
choose and enter on each computer. The service sees only that the file is
an encrypted Finallyshell file and the key derivation's parameters; host
names, addresses and the computer's name are inside the ciphertext. The
sync password cannot be recovered. With a wrong one, sync says so and
changes nothing.

What is synced:

- Folders and connections, deleted ones included (so a delete reaches the
  other computers), but not each host's last connection time, which keeps
  the recent list of each computer its own.
- Settings that mean the same on another computer: language, appearance,
  the General, Mouse, Shortcuts and Terminal pages, the font, the color
  scheme, the background's fit and opacity, and the file icon theme when
  it is one every copy has (Material, Seti or minimal). Pane sizes, the
  download folder, the background image itself and icon themes installed
  from a VSIX stay on each computer.
- With "Include saved passwords" checked, off by default, the passwords and
  key passphrases of the hosts that remember theirs. A computer that has it
  off keeps the passwords already in the file as they are. Private key
  files are not synced; hosts keep their key's path.

Each sync merges three ways: this computer's data, the file's, and the
data both had after the last sync, kept in `sync-state.json`, still
encrypted. Each folder, host, setting and password is merged on its own.
What changed on one side only is kept; what was deleted on one side and
not changed on the other is deleted; what changed on both sides keeps this
computer's version, and the status says how many there were. The first
sync with a service (or after the sync password changed) deletes nothing:
it keeps what either side has. When another computer writes the file
during a sync, the sync reads it again and merges again (WebDAV and S3 send
`If-Match`, for the servers that support it; a gist is checked just before
writing). Before a sync changes
local connections, `hosts.json` is copied to `hosts.before-sync.json`.
Changes made while a sync is under way are merged on top of what it
brought, and synced next; a password saved meanwhile is kept, not
replaced by the one the sync brought. When the service or its fields
change during a sync, what it brought is dropped.

"Sync Now" syncs at once. "Sync automatically" syncs at start, a minute
after changes here (they are looked for once a minute) and every 5, 15
(the default), 30 or 60 minutes for changes made elsewhere. After an error
it waits for the interval. The status line shows the last sync's time and
what it did, or why it failed.

"Replace the Remote with This Computer's Data…" asks first, then writes
this computer's data to the file without merging, encrypted with the sync
password entered: the way to start over, or to change the sync password
(enter the new one, replace, then enter the new one on the other
computers). Without "Include saved passwords", the file then has no
passwords.

The sync password and the service's token, password or secret key are kept
in the system's credential store (see
[Where passwords are stored](configuration.md#where-passwords-are-stored));
in memory only when the system has none. The other fields are the `sync`
key of `settings.json` (see [Sync](configuration.md#sync)).

Not supported yet: Google Drive, OneDrive, signing in to GitHub in the
browser instead of a token, and restoring older versions of the file.

## Keyboard shortcuts

Defaults, the same as FinalShell's. All of them can be changed on the
Terminal › Shortcuts page.

| Action | macOS | Windows / Linux |
|---|---|---|
| Terminal: copy | Cmd+C | Ctrl+Shift+C |
| Terminal: paste | Cmd+V | Ctrl+Shift+V |
| Terminal: find | Cmd+F | Ctrl+Shift+F |
| Terminal: full screen | Cmd+Enter | Ctrl+Shift+Enter |
| Terminal: reconnect (while the session is disconnected and the terminal area has focus) | Enter | Enter |
| Previous tab | Cmd+[ | Ctrl+Shift+Tab |
| Next tab | Cmd+] | Ctrl+Tab |
| Close tab | Cmd+W | Ctrl+Shift+W |
| Connection manager | Cmd+O | Ctrl+Shift+O |
| New tab | Cmd+T | Ctrl+Shift+T |
| Switch to tab 1–9 | Cmd+1…9 | Alt+1…9 |
| Focus command bar (and back to the terminal) | Cmd+Shift+L | Ctrl+Shift+L |
| Switch sessions / connections (the bottom-left panel's pages) | Cmd+Shift+E | Ctrl+Shift+E |
| Show or hide the bottom panel | Cmd+J | Ctrl+Shift+J |

Notes:

- Ctrl+Shift+T used to open the connection manager. It now opens a new
  tab, as in other terminal apps; the new tab has the recent connections
  and a "Connection manager" button. The connection manager moved to
  Ctrl+Shift+O.
- On macOS, Close tab is Cmd+W, not FinalShell's Cmd+Q (which quits). So
  that Cmd+W reaches the window, the macOS menu bar drops MyGo's default
  File menu (its Close Window item holds Cmd+W). In the main window, Cmd+W
  closes the tab. In child windows (Options, the connection editor), Cmd+W
  closes the child window, as in other Mac apps (if "Close tab" was changed
  to another key, that key also closes child windows), and Esc closes them
  too. On Windows and Linux, child windows close with Esc and the system's
  close button. The file editor is the exception: Cmd+W (Ctrl+W) closes a
  tab, and Esc does not close the window (see
  [Remote file editor](#remote-file-editor)).
- On macOS, switching tabs uses only Cmd with a digit: Option with a digit
  types a character (¡, ™, £, ...).
- On Windows, the terminal plugin by default also binds Ctrl+C (copy when
  there is a selection) and Ctrl+V (paste when the clipboard has
  something). Finallyshell removes those, leaving them to the shell as on
  Linux (interrupt, and insert the next key literally). Copy and paste with
  Ctrl+Shift+C and Ctrl+Shift+V, or Ctrl+Insert and Shift+Insert.
- Focus command bar is not Ctrl+Shift+K: on Windows that is the
  terminal's clear screen (as in Windows Terminal). FinalShell switches
  with a lone Ctrl press, which would clash with every Ctrl key the shell
  uses, so it isn't copied. With the command bar off, the key goes to the
  terminal.
- Switch sessions / connections is E as in VS Code's Explorer, which
  shows its side bar's tree; no terminal binds Ctrl+Shift+E. On macOS it
  takes Shift too, as Cmd+E is "Use Selection for Find" in Mac apps.
  Older versions of IBus on Linux use Ctrl+Shift+E for emoji input and
  take it before the app; change the key on the Shortcuts page, or in
  IBus's preferences, if it does nothing.
- Show or hide the bottom panel is J as in VS Code. Ctrl+J alone is a
  newline in the shell, so on Windows and Linux it takes Shift too. It
  does nothing in full screen and on a panel opened in a tab of its own.
- Letters with only Ctrl (Ctrl+A to Ctrl+Z and so on) all belong to the
  shell and are not used as shortcuts on Windows and Linux. In MyGo's
  legacy keyboard mode, Ctrl+[, Ctrl+I and Ctrl+M send nothing; that is an
  upstream issue (#21), and the app does not work around it.

## Layout and split dividers

The dividers between the areas of the window can be dragged to resize them
(or moved with the arrow keys once they have focus), and double-clicking a
divider restores the default size. Every area has a minimum size, so it
cannot be dragged out of sight. The sizes are saved in
`finallyshell/settings.json` and restored at the next start. This includes
the dividers of the Processes and Network tabs, which every session
shares, in the bottom panel and in a panel opened in a tab of its own.

The bottom panel can also collapse to its row of tabs (see
[Window layout](#window-layout)); the divider's position is kept for
when it opens again, and whether it is collapsed is saved with the sizes.

When the bottom panel is on the right, the divider between the terminal
and the column sets the column's width, from 410 DIPs (what the panes
inside the panel need, the Processes details' two halves the widest)
up to what leaves the terminal 320 DIPs; a double click puts it back at
460. The width is saved with the other sizes, and the terminal's
divider under it keeps its position for when the panel goes back to the
bottom.

With the monitor on the right as well as the bottom panel, the divider
between them sets the monitor's share of the column's height, 45% by
default, leaving the monitor its IP line and the panel its tabs and a row
of its table; a double click puts it back at 45%. It is saved with the
other sizes. The column's width is the same whichever of them is in it.

## Language

The UI is in English and Simplified Chinese. By default it follows the
system language. Change it on the General page of the Options window
(the gear button next to the "Sessions" title at the bottom left); the
choice is saved.

## Updates

Finallyshell updates itself from the releases on GitHub, with MyGo's
signed updates and its update window (the updater plugin, in native UI).

- Checking: 10 seconds after launch, then once a day while the app runs,
  it reads the update manifest of the latest release
  (`update-<platform>.json`, one small file from
  `github.com/TooonyChen/finallyshell/releases/latest/download/`, not the
  GitHub API). "Check for updates automatically" in Options › General
  turns this off; it is on by default.
- When a newer version is out, a window offers **Install Update**,
  **Remind Me Later** and **Skip This Version**, and a check box to
  download and install updates without asking from then on. A skipped
  version is not offered again by the background checks; the next one is.
- Installing downloads the update with a progress bar: a small delta from
  the version running when the release has one, else the whole app. It
  checks the Ed25519 signature against the public key built into the app,
  replaces the app, and offers to relaunch; otherwise the new version runs
  at the next launch.
- "Check for Updates…" checks at once and says when the app is up to date
  or the check failed; it offers skipped versions too. It is in
  Options › General, in the tray icon's menu, and on macOS in the app
  menu (Finallyshell › Check for Updates…).
- Options › General also shows the version running and, once a check
  found one, "Version x.y.z is available".
- The window speaks the language the app shows (the Language option), and
  a window opened after the language changes takes the new one.

Where a copy can update itself:

| System | Install | Updates itself |
|---|---|---|
| macOS | The app in `/Applications` or `~/Applications` | Yes |
| Windows | The installer's per-user install in `%LOCALAPPDATA%\Programs`; the portable zip where its folder is writable | Yes |
| Linux | The `.tar.gz` installed with `install.sh` in `~/.local` | Yes |
| Linux | The `.deb` in `/opt` | No: the package manager updates it |
| Any | A development build (`go run`, `mygo dev`) | No |

Where it cannot, Options › General says why and links to the
[releases page](https://github.com/TooonyChen/finallyshell/releases)
instead of offering to check, and "Check for Updates…" in the menus opens
a window that says so.

Notes:

- Versions up to 0.2.0 have no updater: install the first version that
  has one by hand, once. The ones after it update themselves.
- The macOS app is signed ad hoc, so each build's signature differs. After
  an update, macOS may ask again whether Finallyshell may use the
  passwords it saved in the keychain, as it does after installing a new
  version by hand. This is not yet checked on a real Mac.
- The update window shows the new version's release notes, in English and
  Chinese, the same as its page on GitHub (`CHANGELOG.md`).
- Prereleases, notarization on macOS, Authenticode on Windows and an apt
  repository for the `.deb` are not supported.

The choices (automatic checks, installing without asking, the skipped
version, the time of the last check) are kept in `updater.json`; see
[configuration.md](configuration.md#updaterjson).
