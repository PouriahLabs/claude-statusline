# Roadmap

Work that is planned but **not implemented**. Nothing here exists yet; the
README describes what the tool does today.

## Update discovery

Updates are manual: nothing checks for or announces a new release. The three
items below are the plan for fixing that.

The binding constraint on all three: **the render path must never touch the
network.** It runs on every repaint at ~11ms, and blocking on `api.github.com`
during an outage would look like this tool hanging. Only the local cache read
is allowed in that path.

### 1. `claude-statusline upgrade`

The mechanism. Mirrors what `install.sh` already does, in-process:

- Resolve the latest tag from the GitHub releases API.
- Download the asset for this platform, using the same name template as the
  install scripts (`claude-statusline_<version>_<os>_<arch>.tar.gz|zip`).
- Verify against the published `checksums.txt`. **Refuse to install on
  mismatch** — never fall back to unverified, which is the mistake the shell
  installer originally made on macOS.
- Swap the binary in place. On Windows a running `.exe` can't be overwritten,
  but it *can* be renamed: move the current one aside to `.old`, write the new
  one at the original path, delete the `.old` on next start.
- Stdlib only — `net/http`, `archive/zip` and `archive/tar` cover all of it, so
  this adds no dependency.
- If `version` is `dev` (a source build, not a release), refuse and point at
  `go install` instead of clobbering someone's local build.

### 2. `doctor` reports the available version

One explicit, user-initiated network call — no surprise, and it fits what
`doctor` already is. Prints installed vs latest. A network failure is a note,
not a non-zero exit: being offline is not a misconfiguration.

### 3. Passive notice in the bar

The part that actually reaches people who never run `doctor`.

- A daily check, cached to a file exactly like the git cache.
- The fetch runs in a **detached child process** that writes the cache and
  exits. The render only ever reads that file, so nothing blocks.
- When an update is known, show a dim `·v0.3.0` on the model pill — not a new
  pill, and not a colour that competes with the two meters.
- Config key `update_check`.

**Open decision: opt-in or opt-out.** A status line that quietly contacts
GitHub costs trust when someone notices it, even though it's an unauthenticated
release check. Opt-out reaches most users and must be stated plainly in the
README rather than buried; opt-in surprises nobody but reaches almost nobody.
Not worth building until 1 and 2 exist and someone asks for it.

## Codex CLI support

Blocked upstream, not by effort. Codex CLI's status line is built in and
closed: `tui.status_line` takes an ordered list of predefined item ids, with no
external-command contract, so there is nothing to point this binary at. The
request for one is
[openai/codex#17827](https://github.com/openai/codex/issues/17827), open and
unanswered.

If it lands, the port is small. `internal/input` is the only
Claude-Code-specific package; `segments` and `render` already work off a
payload struct rather than the wire format.
