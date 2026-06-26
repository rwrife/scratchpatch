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
