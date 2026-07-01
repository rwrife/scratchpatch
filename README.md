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
sp doctor                            # check store health (orphans, missing files, size)
sp ls --json | jq '.[].id'           # machine-readable output for scripting
sp completion zsh > "${fpath[1]}/_sp" # tab-completion for your shell
sp resurrect <id>                    # changed your mind? pull it back
```

## What works today

`sp new` and `sp ls` are implemented (M3); the full lifecycle —
`sp cat`, `sp open`, `sp rm` (soft-delete), `sp resurrect`, and `sp ls --morgue`
(M4); **automatic reaping** — `sp reap`, with human-friendly TTLs and a
`--dry-run` preview (M5); and a read-only **`sp doctor`** store health check
plus scripting polish — **`sp ls --json`** and **`sp completion`** for
bash/zsh/fish (M6, in progress).

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
so you can see what's about to die before it does. Run `sp reap` from cron or a
launchd job to keep the store tidy automatically (a `--install-cron` helper is
on the backlog).

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
```

A clean store prints a one-line bill of health. When `doctor` finds something,
it lists each orphan and missing file and points you at the safe next steps
(`sp resurrect` what you want to keep, or remove stray files by hand) — it won't
tidy up on its own, in keeping with the never-destructive-by-surprise rule.

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

No tagged release yet (`v0.1.0` is the target). Build from source — it's a single
static binary with no runtime dependencies:

```bash
git clone https://github.com/rwrife/scratchpatch
cd scratchpatch
go build -o sp ./cmd/sp

./sp version      # scratchpatch <version> + tagline
./sp --help       # see available commands
```

Requires Go 1.22+. Drop the `sp` binary anywhere on your `PATH`.

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
