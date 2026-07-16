# scratchpatch 🪦

> `git stash` for the throwaway files you were never going to commit anyway.

`test.py`. `scratch.md`. `foo.json`. `delete-me.sh`. They breed in your repo, clutter `git status`, and rot in `/tmp` until a reboot eats them. **scratchpatch** gives every throwaway a home with an expiration date — outside your repo, indexed, tagged, and auto-reaped before it goes stale. Nothing is ever hard-deleted in one step; expired scratches go to a recoverable **morgue** first.

> ⚠️ **Status:** pre-v0.1, under active construction. See [`PLAN.md`](./PLAN.md) for the roadmap and milestones.

## Why

- `/tmp` is unindexed and wiped unpredictably (or never).
- `git stash` is for changes you *mean* to keep; this is for files that should *never* touch git.
- Editor scratch buffers die with the editor and live in exactly one tool.
- Note apps are for things you keep. scratchpatch optimizes for **forgetting safely**.

## Quickstart (target UX)

```bash
sp new bug-repro --ttl 3d --ext py   # make a throwaway, open it in $EDITOR
sp ls                                # see what's alive and when it expires
sp reap --dry-run                    # preview what's about to die
sp reap                              # sweep expired → morgue, purge old morgue items
sp reap --install-cron               # print a daily-reap crontab line (no daemon)
sp doctor                            # check store health (orphans, missing files, size)
sp doctor --json | jq -e '.healthy'  # gate a script on store health
sp stats                             # fun store metrics: footprint, oldest survivor, tags
sp stats --json | jq '.totalBytes'   # bytes kept out of /tmp, for scripting
sp export --out snap.tar.gz           # snapshot the whole store to a tarball
sp import snap.tar.gz                 # restore it on another machine (merge)
sp ls --json | jq '.[].id'           # machine-readable output for scripting
sp completion zsh > "${fpath[1]}/_sp" # tab-completion for your shell
sp scan <id>                         # tripwire: does this scratch hold a secret?
sp dedup                             # report byte-identical scratches (read-only)
sp dedup --collapse                  # send redundant copies to the morgue (morgue-first)
sp tui                               # browse and manage scratches full-screen
sp resurrect <id>                    # changed your mind? pull it back
sp promote <id>                      # the good ones: graduate a scratch into your repo
```

## What works today

`sp new` and `sp ls` are implemented (M3); the full lifecycle —
`sp cat`, `sp open`, `sp rm` (soft-delete), `sp resurrect`, and `sp ls --morgue`
(M4); **automatic reaping** — `sp reap`, with human-friendly TTLs and a
`--dry-run` preview (M5); a read-only **`sp doctor`** store health check;
scripting polish — **`sp ls --json`** / **`sp doctor --json`** and
**`sp completion`** for bash/zsh/fish; **`sp promote`** to graduate a scratch
into your repo; and a **secret tripwire** — **`sp scan`** flags scratches that
look like they hold credentials, `sp ls` marks them with a 🔑, and `sp promote`
refuses them unless you pass `--allow-secrets`; and **content dedup** —
**`sp dedup`** reports byte-identical scratches and `--collapse` sweeps the
redundant copies to the morgue (M6, in progress); and an optional full-screen
**`sp tui`** browser over the same store (v0.2).

### `sp new [name]`

Creates a scratch in the store, records its metadata, and opens it in
`$EDITOR`.

```bash
sp new                       # auto-named dated slug, e.g. scratch-2026-06-26-2041
sp new bug-repro             # named scratch
sp new api --ext json        # pick the extension (no leading dot)
sp new todo --tag work --tag urgent   # --tag may be repeated
sp new note --ttl 7d         # human durations: s, m, h, d, w (e.g. 30m, 12h, 7d, 2w, 1w2d12h)
sp new draft --no-edit       # create it but don't open an editor
```

- With no name, a dated slug is generated for you.
- If `$EDITOR` is unset, the scratch is still created and its path is printed —
  nothing is lost.
- Defaults: extension **md**, TTL **7d**.

#### Headless capture (pipes, agents, generated output)

