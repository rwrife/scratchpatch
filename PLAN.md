# scratchpatch — PLAN.md

> `git stash` for the throwaway files you were never going to commit anyway.

## 1. Pitch

Developers constantly spawn junk: `test.py`, `scratch.md`, `foo.json`, `delete-me.sh`, `/tmp/asdf`. They scatter across the repo, pollute `git status`, get `.gitignore`'d in a panic, or sit in `/tmp` until a reboot eats them. **scratchpatch** is a tiny CLI that gives every throwaway a *home with an expiration date*: `sp new` drops you into a dated scratch file outside your repo, each scratch carries a TTL, and a reaper sweeps the expired ones into a recoverable "morgue" before deleting for good. It's the difference between "I'll clean that up later" (you won't) and a system that cleans up *for* you — gently, and never destructively.

## 2. Trend inspiration

- **Ask HN: "What developer tool do you wish existed in 2026?"** (news.ycombinator.com/item?id=46345827) — recurring threads about wanting *throwaway / tactile thinking space* ("I want to plan with my hands", "disposable shell containers", "temporary ssh containers that terminate on exit"). The desire for **ephemeral, low-ceremony scratch space** is everywhere; the tooling is all heavyweight (LXC, firecracker, k8s `kubectl run --rm`).
- **"Disposable / ephemeral environments" trend (2026)** — lots of buzz around throwaway *containers* and *sandboxes*, but nothing small for the 90% case: a throwaway *file*, not a throwaway *VM*.
- **".env / scratch files are a security & hygiene risk with AI coding agents"** (bastion.tech, doppler.com, securityboulevard.com — Oct 2025+) — AI agents now litter repos with temp files and half-baked secrets. Keeping scratch work *out of the repo tree entirely* is suddenly a real hygiene win, not just tidiness.
- **The "modern CLI renaissance"** (Terminal Trove, multiple 2026 "modern CLI tools" roundups) — fast, single-binary, opinionated little tools with personality are having a moment. A scratch manager fits that mold perfectly.

## 3. Why it's different

- **vs. `/tmp` + reboot:** `/tmp` is unindexed, untagged, and wiped unpredictably (or never, on long-lived servers). scratchpatch is *indexed, tagged, searchable, and reaped on a schedule you control* — with a recovery window.
- **vs. `git stash`:** stash is for *tracked* changes you intend to revisit. scratchpatch is for files that should *never touch git* in the first place. (Contrast with our own `stash-stash`, which is a concierge for the git stash graveyard — different graveyard entirely.)
- **vs. scratch buffers in your editor (Vim `:enew`, Emacs `*scratch*`, VS Code untitled tabs):** those die with the editor and live only in one tool. scratchpatch is editor-agnostic, persistent across sessions, indexed, and shared across every project.
- **vs. note apps (Obsidian, Notion, `nb`, `jrnl`):** those are for notes you *keep*. scratchpatch is explicitly for things you *won't* keep — TTL is a first-class feature, not an afterthought. It optimizes for *forgetting safely*.
- **vs. ephemeral-container tools (firecracker, devcontainers, `kubectl run --rm`):** those give you a throwaway *machine*. scratchpatch gives you a throwaway *file* in milliseconds, no daemon, no image pull.

As far as I know there's no single-binary CLI that treats "scratch file with an expiry and a recovery morgue" as the core primitive. Adjacent pieces exist; the combination + personality is the fresh angle.

## 4. MVP scope (v0.1)

The smallest useful thing:

- `sp new [name] [--ttl 7d] [--ext md] [--tag foo]` → creates a scratch file in the store (`~/.local/share/scratchpatch/scratches/`), opens it in `$EDITOR`, records metadata (id, created, ttl, tags, origin cwd).
- `sp ls` → table of live scratches: id, name, age, expires-in, tags, size. Color-coded by how close to expiry.
- `sp cat <id>` / `sp open <id>` → print or re-open a scratch.
- `sp rm <id>` → move a scratch to the morgue immediately (not hard-deleted).
- `sp reap` → move all expired scratches to the morgue; hard-delete morgue items older than the grace period (default 3d).
- `sp resurrect <id>` → pull a scratch back out of the morgue.
- A flat-file JSON index (`index.json`) — no DB in v0.1.
- Sensible defaults: default TTL 7d, default ext `md`, store under XDG dirs.

That's a complete, genuinely-useful loop on day one.

## 5. Tech stack

Boring, fast, single-binary:

- **Go** — single static binary, trivial install (`go install` / release artifact), great for a snappy CLI, zero runtime deps. Cross-compiles everywhere.
- **`cobra`** for command structure (battle-tested, what `gh`/`kubectl` use). Predictable subcommand UX.
- **`lipgloss`** (Charm) for the colorized `ls` table — lightweight, no full TUI needed in v0.1.
- **stdlib `encoding/json`** for the index — no DB until we actually need one.
- **XDG base dirs** (`os.UserConfigDir` / a tiny helper) for store location, overridable via `SCRATCHPATCH_HOME`.

Justification: Go gives us the "fast single-binary tool with personality" vibe the trend rewards, and keeps install friction near zero. JSON-on-disk is plenty for hundreds of scratches; we can swap in SQLite later behind the index interface if anyone ever has thousands.

## 6. Architecture

```
cmd/sp/main.go        # cobra root, version, wires subcommands
internal/store/       # the scratch store: paths, create, list, move-to-morgue, hard-delete
internal/index/       # JSON index read/write, metadata model (Scratch struct)
internal/ttl/         # human duration parsing ("7d","2h","30m"), expiry math
internal/render/      # lipgloss table + color-by-expiry, plain fallback when not a TTY
internal/config/      # XDG resolution, defaults, env overrides
```

Key modules:
- **store** owns the filesystem layout (`scratches/`, `morgue/`) and is the only thing that touches disk for content.
- **index** is the source of truth for metadata; store + index are kept consistent via a thin service layer in each command.
- **ttl** is pure functions (easy to unit-test): parse durations, compute `expiresAt`, classify a scratch as fresh / expiring-soon / expired.
- **render** is the only thing that knows about color; everything else returns plain data.

Design rule: **destructive actions are always two-step.** `rm`/`reap` move to the morgue; only `reap` hard-deletes, and only items already past the grace period. There is no command that nukes content in one move.

## 7. Milestones

1. **M1 — Scaffold + hello-world.** Go module, `cobra` root command, `sp version` prints a version + tagline. CI (GitHub Actions: `go build` + `go vet`). README quickstart. Nothing else.
2. **M2 — Store + index foundation.** XDG store layout, `Scratch` metadata model, JSON index read/write with file-locking-lite (atomic write via temp+rename). Unit tests for index round-trip.
3. **M3 — `sp new` + `sp ls`.** Create a scratch, open `$EDITOR`, persist metadata; list live scratches as a colorized table (with a plain non-TTY fallback). TTL + ext + tag flags.
4. **M4 — Lifecycle: `cat`/`open`/`rm`/`resurrect` + the morgue.** Soft-delete to morgue, re-open, restore. Morgue listing via `sp ls --morgue`.
5. **M5 — `sp reap` + TTL engine.** Human-duration parsing, expiry classification, reap expired → morgue, hard-delete past grace. A `--dry-run` that shows what *would* die. Tests for ttl math.
6. **M6 — Polish + personality + ship.** Tombstone-flavored messages, `sp doctor` (store health/orphan detection), shell-completion generation, `--json` output mode, GoReleaser config + first tagged release `v0.1.0`.

Each milestone is independently shippable.

## 8. Backlog / future features (v0.2+)

1. **`sp reap` as a cron/launchd snippet generator** (`sp reap --install-cron`) so cleanup is truly automatic.
2. **Per-project scoping** (`sp ls --here` shows only scratches whose origin cwd is under the current repo).
3. **`sp promote <id>`** — graduate a scratch into the current repo (move it in + open it), for when a throwaway turns out to matter.
4. **Secret tripwire** — warn (and refuse to `promote`) if a scratch looks like it contains an API key / `.env`-style secret. Ties into the AI-agent-hygiene trend.
5. **Templates** (`sp new --from sql` / `--from python`) with a starter snippet + shebang.
6. **Fuzzy picker** (`sp open` with no arg → `fzf`-style interactive selector).
7. **Encryption at rest** for scratches tagged `--secret`.
8. **Size/quota guardrails** — warn when the store exceeds N MB; `sp reap --by-size`.
9. **`sp pin <id>`** — exempt a scratch from reaping without giving it an absurd TTL.
10. **Git-ignore guard** — detect when a scratch was created *inside* a repo and offer to relocate it into the store.
11. **`sp stats`** — fun metrics: scratches created, bytes saved from rotting in `/tmp`, oldest survivor, mortality rate.
12. **Export/import** the store as a single tarball for moving between machines.

## 9. Out of scope (deliberately NOT building)

- A TUI/dashboard app (v0.1 is plain commands + a colorized table; no full-screen UI).
- A daemon or background service — reaping is on-demand or via the cron snippet, never a resident process.
- Cloud sync / accounts / a hosted backend. The store is local files, full stop.
- Throwaway *containers/VMs* — that's a different (heavier) problem; we manage throwaway *files*.
- A real database in v0.1 (JSON-on-disk is the contract; SQLite stays a future option behind the index interface).
- Editor plugins — scratchpatch is editor-agnostic and shells out to `$EDITOR`.
