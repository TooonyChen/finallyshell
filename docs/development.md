# Development

How to build, test and release Finallyshell, and how the code is laid
out. The repository rules (issues first, branch names, commit messages,
pull requests) are in [CONTRIBUTING.md](../CONTRIBUTING.md) and
[CLAUDE.md](../CLAUDE.md).

- [Running](#running)
- [Tests](#tests)
- [Checks before a commit](#checks-before-a-commit)
- [Screenshots](#screenshots)
- [Releasing](#releasing)
- [Code structure](#code-structure)
- [Child windows](#child-windows)
- [Adding an options page](#adding-an-options-page)
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

Pushing main to the `production` branch is a release:

```sh
git push origin origin/main:production
```

`.github/workflows/release.yml` then:

1. Works out the version (`go run ./tools/release version`) from the
   commit types (Conventional Commits) since the last `v*` tag:
   - a breaking change (`feat!:`, `BREAKING CHANGE:`) bumps the major
     version, or the minor version before 1.0;
   - a `feat` bumps the minor version;
   - only `fix` and `perf` bump the patch version;
   - only `docs`, `chore`, `test`, `ci`, `build`, `style` and `refactor`
     make no release, and the workflow stops here.

   With no tag yet, the version in `mygo.json` is used (the first release
   was 0.2.0). A commit that already has a tag is not released again, so
   re-running the workflow is safe.
2. Runs the same checks as pull requests (`.github/workflows/ci.yml`:
   gofmt, `go vet`, `go tool mygo vet`, `go test -race`).
3. Builds for three platforms: macOS arm64 on a macOS runner (ad hoc
   signed, dmg); Linux x86_64 and Windows x86_64 cross-compiled on a Linux
   runner (the Windows installer uses NSIS installed with apt). The version
   is written into `mygo.json` only on the runner; the file on main is not
   changed. `LICENSE` and the third-party licenses
   (`go run ./tools/release licenses`) are put in `resources/` and packaged
   with the app.
4. Tags `v<version>` and publishes a GitHub Release with the installers,
   `SHA256SUMS`, `LICENSE.txt` and `THIRD_PARTY_LICENSES.txt`. The release
   notes are grouped by commit type (`go run ./tools/release notes`).

`production` should only ever be fast-forwarded from main; protect it in
the repository settings (no force pushes).

## Code structure

In Go a folder is a package, and packages cannot import each other in a
cycle, so the code is split by responsibility. Packages under `internal/`
can only be used by this project.

```
main.go              starts MyGo and opens the main window; hands system features (child windows, dialogs, download folder, language) to app
tray.go              tray icon, the confirmation message box, quitting
keychain.go          the system credential store (mygo.Secrets): Platform's SecretGet/Set/Delete
locales/             UI strings: en.json, zh-Hans.json; catalog_gen.go is generated by mygo, do not edit it by hand
tools/release/       release tools: works out the next version, writes the release notes, collects third-party licenses (notices/ has the licenses of libraries packaged as binaries)
tools/screenshots/   regenerates the README screenshots in docs/screenshots/ from the internal/app tests
internal/
  config/            reading and writing hosts and folders (hosts.json) and settings, the background image copy, the config directory; proxy.go is the proxy settings and the jump host chain (finding loops); tunnels.go is the tunnel settings; keys.go is what Backspace and Delete send
  importer/          reads other clients' connections: finalshell.go finds FinalShell's folder, reads the connections and folders in conn, finds duplicates, imports by folder; testdata has made-up samples
  sshx/              SSH connection, authentication, known_hosts, the Conn the terminal uses; proxy.go connects through SOCKS5, HTTP CONNECT and jump hosts; forward.go is port forwarding (-L/-R/-D) and the SOCKS5 server
  termenc/           converting a terminal's connection between the server's encoding (GBK, Big5, ...) and the UTF-8 the terminal reads
  sshtest/           in-process SSH server for tests: password login, an empty shell, port forwarding; StartSFTP also serves SFTP (the local file system)
  monitor/           collecting and parsing server status
  remotefs/          SFTP listing, upload, download (with progress and cancel); textfile.go is the editor's reading (detecting binary files) and safe saving
  icons/             embedded SVG icons (Lucide, ISC license)
  fileicons/         VS Code file icon themes: reading VSIX files and extension folders, finding icons by name, font conversion, installing and finding installed themes; bundled/ has the built-in Material and Seti, languages_gen.go is VS Code's language table, internal/gen generates them
  widgets/           small reusable widgets: icon buttons, usage bars, line charts, form rows, side navigation with pages (nav.go)
  app/               UI and state
    app.go           App state, overall layout, opening and closing sessions
    session.go       one session: connection, terminal, monitor, SFTP; translating error messages
    sidebar.go       left side: monitor panel, vertical session list
    sysinfo.go       the "System info" popover: reading, loading and error states, copying
    terminal.go      terminal area
    findbar.go       the find bar on the terminal, driving the plugin's Search
    fullscreen.go    full screen: only the terminal area, window in the system's full screen
    start.go         new tab: quick connect, search, clearing recent connections
    splits.go        split dividers: minimum sizes of the areas, saving and restoring sizes
    files.go         file panel: folder tree, file table, drop to upload
    fileicons.go     icon themes in the file panel: background loading, showing icons, installing and removing
    options_fileicons.go the File manager › File icon theme page
    transfers.go     transfer queue, the Transfers tab, and transferRow shared by both places
    downloads.go     the Downloads popover, download folder, retry and open, count and progress on the app icon (Platform.ShowTransfers), the Transfers page of the options
    fileeditor.go    editing remote files: tabs, reading, saving, conflicts, asking before closing, opening with the default app
    fileeditor_view.go, fileeditor_text.go
                     the editor window's UI (tabs, toolbar, notices, status bar, questions), encodings and line endings, detecting the language from #!
    tunnels.go       a session's tunnels: started after connecting, stopped on disconnect, the Tunnels tab
    manager.go       connection manager window: folder tree, search, dragging, delete and restore, a.deleteHostForever(h)
    import.go        the "Import from FinalShell" window: finding or choosing the folder, preview and checkboxes, showing the result in the connection manager
    dialogs.go       password, host key and replace confirmations
    options.go       options window: the list of pages and the General page
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
editor, the "Import from FinalShell" window and the file editor. All are
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
