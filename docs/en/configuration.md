# Configuration files

English · [简体中文](../zh-CN/configuration.md)

Finallyshell keeps its hosts and settings in plain JSON files. Passwords
are never written to them; they go to the system's credential store. This
page describes the files. [features.md](features.md) describes the options
behind each setting.

- [Config directory](#config-directory)
- [hosts.json](#hostsjson)
  - [Tunnels](#tunnels)
  - [Proxy](#proxy)
  - [Folders, deleted hosts and other fields](#folders-deleted-hosts-and-other-fields)
  - [Safety](#safety)
- [settings.json](#settingsjson)
- [Sync](#sync)
- [updater.json](#updaterjson)
- [Updates in mygo.json](#updates-in-mygojson)
- [Where passwords are stored](#where-passwords-are-stored)

## Config directory

All files live in `<user config dir>/finallyshell/`, where the user config
dir is Go's `os.UserConfigDir()`: `~/Library/Application Support` on macOS,
`%APPDATA%` on Windows, and `$XDG_CONFIG_HOME` or `~/.config` on Linux. If
the environment variable `FINALLYSHELL_DIR` is set, that directory is used
instead.

| File | Contents |
|---|---|
| `hosts.json` | Saved hosts and folders (see below). |
| `hosts.v1.json` | The original file, kept when a version 1 `hosts.json` is migrated. |
| `hosts.json.broken-<date-time>` | A copy of a `hosts.json` that could not be read. |
| `settings.json` | The options (see below). |
| `known_hosts` | Host keys you have trusted. |
| `background-<time>.<extension>` | The copy of the terminal background image. |
| `icon-themes/` | File icon themes installed from VSIX files, as `<publisher>.<name>.vsix`. |
| `sync-state.json` | What the last sync synced (see [Sync](#sync)). |
| `hosts.before-sync.json` | `hosts.json` as it was before the last sync that changed it. |

`mygo dev`, `go run` and the installed app all use the same directory, so
they share hosts and settings.

Files the editor downloads to open with the default app go to the cache
directory instead: `<cache dir>/finallyshell/opened/<time>/`. Folders older
than a day are deleted the next time a file is opened this way.

## hosts.json

Since version 2, `hosts.json` is an object. Version 1 was an array of
hosts. A version 1 file is migrated automatically the first time it is
read: the original is kept as `hosts.v1.json`, and the next save writes
version 2.

```json
{
  "version": 2,
  "folders": [{"id": "f1", "name": "Production"}, {"id": "f2", "name": "Databases", "parent": "f1"}],
  "hosts": [{
    "id": "h1", "name": "web", "addr": "203.0.113.10", "port": 2222, "user": "root",
    "auth": "key", "keyPath": "~/.ssh/id_ed25519",
    "folder": "f2", "notes": "notes", "noMonitor": true,
    "termType": "vt220", "backspace": "ctrl-h", "noRememberPassword": true,
    "monitorIfaces": ["eth0", "wg0"],
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

### Tunnels

`tunnels` are the host's port forwards:

- `kind` is `local` (`-L`), `remote` (`-R`) or `dynamic` (`-D`, which has
  no `targetHost` or `targetPort`).
- Without `listenHost`, the tunnel listens on `127.0.0.1` only.
- With `disabled` set to `true`, the tunnel does not start with the
  session.
- Unknown fields in a tunnel are kept as they are.

The listen address of a remote tunnel must be an IP address or
`localhost`; see [Tunnels](features.md#tunnels-page).

### Proxy

`proxy` says how the host is reached:

- `kind` is `socks5` or `http`, which have `addr` and `port`, and `user`
  when the proxy needs authentication;
- or `kind` is `jump`, and `jumpHost` is the ID of the jump host.

Without `proxy`, the host is connected directly. Proxy passwords are not
written to the file either; like host passwords, they are kept in the
credential store (see [Where passwords are stored](#where-passwords-are-stored)).

### Folders, deleted hosts and other fields

- `folder` is the ID of the folder the host is in; empty means the top
  level. Folders nest through `parent`.
- A host with `deletedAt` (the time it was deleted) is deleted. It is
  listed only when "Show deleted" is checked in the connection manager.
- `createdAt` is when the host was added. Quick connect sorts the hosts that
  were never connected by it. Hosts saved by older versions do not have it.
- `lastConnectedAt` is when the host last connected, used for the "Recent"
  list of quick connect.

Fields with empty values are not written. Missing fields mean:

| Field | Missing means | Set means |
|---|---|---|
| `noMonitor` | The server monitor is on. | `true`: no monitor, and no extra exec channel. |
| `monitorIfaces` | The network graph shows the busiest interface. | A list of interface names, such as `["eth0", "wg0"]`: the graph shows their sum. Names the server does not have are skipped, and with none left the graph shows the busiest. |
| `termType` | `xterm-256color` | The `TERM` sent to the server. |
| `backspace` | The global setting (`termBackspace`). | `del`: Backspace sends `^?`; `ctrl-h`: it sends `^H`. |
| `delete` | The global setting (`termDelete`). | `vt220`: Delete sends `ESC [3~`; `del`: `^?`; `ctrl-h`: `^H`. |
| `encoding` | The global setting (`termEncoding`). | `utf-8`, `gbk`, `gb18030`, `big5`, `shift_jis`, `euc-jp`, `euc-kr`, `iso-8859-1` or `windows-1252`; unknown values mean UTF-8. |
| `noRememberPassword` | Passwords are remembered. | `true`: "Remember password" was turned off. |

### Safety

- Unknown fields (written by a newer version) are kept as they are, and are
  not lost on save.
- Writes go to a temporary file first, which is then renamed, so a half
  written file is never left behind.
- If `hosts.json` cannot be read (for example after a bad hand edit), the
  app first copies it as is to `hosts.json.broken-<YYYYMMDD-HHMMSS>` in the
  same folder, then starts with an empty list. The next save therefore
  cannot destroy the original content.

## settings.json

Every option is saved here as soon as it changes. Keys are written only
when they differ from the default, so a missing key always means the
default.

| Key | Meaning | Default (when missing) |
|---|---|---|
| `language` | UI language: `en` or `zh-Hans`. | Follow the system. |
| `appearance` | `light` or `dark` forces the app's appearance; `system` follows the system's. | Follow the system (also for unknown values). |
| `noAutoSelectTab` | `true` opens new sessions in the background. | New sessions are selected. |
| `minimizeToTray` | `true` hides the main window to the tray when it is closed. | Closing quits. |
| `noConfirmClose` | `true` closes and quits without asking while sessions are open. | Ask first. |
| `downloadDir` | Folder downloads go into. | The system's Downloads folder. |
| `askDownloadDir` | `true` asks where to save each download. | Use `downloadDir`. |
| `copyOnSelect` | `true` copies the mouse selection to the clipboard. | Off. |
| `rightClick` | `paste`, `copy`, `copy-or-paste` or `ignore`. | The context menu (also for unknown values). |
| `noLinkClick` | `true` keeps Ctrl+click (Cmd+click) from opening links. | Links open. |
| `wheelLines` | Lines per mouse wheel notch, 1 to 20 (higher reads as 20). | 3 |
| `shortcuts` | Changed shortcuts only, such as `{"find": "Ctrl+Alt+F", "closeTab": "none"}`. | Platform defaults. |
| `termEncoding` | The server's character encoding for hosts set to "Default": `gbk`, `gb18030`, `big5`, `shift_jis`, `euc-jp`, `euc-kr`, `iso-8859-1` or `windows-1252`. | `utf-8`, no conversion (also for unknown values). |
| `termBackspace` | `ctrl-h`: Backspace sends `^H`, for hosts set to "Default". | `^?` |
| `termDelete` | `del` or `ctrl-h`: what Delete sends, for hosts set to "Default". | `ESC [3~` |
| `optionAsAlt` | `true` makes Option the Alt (Meta) key on macOS. | Off. |
| `termFontFamily` | Terminal font, a comma-separated list. | JetBrains Mono, Cascadia Mono, Consolas, Menlo, DejaVu Sans Mono, monospace |
| `termFontSize` | Font size, 6 to 48. | 13.5 |
| `termLineHeight` | Line height as a multiple of the font's, 0.8 to 2. | 1 |
| `termFontWeight` | Font weight: 300, 400, 500, 600 or 700. | 400 |
| `termNoLigatures` | `true` turns ligatures off. | Ligatures on. |
| `termTheme` | Ghostty color scheme name. | Tomorrow Night |
| `termBackground` | File name of the background image copy. | No image. |
| `termBackgroundFit` | `contain` or `tile`. | Cover. |
| `termBackgroundOpacity` | Image opacity in percent, 5 to 100. | 30 |
| `keepManagerOpen` | `true` keeps the connection manager open after connecting. | It closes. |
| `fileIconTheme` | `minimal`, `bundled:seti`, or `<publisher>.<name>/<theme ID>`. | Material Icon Theme |
| `sidebarWidth`, `treeWidth` | Widths of the sidebar and the file panel's folder tree, as last dragged. | The app's defaults. |
| `monitorPercent`, `terminalPercent` | The server monitor's share of the sidebar and the terminal's share of the main area, in percent. | The app's defaults. |
| `sync` | The Sync page's settings (see [Sync](#sync)). | Sync is off. |

Details of each option are in [features.md](features.md): see
[General](features.md#general), [Transfers](features.md#transfers),
[Terminal](features.md#terminal-defaults),
[Terminal › Mouse](features.md#terminal--mouse),
[Terminal › Shortcuts](features.md#terminal--shortcuts),
[Appearance](features.md#appearance-font-color-scheme-background-image),
[File icon themes](features.md#file-icon-themes),
[Connection manager](features.md#connection-manager) and
[Layout and split dividers](features.md#layout-and-split-dividers).

## Sync

The Sync page of the options (see [Sync](features.md#sync)) is the `sync`
object of `settings.json`, missing while sync has never been set up:

| Key | Meaning | Default (when missing) |
|---|---|---|
| `provider` | `gist`, `webdav` or `s3`. | Sync is off. |
| `gistId` | The gist holding the file, found or created by the first sync. | Find or create one. |
| `webdavUrl`, `webdavUser` | The WebDAV folder and user. | None. |
| `s3Endpoint`, `s3Region`, `s3Bucket`, `s3Key`, `s3AccessKeyId` | The S3-compatible bucket, the file's key in it and the access key's ID. | Region `us-east-1`, key `finallyshell-sync.json`. |
| `passwords` | `true` includes saved passwords. | Not included. |
| `auto` | `true` syncs automatically. | Off. |
| `interval` | Minutes between automatic syncs: 5, 15, 30 or 60. | 15 |

Each settings key is either synced or kept on each computer; the lists are
`syncedSettings` and `localSettings` in `internal/config/sync.go`. Kept
local: `sidebarWidth`, `monitorPercent`, `terminalPercent`, `treeWidth`,
`termBackground`, `downloadDir`, `askDownloadDir` and `sync`, and a
`fileIconTheme` installed from a VSIX or found in an editor's extensions.

Two more files belong to sync, next to `hosts.json`:

- `sync-state.json`: where the last sync synced (`remote`), when (`at`),
  the computer that wrote the file it read (`device`), and the file as it
  was after that sync (`base`), still encrypted with the sync password: the
  next sync compares both sides with it; and whether that sync included
  this computer's saved passwords (`secrets`): when it did not, the next
  sync does not compare passwords with the base, so turning "Include
  saved passwords" on later receives the passwords rather than deleting
  them. Deleting the file makes the next sync keep what either side has,
  as a first sync does.
- `hosts.before-sync.json`: `hosts.json` as it was before the last sync
  that changed it.

The file on the service, `finallyshell-sync.json`, is JSON:
`{"app": "finallyshell", "format": 1, "kdf": "argon2id", "salt": ..., "t": 3, "m": 65536, "p": 4, "cipher": "aes-256-gcm", "nonce": ..., "data": ...}`,
the binary fields in base64. Decrypted, `data` is
`{"schema": 1, "device": ..., "updatedAt": ..., "folders": [...], "hosts": [...], "settings": {...}, "secrets": {...}}`,
where folders and hosts are those of `hosts.json`, settings the synced
keys of `settings.json`, and secrets the saved passwords by their key in
the credential store.

## updater.json

The choices of the update window (see [Updates](features.md#updates)) are
kept by MyGo's updater plugin in `updater.json`, in the app's user data
folder rather than the config directory above: `~/Library/Application
Support/Finallyshell` on macOS (on its default file system, which ignores
case, the same folder as the config directory), `%APPDATA%\Finallyshell` on Windows,
and `$XDG_CONFIG_HOME/Finallyshell` or `~/.config/Finallyshell` on Linux.
`FINALLYSHELL_DIR` does not move it.

| Key | Meaning | Default (when missing) |
|---|---|---|
| `automaticChecks` | `false` turns the checks in the background off ("Check for updates automatically"). | On. |
| `automaticDownloads` | `true` installs the updates the background checks find without asking; they run at the next launch. | Ask. |
| `skippedVersion` | The version the user skipped; the background checks do not offer it. | None. |
| `lastCheck` | When the last check succeeded. | Never. |

A damaged file is ignored, as if it were missing.

## Updates in mygo.json

`updates` in `mygo.json` is built into the app: where it reads updates and
the key they must be signed with.

```json
"updates": {
  "publicKey": "fwcqse2POLiSdWytl0BHeURrztxcDObwmos+4IlTqHQ=",
  "github": "TooonyChen/finallyshell"
}
```

- `publicKey` is the public half of the key that signs updates. Installed
  copies accept only updates signed with its secret half, which is the
  repository secret `MYGO_UPDATER_PRIVATE_KEY` and never in the
  repository (see [Signing updates](development.md#signing-updates)).
- `github` is the repository whose latest release the app reads the
  update manifests from.

## Where passwords are stored

Passwords and private key passphrases are stored with MyGo's
`mygo.Secrets` in the system's credential store, which the system encrypts
for the current user. The service name is `finallyshell`, not the app's
identifier, so that `mygo dev`, `go run` and the installed app share the
same entries, as they share `hosts.json`. The keys are:

- `host/<host ID>/password`: the password;
- `host/<host ID>/passphrase`: the private key passphrase;
- `host/<host ID>/proxy-password`: the password of this host's SOCKS5 or
  HTTP proxy;
- `sync/password`: the sync password (see [Sync](features.md#sync));
- `sync/<service>/credential`: the token, password or secret access key of
  the sync service (`gist`, `webdav` or `s3`).

"Remember password" is a per-host setting and covers all of these for the
host.

| System | Where | How to look at it |
|---|---|---|
| macOS | A generic password in the login keychain, with the name (service) `finallyshell` and the key as the account | Keychain Access |
| Windows | A generic credential in Credential Manager, with the target `finallyshell/<key>` | Control Panel → Credential Manager → Windows Credentials |
| Linux | Secret Service (GNOME Keyring, KWallet, KeePassXC and others, through libsecret), with the attribute `service` set to `finallyshell` and `account` set to the key | "Passwords and Keys" (Seahorse), or `secret-tool lookup service finallyshell account host/<host ID>/password` |

How passwords are used:

- When connecting, the app first uses a password already known in this run.
  If there is none, it reads the credential store once, and only if the
  store has none either does it show a password prompt. The same goes for
  every password along the way: the host's, its proxy's, each jump host's,
  and each jump host's proxy's. The password prompt also has "Remember
  password". The host's own private key passphrase is read when
  connecting; if it cannot be read, the key is used as if it had no
  passphrase.
- When the server rejects a saved password, the session shows
  "Authentication failed" and a password prompt opens. When a private key
  is missing its passphrase or the passphrase is wrong, a passphrase prompt
  opens the same way (with "Remember passphrase"). When a jump host rejects
  its password, or a proxy rejects authentication, that hop's password
  prompt opens. After you enter it, the same session reconnects.
- Clicking OK in the connection editor saves to the credential store at
  once, since the user explicitly saved. A password typed into a prompt is
  kept only in memory at first, and saved after a successful login,
  replacing the old one. A wrong password is never saved, and the
  previously saved one stays until one that logs in replaces it.
- Other errors from the credential store (for example the user denying
  access or cancelling an unlock) are treated as "nothing saved". Each
  distinct error is logged only once.
- Turning off "Remember password" deletes the password from the credential
  store and keeps it only in memory, so it must be entered again after the
  app restarts. The choice is saved per host (`noRememberPassword` in
  `hosts.json`), and later password prompts are unchecked by default too.
- When the system has no credential store (on Linux without libsecret,
  without a session D-Bus, or with no Secret Service on D-Bus, which is
  common on servers and minimal desktops), passwords are kept in memory
  only. "Remember password" in the editor and the prompts is disabled, with
  the reason shown below it.
- Reading or writing the credential store may make the system ask the user
  something: to unlock the keychain or keyring, or, on macOS, whether a
  rebuilt app may read an entry saved earlier, the first time it does. These
  calls can take a long time, so the app never makes them on the UI thread
  (see the comment at the top of `internal/app/secrets.go`). They run in
  order on a background goroutine, and the results go back to the UI thread
  through `App.post`.