The interactive editor is great for humans and useless in a pipeline. Seed a
scratch's content directly and skip `$EDITOR` entirely — perfect for parking
throwaway logs, API responses, or AI-agent temp output in the reaped store
instead of littering your repo:

```bash
pytest -q 2>&1 | sp new failing-tests --stdin --tag ci   # capture from a pipe
sp new note --stdin <<'EOF'                              # capture from a heredoc
remember to revoke that token
EOF
curl -s https://api.example.com/thing | sp new resp --ext json --stdin
sp new todo --content "ship the thing"                   # one-liner, no pipe
sp new seed --from-file ./scratch.txt                    # seed from a file
```

- `--stdin`, `--content`, and `--from-file` each suppress `$EDITOR`. Pick
  exactly one per invocation (combining them is an error).
- All the usual flags apply: `--ttl`, `--ext`, `--tag`.
- Output still leads with the stable `created scratch <id>` anchor, so scripts
  and tests keep working.
- Captured content is run through the **secret tripwire** just like
  editor-created scratches, so `sp ls` shows the 🔑 marker and `sp promote`
  guards a piped-in credential.
- `--content ""` deliberately creates an empty scratch. `--stdin` on a bare TTY
  with nothing piped in refuses rather than hanging.

### `sp ls`

Lists live scratches: id, name, age, time-to-expiry, tags, and size.

```bash
sp ls            # colorized table on a terminal
sp ls --no-color # force plain output
sp ls | cat      # piped/redirected output is plain, tab-separated (script-friendly)
sp ls --json     # stable JSON array for scripting (no color, no flavor)
```

On a TTY, rows are color-coded by proximity to expiry — **green** = fresh,
**amber** = expiring within 24h, **red** = expired. When stdout isn't a
terminal, output is plain tab-separated text with no color codes.

For scripting, `--json` emits a stable array of records — ids, names, tags,
sizes, raw timestamps, plus pre-computed `ageHuman`, `expiresHuman`,
`expiresInSeconds`, and a `status` of `fresh`/`soon`/`expired` (the same buckets
the table tints). It works with `--morgue` too (`sp ls --morgue --json`), where
it reports purge timing instead. The JSON path is always color- and
personality-free, and an empty store emits `[]` rather than `null`, so
`sp ls --json | jq` stays predictable.

### `sp cat <id>` / `sp open [id]`

Read or re-open a scratch. The `<id>` may be an **unambiguous prefix** — you
rarely need to type the full 8-char id.

```bash
sp cat 1a2b           # print a scratch's contents to stdout
sp open 1a2b          # re-open it in $EDITOR
sp open               # no id → interactive picker over live scratches
```

- Both work on live scratches **and** ones sitting in the morgue.
- If a prefix matches more than one scratch, you'll be told to add more
  characters; if it matches none, you get a clear "no scratch matches" error.
- As with `sp new`, `sp open` falls back to printing the path when `$EDITOR`
  is unset — the scratch is never inaccessible.

**Interactive picker.** Run `sp open` with no id to choose from the live
scratches without typing one out. Each row shows id, name, age, time-to-expiry,
and tags; type to fuzzy-filter (subsequence match, so `tdo` finds `todo`), then
pick:

