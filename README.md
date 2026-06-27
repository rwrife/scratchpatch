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
sp resurrect <id>                    # changed your mind? pull it back
```

## What works today

`sp new` and `sp ls` are implemented (M3); lifecycle and reaping commands are
landing next.

### `sp new [name]`

Creates a scratch in the store, records its metadata, and opens it in
`$EDITOR`.

```bash
sp new                       # auto-named dated slug, e.g. scratch-2026-06-26-2041
sp new bug-repro             # named scratch
sp new api --ext json        # pick the extension (no leading dot)
sp new todo --tag work --tag urgent   # --tag may be repeated
sp new note --ttl 168h       # custom lifespan (Go duration; "7d"-style parsing arrives in M5)
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
```

On a TTY, rows are color-coded by proximity to expiry — **green** = fresh,
**amber** = expiring within 24h, **red** = expired. When stdout isn't a
terminal, output is plain tab-separated text with no color codes.

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

- **Never destructive in one move.** `rm`/`reap` move to the morgue; only items past the grace period are ever hard-deleted.
- **No daemon.** Reaping is on-demand (or a cron snippet you opt into).
- **Local only.** The store is plain files under your XDG dirs (`SCRATCHPATCH_HOME` to override).

## License

MIT
