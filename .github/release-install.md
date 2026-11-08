## Install

| System | File |
|---|---|
| macOS (Apple silicon) | `finallyshell-<version>-arm64.dmg` |
| Windows (x64) | `finallyshell-setup-<version>-amd64.exe`, or the portable `finallyshell-<version>-windows-amd64.zip` |
| Debian, Ubuntu (x64) | `finallyshell_<version>_amd64.deb` |
| Other Linux (x64) | `finallyshell-<version>-linux-amd64.tar.gz` with `install.sh`: `sh install.sh` installs it in `~/.local` |

The macOS app is not notarized by Apple yet, so macOS blocks it the first time. After dragging it to Applications, run:

```sh
xattr -dr com.apple.quarantine /Applications/Finallyshell.app
```

or open System Settings, Privacy & Security, and click "Open Anyway". Windows SmartScreen may also warn about the unsigned installer: click "More info", then "Run anyway".

Check a download with `sha256sum -c --ignore-missing SHA256SUMS`.

Finallyshell is offered under the PolyForm Noncommercial License 1.0.0 or the PolyForm Internal Use License 1.0.0 (`LICENSE.txt`); `THIRD_PARTY_LICENSES.txt` lists the software it includes.
