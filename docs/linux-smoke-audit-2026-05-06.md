# Linux Smoke Audit — 2026-05-06

## Host

- **Distro / version:** Ubuntu 24.04 LTS (Noble Numbat), running as Docker container
  (`docker run --rm ubuntu:24.04`) on macOS (Darwin 25.3.0, Apple Silicon M1)
- **Architecture:** arm64 (aarch64) — Docker runs natively on M1
- **Tool versions installed during audit:**
  - tmux 3.4 (`apt install tmux`)
  - git 2.43.0 (`apt install git`)
  - gcc 13.3.0 (`apt install gcc`)
  - Go 1.26.3 (manual download from go.dev; see below)
  - `gh`, `glab`, `claude`, Dolt: not installed (see What broke or surprised)
- **Codebase commit:** `f40ab7d feat(ticket): add --depends-on flag to gt ticket create (nc-291) (#339)`
  (HEAD of main at time of audit; nc-292 was in-flight and not merged)

---

## Audit note: full lifecycle not reached

Steps 1–2 (install, build) and Step 3 (ct init up to Dolt) were completed in the Docker
container. Steps 4–6 (full ticket lifecycle, dashboard render, ct stop/ct nuke) require
an interactive environment with tmux, Claude API credentials, GitHub auth, and a running
Dolt server. These steps cannot be run headlessly in Docker and are documented under
"What broke or surprised" as environment limitations, not software failures.

A full lifecycle smoke on a real Ubuntu 24.04 VM (Multipass or similar) is the follow-up
for nc-298 (install docs rewrite).

---

## What worked

1. **apt dependency install** — `tmux`, `git`, `gcc`, `curl`, `make` all install cleanly
   via `apt-get install` on Ubuntu 24.04. No conflicts, no missing packages.
   Exact command: `sudo apt-get install -y tmux git gcc curl make`

2. **Go 1.26.x download from go.dev** — Once the correct URL format was identified
   (`go1.26.x.linux-arm64.tar.gz` — note the dash between `linux` and `arm64`, not a dot),
   Go 1.26.3 downloaded and extracted successfully:
   ```bash
   curl -fsSL https://go.dev/dl/go1.26.3.linux-arm64.tar.gz | tar -xz -C /usr/local
   export PATH=/usr/local/go/bin:$PATH
   go version  # → go version go1.26.3 linux/arm64
   ```

3. **`git clone`** — `git clone https://github.com/katerina7479/company_town.git`
   completed without errors.

4. **`make build`** — Both binaries compiled cleanly on Linux arm64 with `CGO_ENABLED=1`
   (required for go-sqlite3). Output:
   ```
   ct binary: -rwxr-xr-x 1 root root 15072176 ... bin/ct
   gt binary: -rwxr-xr-x 1 root root 13959944 ... bin/gt
   ```
   No build errors, no missing CGO dependencies.

5. **`make install`** — `go install ./cmd/ct` and `go install ./cmd/gt` both succeeded.
   Binaries installed to `$GOPATH/bin` (`/root/go/bin` in the container).

6. **`ct --version` and `gt --version`** — Both printed `ct version dev` and
   `gt version dev` respectively. PATH was set correctly (`$GOPATH/bin` on PATH).

7. **`ct init` config phase** — With piped input (`printf 'nc\ngithub\n...'`), all six
   prompts accepted values and `config.json` was created:
   ```
   created: config.json (platform=github, ticket_prefix="nc", session_prefix="ct-",
     dolt port=3306, database="company_town", preset=go)
   ```
   All eight agent CLAUDE.md templates were deployed to the init directory.
   Git remote was added correctly from the repo value.

---

## What broke or surprised

### 1. Go 1.26.x not available via apt on Ubuntu 24.04 (README gap)

`apt-get install golang-go` on Ubuntu 24.04 installs **Go 1.22.2**, not 1.26.x. The
project's `go.mod` requires `go 1.26.3`, so the apt-installed Go is insufficient. Build
fails with a toolchain mismatch error.

**Expected:** README says "Go 1.22+"; apt version should work.
**Actual:** Project requires 1.26.3; apt provides 1.22.2; build fails.
**Workaround:** Download directly from go.dev:
```bash
curl -fsSL https://go.dev/dl/go1.26.3.linux-arm64.tar.gz | tar -xz -C /usr/local
export PATH=/usr/local/go/bin:$PATH
```
**Quick fix:** README updated in this PR — "Go 1.22+" → "Go 1.26+", with a note that
Ubuntu apt lags behind and a direct-download command for Linux.

### 2. Correct Go download URL uses a dash, not dots (documentation gap)

The filename format for Go Linux tarballs uses a dash between OS and arch:
`go1.26.3.linux-arm64.tar.gz`. Using dots (`go1.26.3.linux.arm64.tar.gz`) returns 404.
**Quick fix:** Added the exact URL pattern to the README Linux build note.

