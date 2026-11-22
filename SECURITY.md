# Security policy

## Reporting a vulnerability

Open a [private security advisory](https://github.com/TooonyChen/finallyshell/security/advisories/new)
rather than a public issue. If you cannot use advisories, mail the
maintainer (the address is on the GitHub profile) with `SECURITY` in the
subject.

Please include: what you can do with the flaw, the version
(`Finallyshell › About`), your platform, and the steps to reproduce. A
proof of concept helps; a working exploit against someone else's server
does not.

We aim to answer within a week, and to ship a fix for anything that lets
someone read a host's password, a session's traffic, or the sync file.
There is no bug bounty.

## What this app keeps, and where

| What | Where |
| --- | --- |
| Hosts, folders, settings | JSON files in the user config folder |
| Passwords | the system keychain, service name `finallyshell` (or memory only, if the user picks that) |
| Sync file (if the user turns it on) | GitHub Gist, WebDAV, or an S3-compatible bucket, **encrypted on the device** |
| SSH host keys | the user's `known_hosts`-style file in the config folder |

The sync file is AES-256-GCM under a key Argon2id derives from the sync
password (t=3, 64 MiB, p=4); the KDF parameters travel with the file so
they can be raised later. See `internal/cloudsync/crypto.go`.

## What leaves the machine

Nothing, unless the user turns it on. The complete list of outbound
endpoints the code can reach:

| Endpoint | When | Why |
| --- | --- | --- |
| `github.com` (API, releases) | always, on a version check | updates; the update archives are signed with the key in `mygo.json` |
| the user's own SSH servers | always | that is the product |
| GitHub Gist / WebDAV / S3 URL | only if sync is configured | the encrypted sync file |
| `raw.githubusercontent.com`, `cdn.jsdelivr.net` | when the IP database is downloaded | ip2region data for offline IP locations |
| `api.ip.sb` (HTTPS) | only if online IP locations are turned on | city and country of an address |
| `ip-api.com` (**plaintext HTTP**) | only if online IP locations are turned on, and only when the HTTPS service is busy | the same, in batches |
| `nxtrace.org` | only when the user runs NextTrace | route tracing |

There is no telemetry, no crash reporting, no analytics, and no phone-home.

### Known trade-off: `ip-api.com` is HTTP

`ip-api.com`'s free service is HTTP only, so when online IP locations are
on, the addresses being looked up — your servers' — can be seen by anyone
on the path. The lookups are cached on disk (`config.GeoDir()/cache.json`)
so each address is asked once, and the feature is off by default. If that
is not acceptable, leave online locations off: the offline database gives
the same map pins without any request.

## Signed releases

- Update archives: signed with the key pinned in `mygo.json`
  (`updates.publicKey`), verified by MyGo's updater before it installs.
- Release files: `SHA256SUMS`, plus build provenance and an SBOM
  attestation that `gh attestation verify <file> -R TooonyChen/finallyshell`
  checks against GitHub's key.
- The installers themselves are **not** code-signed (no Apple Developer ID,
  no Authenticode), which is why macOS asks you to run `xattr` once. That
  is a distribution gap, not a signature one: the update channel is signed
  even though the first download is not.

## Scope

In scope: anything that breaks the guarantees above — a way to make the
app send data to an endpoint not in the list, to read another user's
keychain entry, to install an unsigned update, to decrypt a sync file
without its password, or to make a saved host connect somewhere other
than where it says.

Out of scope: flaws in the servers you connect to, the security of the
GitHub/WebDAV/S3 account you sync through, and anything that needs an
attacker who can already write to your config folder.
