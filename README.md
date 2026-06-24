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

Coming with the first tagged release (`v0.1.0`). Until then:

```bash
git clone https://github.com/rwrife/scratchpatch
cd scratchpatch && go build -o sp ./cmd/sp
```

## Design rules

- **Never destructive in one move.** `rm`/`reap` move to the morgue; only items past the grace period are ever hard-deleted.
- **No daemon.** Reaping is on-demand (or a cron snippet you opt into).
- **Local only.** The store is plain files under your XDG dirs (`SCRATCHPATCH_HOME` to override).

## License

MIT
