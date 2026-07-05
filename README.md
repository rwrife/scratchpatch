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
sp ls --json | jq '.[].id'           # machine-readable output for scripting
sp completion zsh > "${fpath[1]}/_sp" # tab-completion for your shell
sp scan <id>                         # tripwire: does this scratch hold a secret?
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
refuses them unless you pass `--allow-secrets` (M6, in progress).

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

### `sp cat <id>` / `sp open <id>`

Read or re-open a scratch. The `<id>` may be an **unambiguous prefix** — you
rarely need to type the full 8-char id.

```bash
sp cat 1a2b           # print a scratch's contents to stdout
sp open 1a2b          # re-open it in $EDITOR
```

- Both work on live scratches **and** ones sitting in the morgue.
- If a prefix matches more than one scratch, you'll be told to add more
  characters; if it matches none, you get a clear "no scratch matches" error.
- As with `sp new`, `sp open` falls back to printing the path when `$EDITOR`
  is unset — the scratch is never inaccessible.

### `sp rm <id>` — soft-delete to the morgue

Moves a scratch into the morgue. **This never destroys content** — it just
relocates the file and stamps a deletion time. Restore it any time with
`sp resurrect`.

```bash
sp rm 1a2b            # → moved to the morgue; printed restore hint
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

## Install

### Prebuilt binaries (once `v0.1.0` ships)

scratchpatch is a single static binary with no runtime dependencies. When a
release is tagged, cross-platform binaries (Linux, macOS, Windows × x86_64 /
arm64) are published to [**GitHub Releases**](https://github.com/rwrife/scratchpatch/releases)
by [GoReleaser](https://goreleaser.com). Grab the archive for your platform,
extract it, and drop `sp` anywhere on your `PATH`:

```bash
# example once artifacts exist — replace with the real version/URL
tar -xzf scratchpatch_0.1.0_macos_arm64.tar.gz
sudo mv sp /usr/local/bin/
sp version      # scratchpatch v0.1.0 + tagline
```

Each release also ships a `checksums.txt` so you can verify what you downloaded.

> No tagged release exists yet — `v0.1.0` is the target. Until then, build from
> source.

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
