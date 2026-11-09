# Development

English · [简体中文](../zh-CN/development.md)

How to build, test and release Finallyshell, and how the code is laid
out. The repository rules (issues first, branch names, commit messages,
pull requests) are in [CONTRIBUTING.md](../../CONTRIBUTING.md) and
[CLAUDE.md](../../CLAUDE.md).

- [Running](#running)
- [Tests](#tests)
- [Checks before a commit](#checks-before-a-commit)
- [Screenshots](#screenshots)
- [Releasing](#releasing)
- [Signing updates](#signing-updates)
- [Code structure](#code-structure)
- [Child windows](#child-windows)
- [Adding an options page](#adding-an-options-page)
- [Adding an import source](#adding-an-import-source)
- [Closing and quitting](#closing-and-quitting)
- [Single instance](#single-instance)
- [Adding a UI string](#adding-a-ui-string)

## Running

Finallyshell needs Go 1.27.3 or later. The UI is
[MyGo](https://github.com/egoist/mygo)'s native UI, and the terminal is
MyGo's terminal plugin, built on libghostty-vt, which is loaded at run
time, so no cgo is needed.

```sh
go tool mygo dev     # development mode, restarts when the code changes
go tool mygo build   # package the app into build/<platform>/
go test ./...
```

The first `go run` or `go test` downloads libghostty-vt into the user's
cache directory. Apps packaged by `mygo build` include the library.

## Tests

UI code is tested headless with `ui.NewTester`; `tt.Image()` returns a
frame of the window.

Integration tests against a real server (connecting, the terminal, the
monitor, drag-and-drop upload, download):

```sh
FINALLYSHELL_TEST_SSH=user:password@127.0.0.1:22 go test ./internal/app -run SSH -v
```

The tunnel tests (`TestTunnels` in `internal/sshx`, `TestSSHTunnels` in
`internal/app`) use the same variable. The server must be on this machine
(the ports of remote tunnels are connected to from this machine) and must
allow TCP forwarding (`AllowTcpForwarding yes`). Without the variable,
they use the in-process test server (`internal/sshtest`).

## Checks before a commit

These are the checks CI runs (`.github/workflows/ci.yml`) on every pull
request:

```sh
gofmt -l .           # must print nothing; go fmt ./... fixes it
go vet ./...
go tool mygo vet
go test -race ./...
```

`go fmt` uses the gofmt of the toolchain in `go.mod`; a `gofmt` on `PATH`
from another Go version can align comments differently.

Before the tests, CI runs `go run ./tools/natives`, which downloads
libghostty-vt into the user's cache. It keeps trying for up to five
minutes, waiting 2, 4, 8 and 16 seconds, then 30 seconds, between tries,
and then fails with the last error. GitHub sometimes answers the download
with 504 for minutes, and every test that opens a terminal would fail. CI
keeps the cache's `mygo/natives` folder (`~/.cache/mygo/natives` on the
Linux runner) between runs, keyed by `go.sum`, and saves it right after a
download, even if the tests then fail, so most runs don't download it at
all.

To look at what the UI tests draw, set `SHOT_DIR` to a folder and the tests
save their frames there:

```sh
SHOT_DIR=/tmp/shots go test ./internal/app
```

## Screenshots

The screenshots in `README.md` and `README.zh-CN.md` live in
`docs/screenshots/`, one per language (`-en.png`, `-zh-CN.png`).
Regenerate them with:

```sh
go run ./tools/screenshots
```

It runs the `internal/app` tests with `SHOT_DIR` set to a temporary
folder, then copies the chosen frames into `docs/screenshots/`. The tests
draw a window's content only, so the tool adds a macOS-style title bar
with the window's title, rounded corners, a thin border and a shadow.
The list of frames, their titles and crops is `shots` in
`tools/screenshots/main.go`. The main window scenes come from
`TestReadmeScreenshots` in `internal/app/readme_test.go`; the child window
scenes (connection manager, connection editor, options, import, file
editor) come from the existing tests of those windows.

## Releasing

A release is a version's section in `CHANGELOG.md`: write it, merge it,
then push main to the `production` branch.

1. Add the section on top of `CHANGELOG.md`, below the title, in the
   last pull request merged before the release or in one of its own:

   ```markdown
   ## 0.2.2

   ### English

   - …

   ### 简体中文

   - …
   ```

   The heading is the version, `MAJOR.MINOR.PATCH`, higher than the last
   `v*` tag; the owner chooses it. Both parts are needed: every release's
   notes are in English and Simplified Chinese. Write what users see and
   do, not the commit titles; `go run ./tools/release draft` lists the
   `feat`, `fix`, `perf` and breaking commits since the last tag to start
   from. Keep each bullet on one line, so no line break puts a space
   inside Chinese text. `TestChangelogFile` (tools/release) checks the file
   in every pull request: its headings are versions, newest first, and
   the newest section has both parts. The file starts at 0.2.1; earlier
   versions' notes are on their GitHub releases.
2. Merge it, then:

   ```sh
   git push origin origin/main:production
   ```

`.github/workflows/release.yml` then:

1. Works out the version (`go run ./tools/release version`): the newest
   section of `CHANGELOG.md`, when it is newer than the last `v*` tag.
   When that version is already tagged, there is nothing to release and
   the workflow stops here, so pushing main without a new section, or
   re-running the workflow, releases nothing. It fails, before anything
   is built, when the section is older than the last tag, a heading is
   not a version, or the section lacks its English or Chinese part. With
   no tag yet, the newest section is the first release.

   The commit types (Conventional Commits) since the last tag only check
   the version: a breaking change (`feat!:`, `BREAKING CHANGE:`) calls
   for a major bump (minor before 1.0), a `feat` for a minor one, `fix`
   and `perf` for a patch. A version lower than that is a warning on the
   run, and the release goes on: 0.2.1, after the `feat` commits since
   0.2.0, is one.
2. Runs the same checks as pull requests (`.github/workflows/ci.yml`:
   gofmt, `go vet`, `go tool mygo vet`, `go test -race`).
3. Builds for four platforms: macOS arm64 and Intel x86_64 on a macOS
   runner (ad hoc signed, dmg); Linux x86_64 and Windows x86_64
   cross-compiled on a Linux runner (the Windows installer uses NSIS
   installed with apt). The version
   is written into `mygo.json` only on the runner; the file on main is not
   changed. `LICENSE` and the third-party licenses
   (`go run ./tools/release licenses`) are put in `resources/` and packaged
   with the app.

   The build has the secret key `MYGO_UPDATER_PRIVATE_KEY` (see
   [Signing updates](#signing-updates)), so `mygo build` also writes each
   platform's update: the app as an archive
   (`finallyshell-<version>-<platform>.tar.gz`), deltas from the last
   three releases that have updates (`…-<from>-to-<version>-….delta`) and
   the manifest `update-<platform>.json`, which names them with their
   signatures. The manifest's notes, which the update window shows, are
   the version's section of `CHANGELOG.md`; `mygo build` fails without
   one. The job fails when the secret is missing, when there is no
   manifest, when the manifest is not this version in this release, or
   when its notes are not `go run ./tools/release notes <version>`, and
   otherwise copies the manifest and the files it names with the
   installers.

   `mygo build` downloads each target's native libraries (libghostty-vt)
   from GitHub into the cache's `mygo/natives` folder
   (`~/Library/Caches/mygo/natives` on the macOS runner,
   `~/.cache/mygo/natives` on the Linux ones), and gives up on a timeout.
   The job keeps that folder between runs, keyed by the runner's OS, the
   target and `go.sum`, and saves it after a build that succeeded. It runs
   the build through `.github/scripts/build-with-retries.sh`, which runs
   it again when it fails on a download (its output ends with
   `error: downloading …`), for up to ten minutes, waiting 15 and 30
   seconds, then 60 seconds, between tries, and then fails with the last
   error; a download
   done before the failure stays in the cache, so the next try fetches
   only what is missing. Any other error fails the build at once.
   `.github/scripts/build-with-retries-test.sh` tests the script against a
   fake `mygo`. One platform failing doesn't cancel the others
   (`fail-fast: false`); nothing is published unless all four are built.
4. Checks that the manifests of all four platforms are there, then tags
   `v<version>` and publishes a GitHub Release with the installers, the
   updates, `SHA256SUMS`, `LICENSE.txt` and `THIRD_PARTY_LICENSES.txt`.
   Its notes are the version's section
   (`go run ./tools/release notes <version>`), then
   `.github/release-install.md`, how to install, in English and Chinese.
   The release is published, not a draft, as installed apps read the
   manifests of the latest release; `gh release create` keeps it a draft
   while it uploads the files, so apps never see a manifest whose archive
   is not there yet.

`production` should only ever be fast-forwarded from main; protect it in
the repository settings (no force pushes).

## Signing updates

Installed apps accept only updates signed with the project's Ed25519 key
(MyGo's [auto-updates](https://github.com/egoist/mygo/blob/main/docs/updates.md)).
The owner made it with `mygo keygen` on 2026-11-08:

- The secret key is the repository secret `MYGO_UPDATER_PRIVATE_KEY`, and
  the owner keeps a copy. It is never committed, and never written to
  `updates.privateKey`. Losing it strands every installed copy at its
  version; anyone who has it can ship code to users.
- The public key is `updates.publicKey` in `mygo.json`; `TestUpdatesConfig`
  in the main package checks it is still there.
- Builds without the secret, such as `go tool mygo build` on your
  machine, still build the apps and skip the update files. Pull request CI
  does not build.

The app's side is `updates.go`: it uses the updater plugin's native UI
window (`native.New`) without a `Language`, so the window speaks
`App.Locale`, which follows the Language option, and gives
`internal/app` the state of the Options row through the `Platform`
methods `Updates`, `CheckForUpdates` and `SetAutomaticUpdateChecks`.
`updater.OnChange` runs on the UI thread and only redraws the windows.

To try an update without a release, build two versions with a throwaway
key and serve them from your machine:

1. `go tool mygo keygen -o /tmp/keys`, and in a copy of `mygo.json` set
   `updates` to that public key and `"url": "https://localhost:8443/u"`.
   A build with a key needs its version's section in `CHANGELOG.md`, so
   add `## 0.2.0` and `## 0.3.0` sections to a scratch copy, or point
   `updates.changelog` at a file that has them.
2. With `MYGO_UPDATER_PRIVATE_KEY="$(cat /tmp/keys/mygo-update.key)"`,
   build version 0.2.0 (`go tool mygo build -o dist-old`), serve its
   archive and manifest over HTTPS from `u/` with a self-signed
   certificate for `localhost`, then build 0.3.0, which makes a delta from
   the served 0.2.0. Serve its archive, delta and manifest.
3. Extract the 0.2.0 archive to `~/.local/finallyshell.app` (under a
   separate `HOME`) and start it with `SSL_CERT_FILE` naming a bundle that
   includes the certificate, and `NO_PROXY=localhost`. The update window
   shows after 10 seconds. `{"automaticDownloads": true}` in
   `updater.json` makes it install without asking.

Delete the throwaway key afterwards.

## Code structure

In Go a folder is a package, and packages cannot import each other in a
cycle, so the code is split by responsibility. Packages under `internal/`
can only be used by this project.

```
main.go              starts MyGo and opens the main window; hands system features (child windows, dialogs, download folder, language) to app
tray.go              tray icon, the confirmation message box, quitting
updates.go           the update window (MyGo's updater plugin, native UI), the Updates row's status, the macOS app menu with "Check for Updates…"
keychain.go          the system credential store (mygo.Secrets): Platform's SecretGet/Set/Delete
locales/             UI strings: en.json, zh-Hans.json; catalog_gen.go is generated by mygo, do not edit it by hand
tools/natives/       downloads libghostty-vt for the tests before CI runs them, trying again for up to five minutes (see Tests)
tools/release/       release tools: the next version and its notes from CHANGELOG.md, a draft of them from the commits, the third-party licenses (notices/ has the licenses of libraries packaged as binaries)
tools/screenshots/   regenerates the README screenshots in docs/screenshots/ from the internal/app tests
internal/
  config/            reading and writing hosts and folders (hosts.json) and settings, the background image copy, the config directory; proxy.go is the proxy settings and the jump host chain (finding loops); tunnels.go is the tunnel settings; keys.go is what Backspace and Delete send; sync.go is the Sync page's settings, which settings are synced, sync-state.json and the copy of hosts.json before a sync; history.go is the command bar's history files and reading bash's and zsh's history files; quickcommands.go is the Commands tab's quickcommands.json
  cloudsync/         sync through the user's own storage: crypto.go encrypts the file (Argon2id, AES-256-GCM), merge.go merges three ways, sync.go reads, merges and writes it (retrying on a conflict); gist.go, webdav.go and s3.go are the services (s3.go signs with SigV4 by hand), remotehttp.go their shared HTTP code
  importer/          reads other clients' connections: importer.go is the list of sources (readers), the entries and problems they read, the text decoder (UTF-8, UTF-16, GB18030), finding duplicates and importing by folder; finalshell.go finds FinalShell's folder and reads the connections and folders in conn; xshell.go finds Xshell's Sessions folder and reads its .xsh files and .xts exports; securecrt.go finds SecureCRT's config folder and reads the .ini files of its Sessions folder; mobaxterm.go finds MobaXterm.ini and reads its bookmark sections, and .mxtsessions and .moba exports; sshconfig.go reads OpenSSH's ~/.ssh/config (Host blocks and patterns, Include, ProxyJump); table.go reads a server list in a CSV file, or the first sheet of an .xlsx with xlsx.go, a reader of its own (archive/zip and encoding/xml, with limits against zip bombs); testdata has made-up samples
  sshx/              SSH connection, authentication, known_hosts, the Conn the terminal uses; proxy.go connects through SOCKS5, HTTP CONNECT and jump hosts; forward.go is port forwarding (-L/-R/-D) and the SOCKS5 server
  termenc/           converting a terminal's connection between the server's encoding (GBK, Big5, ...) and the UTF-8 the terminal reads
  sshtest/           in-process SSH server for tests: password login, an empty shell, port forwarding; StartSFTP also serves SFTP (the local file system)
  monitor/           collecting and parsing server status; net.go is every network interface's speeds and history, and the sum of those chosen; netstat.go is the Network tab's sockets (ss, or /proc/net and /proc/<pid>/fd), their rates and rows; trace.go is trace route over an exec channel (NextTrace's RAW output, traceroute, tracepath)
  geo/               where IP addresses are: ip2region's data through its Go binding (xdb.go), downloading it from GitHub or jsDelivr against its checksum (download.go), the online lookups on ip-api.com and ip.sb (online.go) and their cache (cache.go); geotest/ makes small .xdb files for tests
  remotefs/          SFTP listing, upload, download (with progress and cancel), rename, mkdir, create and delete (ops.go); textfile.go is the editor's reading (detecting binary files) and safe saving; archive.go is archive transfers: packing and safely unpacking tar.gz streams, and running tar over an exec channel
  icons/             embedded SVG icons (Lucide, ISC license)
  fileicons/         VS Code file icon themes: reading VSIX files and extension folders, finding icons by name, font conversion, installing and finding installed themes; bundled/ has the built-in Material and Seti, languages_gen.go is VS Code's language table, internal/gen generates them
  widgets/           small reusable widgets: icon buttons, usage bars, line charts, form rows, side navigation with pages (nav.go)
  app/               UI and state
    app.go           App state, overall layout, opening and closing sessions
    session.go       one session: connection, terminal, monitor, SFTP; translating error messages
    sidebar.go       left side: monitor panel, vertical session list
    conntree.go      the left panel's Connections page: the switch between its pages, the connection manager's tree in one column, the folders kept open
    paneltab.go      panel rows: a session's bottom panel in a row of the list of its own, sharing the session's connection (connSession)
    netifaces.go     the network graph's interface menu: its label, choosing interfaces and keeping the choice per host
    sysinfo.go       the "System info" popover: reading, loading and error states, copying
    terminal.go      terminal area
    findbar.go       the find bar on the terminal, driving the plugin's Search
    commandbar.go    the command bar under the terminal: its keys, the path, command and history popups, words and fields
    commandbar_data.go what the popups read: folders over SFTP, command names over an exec channel (or SFTP), the hosts' history, the server's shell history
    options_commandbar.go the Terminal › Command bar page
    fullscreen.go    full screen: only the terminal area, window in the system's full screen
    start.go         new tab: quick connect, search, clearing recent connections
    splits.go        split dividers: minimum sizes of the areas, saving and restoring sizes
    files.go         file panel: folder tree, file table, drop to upload
    fileops.go       rename, delete, new folder and file: their dialog and the tree's menu
    fileicons.go     icon themes in the file panel: background loading, showing icons, installing and removing
    options_fileicons.go the File manager › File icon theme page
    transfers.go     transfer queue, the Transfers tab, and transferRow shared by both places
    archive.go       archive transfers in the file panel: their menu items, their transfer rows, "Retry over SFTP"
    downloads.go     the Downloads popover, download folder, retry and open, count and progress on the app icon (Platform.ShowTransfers), the Transfers page of the options
    fileeditor.go    editing remote files: tabs, reading, saving, conflicts, asking before closing, opening with the default app
    fileeditor_view.go, fileeditor_text.go
                     the editor window's UI (tabs, toolbar, notices, status bar, questions), encodings and line endings, detecting the language from #!
    tunnels.go       a session's tunnels: started after connecting, stopped on disconnect, the Tunnels tab
    network.go       the Network tab: the table of listening sockets and processes, the tabs of their connections
    network_trace.go trace route in the Network tab: its tabs, the missing NextTrace notice, the dialog for a typed address
    quickcmds.go     the Commands tab: groups, the buttons of quick commands, sending them through the command bar's sendLine, dragging, the context menu
    quickcmds_editor.go the quick command editor's window
    geo.go           IP locations: the download bar, loading the data off the UI thread, the Location column's text, the online lookups' queue
    options_network.go the Network monitor page of the options
    manager.go       connection manager window: folder tree, search, dragging, delete and restore, a.deleteHostForever(h); the tree's state (managerState) and actions, which the Connections page shares
    import.go        the "Import connections" window: choosing the source, finding or choosing its file or folder, preview and checkboxes, showing the result in the connection manager, a list's passwords into the keychain
    import_sources.go the import window's texts of each source (importTexts), and of the reasons and notes the sources give
    import_table.go  a server list's parts of the import window: the template "Save template…" writes in the app's language, the texts of the list's errors
    dialogs.go       password, host key and replace confirmations
    options.go       options window: the list of pages and the General page
    options_appearance.go the Appearance row of the General page: light, dark or the system's
    palette.go       the app's light and dark themes, the colors besides the theme's for each, and useTheme, which every window calls to pick them by the frame's appearance
    options_updates.go the Updates row of the General page: version, checking, why a copy cannot update
    keyseq.go        the Terminal page of the options (default encoding, Backspace and Delete), the choices the connection editor shares, and the key handlers that send them
    mouse.go         the Terminal › Mouse page, and how its settings become terminal options
    shortcuts.go     shortcuts: actions and their defaults per platform, keys that cannot be used, conflicts, dispatching window shortcuts, the terminal's Keybinds
    shortcutspage.go the Terminal › Shortcuts page and the key recorder
    keycombo.go      key combinations: how they are written in settings.json and shown in the UI (⌘⇧ symbols on macOS)
    appearance.go    terminal appearance: how font, color scheme and background image become terminal options; drawing, copying and deleting the background
    bgimage.go       reading the background image: size limits, scaling down, EXIF orientation
    options_font.go, options_colors.go, options_background.go
                     the Terminal › Font, Color scheme and Background image pages and their previews
    editor.go        connection editor window: a.newConnection(), a.editConnection(h), validation and saving
    editor_proxy.go  the Proxy page of the connection editor
    editor_tunnels.go the Tunnels page of the connection editor: the table and the add/edit dialog
    proxy.go         connecting through proxies and jump hosts: dial options, the passwords to ask for before connecting, translating each hop's errors
    sync.go          syncing: the payload of hosts and settings, reading the saved passwords on the secret queue, automatic sync from the main window's frames, applying what a sync brought (merging again what changed meanwhile)
    options_sync.go  the Sync page of the options
    secrets.go       remembering passwords: credential store keys, the background read/write queue, "Remember password", the password prompt, passwords on permanent delete and duplicate
    closing.go       what closing the main window and quitting do: hiding to the tray, asking first
    windows.go       child windows: opening, focusing and closing by kind, redrawing every window
    format.go        formatting sizes, speeds and latency
```

Dependencies go one way: `app` uses every other package, `widgets` uses
only `icons`, and the other packages do not depend on the UI or on each
other.

## Child windows

As in FinalShell, settings-type dialogs are separate windows (MyGo child
windows, `WindowOptions.Parent`), not overlays inside the main window. They
are currently the Options window, the connection manager, the connection
editor, the "Import connections" window and the file editor. All are
child windows of the main window, except the connection editor and the
import window when opened from the connection manager: those are modal
child windows of the connection manager, as in FinalShell. As a result:

- On macOS the editor is a sheet of the connection manager. It covers the
  connection manager and does not block the main window. A sheet has no
  title bar, so on macOS modal windows show their title ("Edit connection"
  or "New connection") at the top of the window (`sheets` in
  `windows.go`). The connection manager is 900×640 by default, larger than
  the editor, so the sheet fits.
- On Windows only the connection manager is disabled; the main window can
  still be used.
- On Linux (GTK) a modal window blocks every window of the app, so while
  the editor is open the main window's terminal does not take input either.
  FinalShell's own dialogs (Swing's default application-modal dialogs)
  behave the same way, and editing a connection takes little time, so this
  is accepted.
- An editor opened from the main window ("New connection" on a new tab) is
  still a normal child window of the main window, whether or not the
  connection manager is open. Otherwise, on macOS, it would become a sheet
  of another window that might be behind.

How child windows work:

- Only one window of each kind (`WindowKind`) is open at a time:
  `a.openWindow(spec, view)` brings it to the front if it is already open.
  `WindowSpec` gives the title (a function from locales, fetched again when
  the language changes), the content size, the minimum size, the parent
  window (the main window by default) and whether it is modal.
- Esc in a window (and Cmd+W on macOS), or `a.closeWindow(kind)`, closes
  it. Closing a parent window closes its child windows; closing the main
  window closes all child windows. `WindowSpec.OwnKeys` leaves Esc and
  Cmd+W to the window itself (the file editor uses Cmd+W to close a tab).
  `CloseRequested` asks the window first when the user clicks the close
  button, and the window stays open if it returns false (the editor asks
  about unsaved changes); the app closing a window itself does not ask.
  `Closed` is called after the window has closed. When the title changes
  (the editor switches tabs), call `Platform.RetitleWindow`.
- All windows share one `App`, and all run on the UI thread. A window is
  redrawn only after its own input. After changing state that other
  windows also show (saved hosts, settings), call `a.redraw()` to redraw
  every window. Work posted with `App.post` also redraws every window when
  it is done.
- In tests, `fakePlatform` records the windows that are opened, and
  `p.show(t, kind)` draws a window's content with a separate `ui.Tester`.

Behavior per platform (confirmed by reading the MyGo v0.8.1 backend code,
not yet tried one by one on real machines):

| | Normal child window | Modal (`Modal`) |
|---|---|---|
| macOS | A normal `NSWindow`, always above its parent, hidden along with the parent when the parent is minimized | A sheet of the parent (slides out below the title bar, has no title bar of its own, positioned by the system); the same when the parent is itself a child window |
| Windows | A window owned by the parent (owner), always above it, with no taskbar button of its own | The parent is disabled while it is open (`EnableWindow`); only the direct parent |
| Linux | GTK `transient_for` the parent | `gtk_window_set_modal`: GTK blocks input to every other window of the app, not only the parent. Under Wayland, the compositor decides window positions |

Child windows open centered over their parent and cannot be minimized.

## Adding an options page

The pages are listed in `optionPages` in `internal/app/options.go`, in tree
order, with each parent page before its children. A page whose `view` is
`nil` shows "These options are coming in a later version."

To give a page content, write `func (a *App) fontPage(c *ui.Context)` in a
new file and change that entry's `view: nil` to `view: (*App).fontPage`.
For a new page, add an entry
`{id: ..., parent: ..., title: locales.Xxx, view: ...}`. Each entry takes
several lines, so several people changing different pages do not touch the
same lines.

On a page, change settings with `a.changeSettings(func() { ... })`, which
saves them and redraws every window.

## Adding an import source

The import window lists the sources of `internal/importer`, in the order
of `readers` in `importer.go`. A source is a `Kind` and a `Reader`:

```go
const Xshell Kind = "xshell"

var xshellReader = &Reader{
	Kind:   Xshell,
	Folder: true,             // the user chooses a folder; else a file,
	                          // of Extensions: []string{"csv"} (none: any)
	Find:   FindXshell,       // the default file or folder, "" for none; nil for none at all
	Read:   ReadXshell,       // func(path string) (*Source, error)
}
```

To add one:

1. `internal/importer/<source>.go`: the `Kind` constant, the `Reader`,
   its `Find` and `Read`, and a comment on what is known of the format and
   where it comes from. `Read` returns a `Source` with its `Kind` and the
   `Path` read; each connection is an `Entry`: `Host` (no ID, no folder),
   `Folder` (nil for the top level), `File` (`At(file, line)` for a line),
   `KeyFile` for a key file the source names, `NeedsKey` for a key kept in
   the client's own store, `Jump` for the entry of its jump host, `Note`
   for a setting not imported, `Password`
   only for a list the user wrote (it goes to the keychain, never to
   `hosts.json`). What it cannot read is a `Problem` (`NewProblem`,
   `ReadProblem`); with no connection at all, it returns
   `ErrNoConnections`. A `Reason` or `Note` of its own is a constant in
   this file; a reason for a connection skipped on purpose (not failed)
   is added to `skipped` in an `init` of this file:
   `func init() { skipped[Telnet] = true }`. `ReadText` reads a text file in whatever encoding
   it is in, and `sortSource` sorts the result as FinalShell's is. Tests
   go in `<source>_test.go`, made-up samples in `testdata/<source>/`.
2. One line in `readers`, in `importer.go`, where the window lists it.
3. One entry in `importTexts`, in `internal/app/import_sources.go`: its
   name in the list, the chooser's title, the name of its file types (for
   a file with extensions), what to say when nothing is found, and the
   note above the list (`locales.ImportPasswordsNote(name)` for a client
   that encrypts passwords). A new `Reason` or `Note` gets its text in
   `importProblemText` or `importNoteText` there. Optionally, `errorText`
   says the source's own errors in words of the app (else the window
   shows "Could not read it" and the error), and `template` is a file for
   "Save template…" to write, for a source the user writes, as the server
   list's (`import_table.go`). The strings go in both locales (see
   [Adding a UI string](#adding-a-ui-string)).

`TestImportSourcesListed` fails for a source without texts. Nothing else
in `import.go` changes for a source.

## Closing and quitting

| | Close button | Quit (tray menu, Cmd+Q, SIGINT) |
|---|---|---|
| Tray on (and the system has a tray) | Hide to the tray, without asking | If sessions are open and confirmation is on: ask first |
| Tray off | If sessions are open and confirmation is on: ask first, then quit | Same |

"Sessions are open" means connected or connecting. The logic is in
`closing.go` (`CloseRequested`, `QuitRequested`, tested with a fake
Platform), and `main.go` calls it from the main window's `OnClose` and
from `App.OnBeforeQuit`. The confirmation is `mygo.Dialog.Message`; a
goroutine waits for the answer, and calls `mygo.App.Quit()` once the user
answers Quit.

Behavior per platform, confirmed by reading the MyGo v0.8.1 code and not
tried on real machines:

- macOS: the tray icon is in the menu bar, as a template image (it follows
  the menu bar's color), and clicking it opens the menu. The Dock icon is
  always there, and clicking it (`OnActivate`) shows the hidden window. The
  confirmation is a sheet of the main window (a separate dialog when the
  window is hidden).
- Windows: left-clicking the icon shows the window, right-clicking opens
  the menu, and the template image is drawn white on a dark taskbar.
- Linux: uses AppIndicator, which needs `libayatana-appindicator3`.
  Without it, `NewTray` returns an error and the window is not hidden.
  GNOME does not show AppIndicator icons by default (an extension is
  needed); the icon is then created but invisible. Starting the app again
  brings the window back (see below).

## Single instance

Only one copy of the app runs at a time
(`mygo.App.RequestSingleInstanceLock`). When it is started again, the new
process hands its command line to the running one and exits, and the
running app shows its main window (restoring it from the tray or from
being minimized) and brings it to the front. This also keeps two processes
from writing `hosts.json` and `settings.json` at the same time.

- The lock is per app name (plus the identifier once packaged), so the
  `mygo dev` build and the installed app do not affect each other.
- The lock is released when the app exits, and a lock file left by a crash
  is cleaned up at the next start.
- Under Wayland, the window gets focus through the activation token the new
  process brings. On Windows, the new process hands foreground rights to
  the running app. On macOS, opening the app from Finder or the Dock only
  ever opens one copy anyway; clicking the Dock icon goes through
  `OnActivate`, which also shows a window hidden in the tray.

## Adding a UI string

1. Add the key and the English text to `locales/en.json` (placeholders go
   in `@key.placeholders`), and the Chinese text to `locales/zh-Hans.json`.
2. Run `go tool mygo generate` (`mygo dev` does this automatically), and
   use `locales.Key(...)` in the code. Commit the regenerated
   `locales/catalog_gen.go`.

The format is ICU MessageFormat; see MyGo's
[i18n documentation](https://github.com/egoist/mygo/blob/main/docs/i18n.md).