### 3. `$GOPATH/bin` not on PATH by default on Ubuntu

After `make install`, binaries land in `~/go/bin` (the default GOPATH when none is set).
Ubuntu's default shell environment does not add `~/go/bin` to PATH. `ct` and `gt` would
report "command not found" without the explicit export.
**Quick fix:** README updated in this PR — added a PATH note to the "Install from source"
section.

### 4. `ct init` fails at Dolt: not in apt, not in README troubleshooting

After accepting all config prompts, `ct init` tried to start Dolt:
```
Connecting to Dolt server...
  Dolt server not responding, starting it...
error: dolt init: exec: "dolt": executable file not found in $PATH
```
Dolt is already listed in the README Requirements section with a link, but there is no
install command shown for Linux (the troubleshooting section only covers tmux, gh, and
claude). The Dolt installer script works on Linux:
```bash
curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash
```
(Not tested in this Docker run — could not authenticate GitHub for a real `ct init` run.)
**Recommended fix:** Add the Linux Dolt install command to the README Install from source
section or troubleshooting. Filed as a note; the full install-docs rewrite belongs in nc-298.

### 5. `ct init` platform prompt has no default for repos with no remote

When running `ct init` in a `git init` directory with no remote configured, the VCS
platform prompt shows no default:
```
  VCS platform (github, gitlab):
```
Pressing Enter with no input returns an error:
```
error: collecting init params: platform: invalid value for VCS platform (github, gitlab)
```
The init aborts rather than re-prompting. When input is provided (or piped), it works fine.
**Severity:** Low — users just need to type "github" or "gitlab". The error message is
clear enough. A default of "github" was considered but would bias the VCS-agnostic design.
**Filed:** nc-302 (P5) — ct init should re-prompt on invalid platform, not abort.

### 6. Report path conflict: `.company_town/docs/` is gitignored

The nc-295 spec places the report at `.company_town/docs/linux-smoke-audit-<date>.md`.
However, `.company_town/` is in the repo's `.gitignore` (it holds runtime state that
doesn't belong in version control). A file there cannot be committed or reviewed via PR.
This report is placed in `docs/` at the repo root instead.
**Decision (nc-303):** Use `docs/` at the repo root for all committed reports and audits.
`.company_town/` remains fully gitignored as runtime-only state.

### 7. Steps 4–6 not testable in Docker

Full lifecycle (ticket creation → architect → prole → PR → CI → review → merge) requires:
- An interactive tmux session (Docker has no TTY in `docker run --rm`)
- Claude API credentials (`claude` CLI)
- GitHub credentials (`gh auth login`)
- Running Dolt server (requires network and Dolt binary)
- Real CI run on GitHub (~5 min wait)

Dashboard rendering (step 5) requires a real terminal emulator with a TTY; ct nuke/stop
(step 6) require a running daemon.

None of these are testable headlessly. A full lifecycle smoke requires a real VM or cloud
instance with all dependencies authenticated. Recommended: Multipass Ubuntu 24.04 VM on
macOS or a GitHub Codespaces environment.

### 8. linux_arm64 release binary not provided

The README lists darwin_arm64, darwin_amd64, linux_amd64 as release targets. Linux arm64
(Raspberry Pi, cloud instances with ARM, M-series Mac Docker) requires building from
source. Building from source works correctly (see Step 2), but pre-built binaries would
improve the getting-started experience. This is nc-297's scope.

---

## Quick fixes landed in this PR

- **README: "Go 1.22+" → "Go 1.26+"** (two occurrences: Requirements section and
  "Install from source" section). The project's `go.mod` requires `go 1.26.1`; the old
  value was stale from before the toolchain upgrade.
- **README: Linux Go install note** — Added direct-download instructions with the correct
  URL format (`linux-arm64`, dash not dots) so Linux users building from source don't
  have to puzzle out the apt vs. go.dev gap.
- **README: GOPATH/bin PATH note** — Added `export PATH=$HOME/go/bin:$PATH` to the
  "Install from source" section so Linux users know where `make install` puts the binaries.

---

## New children filed under nc-294

- **nc-302** (P5) — `ct init` aborts on invalid platform input instead of re-prompting —
  low-severity UX issue; raised during audit. (Parent set to nc-295.)
- **nc-303** (P4) — Clarify report path convention: `.company_town/docs/` vs. `docs/`
  at repo root — gitignore conflict means `.company_town/docs/` cannot hold committed
  reports; need a decision on convention before nc-298 (install docs) lands. (Parent set to nc-294.)

---

## Open questions for CEO

1. **Full lifecycle smoke:** Should a follow-up audit run on a real Multipass VM be
   tracked as a sub-task of nc-295 or folded into nc-298's install-docs work?
2. ~~**Report path convention:** decided by nc-303 — `docs/` at repo root is the
   convention. `.company_town/` stays fully gitignored.~~
