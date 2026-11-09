## Install

| System | File |
|---|---|
| macOS (Apple silicon) | `finallyshell-<version>-arm64.dmg` |
| macOS (Intel) | `finallyshell-<version>-amd64.dmg` |
| Windows (x64) | `finallyshell-setup-<version>-amd64.exe`, or the portable `finallyshell-<version>-windows-amd64.zip` |
| Debian, Ubuntu (x64) | `finallyshell_<version>_amd64.deb` |
| Other Linux (x64) | `finallyshell-<version>-linux-amd64.tar.gz` with `install.sh`: `sh install.sh` installs it in `~/.local` |
| Debian, Ubuntu (ARM64) | `finallyshell_<version>_arm64.deb` |
| Other Linux (ARM64) | `finallyshell-<version>-linux-arm64.tar.gz` with `install.sh`: `sh install.sh` installs it in `~/.local` |

The macOS app is not notarized by Apple yet, so macOS blocks it the first time. After dragging it to Applications, run:

```sh
xattr -dr com.apple.quarantine /Applications/Finallyshell.app
```

or open System Settings, Privacy & Security, and click "Open Anyway". Windows SmartScreen may also warn about the unsigned installer: click "More info", then "Run anyway".

On Intel Macs the app draws its frames in memory rather than with the GPU, where some drivers leave the window blank and a frame that never finishes takes the whole session with it. If your Mac's GPU is fine with Metal, `MYGO_GPU=1` uses it.

Check a download with `sha256sum -c --ignore-missing SHA256SUMS`.

Finallyshell is offered under the PolyForm Noncommercial License 1.0.0 or the PolyForm Internal Use License 1.0.0 (`LICENSE.txt`); `THIRD_PARTY_LICENSES.txt` lists the software it includes.

## Updates

Finallyshell checks for new versions and updates itself (Options › General › Updates), except the `.deb` package, which you update by installing the new `.deb`. Versions up to 0.2.0 cannot update themselves: install this release, or a later one, by hand once. The `update-*.json` and `.delta` files, and the `.tar.gz` of macOS and Windows, are the app's updates; you don't need to download them.

## 安装

| 系统 | 文件 |
|---|---|
| macOS（Apple 芯片） | `finallyshell-<版本>-arm64.dmg` |
| macOS（Intel 芯片） | `finallyshell-<版本>-amd64.dmg` |
| Windows（x64） | 安装程序 `finallyshell-setup-<版本>-amd64.exe`，或便携版 `finallyshell-<版本>-windows-amd64.zip` |
| Debian、Ubuntu（x64） | `finallyshell_<版本>_amd64.deb` |
| 其他 Linux（x64） | `finallyshell-<版本>-linux-amd64.tar.gz` 和 `install.sh`：运行 `sh install.sh` 装到 `~/.local` |
| Debian、Ubuntu（ARM64） | `finallyshell_<版本>_arm64.deb` |
| 其他 Linux（ARM64） | `finallyshell-<版本>-linux-arm64.tar.gz` 和 `install.sh`：运行 `sh install.sh` 装到 `~/.local` |

macOS 版还没有经过 Apple 公证，第一次打开会被 macOS 拦下。把它拖进“应用程序”文件夹后，运行：

```sh
xattr -dr com.apple.quarantine /Applications/Finallyshell.app
```

或者在“系统设置 › 隐私与安全性”里点“仍要打开”。Windows 的 SmartScreen 可能会提示安装程序没有签名，点“更多信息”，再点“仍要运行”。

在 Intel Mac 上应用用内存绘制，不经过 GPU：有些驱动会让窗口一片空白，一帧画不完还会拖住整个应用。如果你的 Mac 的 GPU 用 Metal 正常，`MYGO_GPU=1` 可以用回它。

用 `sha256sum -c --ignore-missing SHA256SUMS` 校验下载的文件。

Finallyshell 以 PolyForm Noncommercial License 1.0.0 或 PolyForm Internal Use License 1.0.0 授权（`LICENSE.txt`）；`THIRD_PARTY_LICENSES.txt` 列出了它包含的软件。

## 更新

Finallyshell 会检查新版本并自行更新（选项 › 常规 › 更新），`.deb` 安装包除外，它要通过安装新的 `.deb` 来更新。0.2.0 及更早的版本不能自行更新：请手动安装一次这个版本或之后的版本。`update-*.json`、`.delta` 文件，以及 macOS 和 Windows 的 `.tar.gz`，是给应用自动更新用的，不需要下载。