- If [`fzf`](https://github.com/junegunn/fzf) is on your `PATH`, it drives the
  picker. Pass `--no-fzf` to force the built-in one instead.
- Otherwise a built-in prompt lists the scratches: type to filter, enter a
  number to choose, a blank line to take the top match, or `q` to cancel.
- Piped / non-interactive input degrades to a one-shot numbered choice.
- **Esc / Ctrl-C / `q` cancels cleanly** — nothing is opened or changed.

### `sp rm <id>` — soft-delete to the morgue

Moves a scratch into the morgue. **This never destroys content** — it just
relocates the file and stamps a deletion time. Restore it any time with
`sp resurrect`.

```bash
sp rm 1a2b            # → buried in the morgue (not gone, just resting); printed restore hint
```

### `sp resurrect <id>` — bring it back

Pulls a soft-deleted scratch back out of the morgue and into the live set.

```bash
sp resurrect 1a2b     # (alias: sp restore)
```

### `sp promote <id> [dest]` — graduate a scratch into your repo

Sometimes a throwaway turns out to matter. `sp promote` is the escape hatch:
it moves the scratch's file out of the store and into your working tree, then
drops it from the index — once promoted it's the repo's to keep, and the reaper
can't touch it.

```bash
sp promote 1a2b                  # into the current dir, named from the scratch (e.g. bug-repro.py)
sp promote 1a2b ./notes          # into a directory: ./notes/<slug>.<ext>
sp promote 1a2b keep.md          # to an explicit path (renames on the way out)
sp promote 1a2b keep.md --force  # overwrite an existing destination
sp promote 1a2b --no-open        # don't open it in $EDITOR afterwards
sp promote 1a2b --allow-secrets  # promote even if the secret tripwire flags it
```

- With no `dest`, the file lands in the current directory under a slug of the
  scratch's name (its id when unnamed), keeping the original extension.
- An existing-directory `dest` drops the file inside it; any other `dest` is the
  full target path.
- Promoting **never overwrites** an existing file without `--force`, and a
  refused promote leaves the scratch untouched in the store.
- Promoting a scratch that trips the **secret tripwire** is refused unless you
  pass `--allow-secrets` — run `sp scan <id>` to see the masked findings first.
- After moving, the promoted file opens in `$EDITOR` (skip with `--no-open`);
  a missing `$EDITOR` is not fatal — the move already happened.

### `sp ls --morgue`

Lists the morgue: id, name, when each was deleted, **time until purge**, tags,
and size. Rows are tinted **amber** while there's grace left and **red** once
they're eligible for hard-deletion by `sp reap`.

```bash
sp ls --morgue            # what's in the morgue and how long it has left
sp ls --morgue --no-color # plain, script-friendly
```

### `sp reap` — sweep expired, purge past-grace

The reaper, and the one place scratchpatch ever destroys content. It runs two
stages, in order, and **never does both to the same scratch in one run**:

1. **Expired live scratches → morgue** (soft-delete). Their grace clock starts
   *now*, so a scratch that expires today is swept today but not purged today.
2. **Past-grace morgue items → gone** (hard-delete). Only morgue scratches that
   have aged beyond the grace window (default **3d**) are removed for good.

```bash
sp reap --dry-run    # preview exactly what would move and what would die
sp reap              # do it
sp reap --no-color   # plain output
```

`--dry-run` changes nothing on disk or in the index — it just prints the plan,
so you can see what's about to die before it does.

#### Automating it (no daemon, just a schedule)

To stop remembering to run it, `sp reap --install-cron` prints a ready-to-use
crontab line (a daily 03:00 sweep) plus an idempotent installer — re-running the
installer replaces the line instead of stacking duplicates. scratchpatch never
edits your crontab or runs a background process; it only prints what to run.

```bash
sp reap --install-cron              # print the crontab line + idempotent installer
sp reap --install-cron --launchd    # macOS: emit a launchd LaunchAgent instead
sp reap --uninstall-cron            # print how to remove the scheduled reap
```

Install it in one paste:

```bash
# adds exactly one scratchpatch line, even if run twice
eval "$(sp reap --install-cron | sed -n 's/^  //p' | grep crontab)"
```

Or copy the printed crontab line yourself. On macOS, `--launchd` prints a
`com.scratchpatch.reap.plist` to drop in `~/Library/LaunchAgents/` and the
`launchctl load` command to activate it. Either way it just schedules the
existing on-demand `sp reap` — consistent with scratchpatch's no-resident-daemon
rule.

Human durations everywhere a lifespan is accepted: `s`, `m`, `h`, `d`, `w`, and
composites like `1w2d12h`. `sp new --ttl 7d` and `sp new --ttl 30m` both work;
Go-style durations (`168h`, `1h30m`) are still accepted too.

### `sp doctor` — store health check

Gives the store a checkup by reconciling the index against what's actually on
disk. It's **read-only** — it diagnoses, but never moves, deletes, or rewrites
anything. It reports three things:

- **orphaned content** — files in `scratches/` or `morgue/` with no index entry
  (bytes the store forgot how to describe).
- **missing content** — index entries whose file is gone, so `sp cat`/`sp open`
  would fail on them.
- **footprint** — how many live/morgue scratches you have and how much disk the
  content occupies.

```bash
sp doctor            # colorized: green when healthy, amber/red when it finds drift
sp doctor --no-color # plain, script-friendly
sp doctor | cat      # piped output is plain text
sp doctor --json     # stable JSON object for scripting (no color, no flavor)
```

A clean store prints a one-line bill of health. When `doctor` finds something,
it lists each orphan and missing file and points you at the safe next steps
(`sp resurrect` what you want to keep, or remove stray files by hand) — it won't
tidy up on its own, in keeping with the never-destructive-by-surprise rule.

For scripting, `--json` emits a single stable object mirroring the report: a
top-level `healthy` flag, the live/morgue counts, tracked/orphan/total sizes
(raw bytes plus human strings), and `orphans`/`missing` arrays (always arrays,
never null). Gate a script on the store's health without parsing prose:
`sp doctor --json | jq -e '.healthy'`, or list drift with
`sp doctor --json | jq '.orphans[].path'`.

### `sp stats` — fun store metrics

Takes the store's pulse and prints the little numbers that make the whole scheme
feel worth it. Like `doctor`, it's **read-only** and derives everything from the
index — no new counters, nothing changed. It reports:

- **living** — how many scratches you're keeping and the bytes they hold.
- **oldest survivor** — the scratch that has dodged the reaper longest, and for
  how long.
- **morgue** — recoverable soft-deleted bytes, plus how many are already past
  the grace window (one `sp reap` from gone).
- **footprint** — total bytes that passed through the store instead of rotting
  loose in `/tmp` (as far as the index can account for — v0.1 keeps no all-time
  counters, so this is the current live + morgue footprint, honestly labeled).
- **top tags** — your most-used labels, ranked.

```bash
sp stats             # colorized report with a little tombstone flavor
sp stats --no-color  # plain, script-friendly
sp stats --json      # stable JSON object for scripting (no color, no flavor)
```

An empty store gets a friendly zero-state, not a wall of zeros. For scripting,
`--json` emits a single stable object: live/morgue counts, raw bytes plus human
strings for each size, a `graceSeconds` field, an `oldest` sub-object (`null`
when there are no live scratches), and a `tags` array (always an array, never
null). Pull the headline number with `sp stats --json | jq '.totalBytes'`.

### `sp export` / `sp import` — move the store between machines

The store is local files by design, but you probably have more than one machine.
`export` snapshots the whole store into a single dependency-free `.tar.gz`
(stdlib `archive/tar` + `compress/gzip` only); `import` restores it elsewhere.

```bash
sp export                              # scratchpatch-export-<timestamp>.tar.gz in cwd
sp export --out snap.tar.gz            # write to a specific file
sp export --include-morgue             # also archive soft-deleted scratches
sp export --out - | ssh box 'sp import -'   # pipe straight to another machine

sp import snap.tar.gz                   # merge (default): add, never clobber
sp import - < snap.tar.gz               # read the tarball from stdin
sp import snap.tar.gz --replace         # back up, then replace the whole store
```

By default only **live** scratches are exported; `--include-morgue` also bundles
morgued ones (they land back in the morgue on import). Import has two modes:

- **`--merge` (default)** — adds incoming scratches. On an id collision it keeps
  your existing scratch and reports the incoming one as *skipped*; it never
  overwrites content silently.
- **`--replace`** — destructive, so it must be explicit. It first writes a
  timestamped backup tarball next to the store root, *then* replaces the store
  with the archive's contents, keeping the operation recoverable.

Exports carry a self-describing manifest (not the raw index), so a round-trip —
export → fresh store → import — reproduces each scratch's content and metadata
(id, name, tags, timestamps, TTL) identically.

### `sp scan <id>` — the secret tripwire

AI coding agents and tired humans leak API keys and `.env` dumps into throwaway
files without thinking. `sp scan` runs a conservative heuristic detector over a
single scratch and reports anything that looks like a credential — **with the
values masked**. It never echoes a full secret back to your terminal.

It catches:

- **AWS access key ids** (`AKIA…`/`ASIA…` + the fixed-length tail),
- **PEM private-key headers** (`-----BEGIN … PRIVATE KEY-----`),
- **secret-looking assignments** — `API_KEY=`, `TOKEN=`, `SECRET=`,
  `PASSWORD=` and friends with a non-placeholder value,
- **long high-entropy tokens** that look generated (bearer tokens, opaque keys).

The heuristics are deliberately conservative to avoid alarm fatigue: template
values like `API_KEY=changeme`, `TOKEN=<your-token>`, and `${VAR}` references
stay quiet, as do ordinary prose, long numbers, and URLs.

```bash
sp scan 1a2b            # report masked findings by line number
sp scan 1a2b --no-color # plain, script-friendly
sp scan 1a2b --json     # stable JSON object (no color, no flavor)
sp scan 1a2b || echo blocked   # non-zero exit when secrets are found
```

A clean scratch prints a one-line bill of health and exits `0`. A tripped one
lists each finding as `L<line>  <rule>  <masked>` and exits **non-zero**, so
`sp scan` slots straight into pre-commit hooks and CI. The `--json` form carries
a top-level `tripped` flag and a `findings` array (always an array, never null;
each finding has `kind`, `line`, `rule`, and a `masked` preview — never the raw
value): `sp scan 1a2b --json | jq -e '.tripped | not'`.

The tripwire also shows up where it matters most:

- **`sp ls`** puts a 🔑 next to any scratch that trips, and `sp ls --json` sets
  `"secret": true` on it.
- **`sp promote`** refuses to graduate a tripped scratch into your repo unless
  you pass `--allow-secrets` — the last line of defense before a leaked key
  lands somewhere it might get committed.

### `sp dedup` — find (and collapse) byte-identical scratches

Throwaway files breed duplicates: the same log pasted three times, an agent
re-capturing identical output on every run, the same snippet `sp new`'d across
two projects. Each copy ages out separately and quietly wastes footprint.
`sp dedup` hashes every **live** scratch's content (SHA-256), groups the
byte-identical copies into clusters, and reports how much they cost — naming the
oldest copy in each cluster as the **canonical** one to keep.

This is a content-equality primitive, distinct from `sp doctor` (which
reconciles index-vs-disk drift, not equality) and `sp reap` (TTL-based). Two
scratches with different names, tags, or extensions but identical bytes are
still duplicates.

```bash
sp dedup             # read-only report of duplicate clusters (canonical + wasted bytes)
sp dedup --no-color  # plain, script-friendly
sp dedup --json      # stable JSON object (no color, no flavor)
sp dedup --collapse  # move the redundant copies to the morgue, keep the canonical
```

By default `sp dedup` is **strictly read-only** — it moves nothing. Pass
`--collapse` to send the redundant copies to the morgue, keeping each cluster's
canonical (oldest) member live. Like `sp rm`, collapse is **morgue-first and
never hard-deletes**: collapsed copies are recoverable with `sp resurrect` until
the reaper purges them past the grace window.

A unique store prints a clean, one-line bill of health. The `--json` form
carries a top-level `clean` flag plus `clusters` (always an array, never null;
each cluster has its full `digest`, `wastedBytes`, and `members` with a
`canonical` flag) and a `collapsed` object that is `null` until a `--collapse`
run moves something: `sp dedup --json | jq -e '.clean'`.

### `sp completion <bash|zsh|fish>`

Prints a shell completion script to stdout so `sp`'s commands and flags
tab-complete. The output is plain text (no color, no flavor), safe to redirect
straight into a file.

```bash
# bash
source <(sp completion bash)
sp completion bash > /etc/bash_completion.d/sp   # or persist it

# zsh (ensure `autoload -U compinit && compinit` runs in your .zshrc)
sp completion zsh > "${fpath[1]}/_sp"

# fish
sp completion fish > ~/.config/fish/completions/sp.fish
```

### `sp tui` — the full-screen browser

When you'd rather point-and-act than pipe, `sp tui` opens an interactive,
full-screen browser over the same store — no second source of truth, just a
keyboard-driven shell over `sp ls`/`rm`/`resurrect`/`open`. Live scratches on
the left (color-coded by expiry, 🔑 for anything the secret tripwire flagged),
the selected scratch's content previewed on the right. Secret scratches are
**never dumped** into the preview — you get a redaction notice pointing at
`sp scan` instead.

```bash
sp tui   # needs an interactive terminal; scripts should use `sp ls --json`
```

Keybindings:

- `↑`/`↓` (or `k`/`j`) — move the selection; `g`/`G` jump to top/bottom
- `tab` (or `m`) — flip between the **live** and **morgue** panes
- `/` — live filter by name, id, or tag; `esc` clears it, `enter` accepts
- `o` / `enter` — open the selection in `$EDITOR` (suspends and restores the TUI)
- `d` (or `x`) — soft-delete the selection to the morgue (live pane)
- `r` — resurrect the selection (morgue pane)
- `R` — reload both panes from disk
- `q` / `esc` — quit and restore the terminal

Every file-moving action (`d`, `r`) asks for a `y/N` confirmation, and — like
the rest of scratchpatch — the TUI **never hard-deletes**: `rm` here is still
just a move to the morgue. Not attached to a terminal? `sp tui` bows out with a
clear pointer to `sp ls`.

## Install

### Prebuilt binaries

scratchpatch is a single static binary with no runtime dependencies.
Cross-platform binaries (Linux, macOS, Windows × x86_64 / arm64) are published
to [**GitHub Releases**](https://github.com/rwrife/scratchpatch/releases) by
[GoReleaser](https://goreleaser.com). Grab the archive for your platform,
extract it, and drop `sp` anywhere on your `PATH`:

```bash
# macOS (Apple Silicon) — swap the platform/arch for yours; see the release page
VER=0.1.0
curl -sSL -O "https://github.com/rwrife/scratchpatch/releases/download/v${VER}/scratchpatch_${VER}_macos_arm64.tar.gz"
tar -xzf "scratchpatch_${VER}_macos_arm64.tar.gz"
sudo mv sp /usr/local/bin/
sp version      # scratchpatch v0.1.0 + tagline
```

Archives follow `scratchpatch_<version>_<os>_<arch>` (`os` is `linux`, `macos`,
or `windows`; `arch` is `x86_64` or `arm64`; Windows ships a `.zip`). Each
release also ships a `checksums.txt` so you can verify what you downloaded.

### From source

```bash
git clone https://github.com/rwrife/scratchpatch
cd scratchpatch
go build -o sp ./cmd/sp

./sp version      # scratchpatch <version> + tagline
./sp --help       # see available commands
```

Requires Go 1.22+. Drop the `sp` binary anywhere on your `PATH`.

### Maintainers: cutting a release

Releases are tag-driven. Push a `vX.Y.Z` tag and the
[`Release` workflow](./.github/workflows/release.yml) runs GoReleaser to build
the binaries and publish the GitHub Release:

```bash
goreleaser check                         # validate .goreleaser.yaml
goreleaser release --snapshot --clean    # dry-run into ./dist (no publish, no tag)
git tag v0.1.0 && git push origin v0.1.0 # the real thing
```

## Where things live

scratchpatch keeps everything in a local store. The layout:

```
$SCRATCHPATCH_HOME/            # default: ~/.local/share/scratchpatch
├── index.json                # metadata for every scratch (the source of truth)
├── scratches/                # live scratch files
└── morgue/                   # soft-deleted scratches, awaiting hard-delete
```

- Set `SCRATCHPATCH_HOME` to relocate the entire store (handy for testing or
  keeping scratches on a specific volume). Otherwise `XDG_DATA_HOME` is honored,
  falling back to `~/.local/share/scratchpatch`.
- `index.json` is written atomically (temp file + rename), so an interrupted
  command can never leave a half-written index.
- Defaults: TTL **7d**, extension **md**, morgue grace period **3d**.

## Design rules

- **Never destructive in one move.** `rm`/`reap` move to the morgue; only items past the grace period are ever hard-deleted, and only by `sp reap`.
- **No daemon.** Reaping is on-demand (or a cron snippet you opt into).
- **Local only.** The store is plain files under your XDG dirs (`SCRATCHPATCH_HOME` to override).

## License

MIT
