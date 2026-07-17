// json.go is render's machine-facing counterpart to the colorized tables.
//
// Where Table/MorgueTable draw glanceable, human-tinted output, the JSON
// emitters here produce stable, scriptable records for `--json` mode. Per the
// M6 contract, this path carries no color and no tombstone personality — it's
// pure data so `sp ls --json | jq` stays predictable.
//
// The JSON views deliberately denormalize a few computed fields the tables
// show (age, time-to-expiry/purge, lifecycle status) so a script gets the same
// information the table does without re-deriving it from timestamps. Raw
// timestamps are included too, so nothing is lost.
package render

import (
	"encoding/json"
	"io"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// ScratchJSON is the scriptable record for a live scratch under `sp ls --json`.
// It mirrors the live table's columns and adds machine-friendly raw values
// alongside the human strings, so both `jq '.[].id'` and a glance at the same
// JSON read naturally. Field order here is the emitted key order.
type ScratchJSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Tags      []string  `json:"tags"`
	Ext       string    `json:"ext"`
	Size      int64     `json:"size"`
	SizeHuman string    `json:"sizeHuman"`
	CreatedAt time.Time `json:"createdAt"`
	AgeHuman  string    `json:"ageHuman"`
	ExpiresAt time.Time `json:"expiresAt"`
	// ExpiresInSeconds is time-to-expiry in whole seconds; negative once the
	// scratch is past its deadline.
	ExpiresInSeconds int64  `json:"expiresInSeconds"`
	ExpiresHuman     string `json:"expiresHuman"`
	// Status is one of "fresh", "soon", or "expired", matching the live
	// table's color buckets so a script can branch the same way the eye does.
	Status    string `json:"status"`
	OriginCwd string `json:"originCwd"`
	// Secret is true when the scratch tripped the secret tripwire (the same
	// signal the 🔑 marker shows in `sp ls`). Scripts can gate a bulk promote on
	// `.[] | select(.secret)` without shelling out to `sp scan` per id.
	Secret bool `json:"secret"`
	// Pinned is true when the scratch is exempt from reaping (the same signal
	// the 📌 marker / PIN token shows in `sp ls`). Scripts can list what will
	// survive the next reap with `.[] | select(.pinned)`.
	Pinned bool `json:"pinned"`
}

// MorgueJSON is the scriptable record for a soft-deleted scratch under
// `sp ls --morgue --json`. It swaps expiry for purge timing, matching the
// morgue table's PURGES column.
type MorgueJSON struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Tags         []string  `json:"tags"`
	Ext          string    `json:"ext"`
	Size         int64     `json:"size"`
	SizeHuman    string    `json:"sizeHuman"`
	CreatedAt    time.Time `json:"createdAt"`
	DeletedAt    time.Time `json:"deletedAt"`
	DeletedHuman string    `json:"deletedHuman"`
	PurgeAt      time.Time `json:"purgeAt"`
	// PurgeInSeconds is time-until-hard-deletion in whole seconds; <= 0 means
	// the item is already past its grace window and eligible for reaping.
	PurgeInSeconds int64  `json:"purgeInSeconds"`
	PurgeHuman     string `json:"purgeHuman"`
	// Purgeable reports whether the item is past grace (ready to be reaped).
	Purgeable bool `json:"purgeable"`
}

// scratchJSON builds a ScratchJSON view of a live scratch as of now. It reuses
// the same humanizers and lifecycle classification the tables use, so the two
// renderings can never disagree about age, expiry phrasing, or status. secret
// is threaded in from the caller's tripwire scan (the JSON layer stays pure and
// does no scanning itself).
func scratchJSON(s index.Scratch, now time.Time, secret bool) ScratchJSON {
	return ScratchJSON{
		ID:               s.ID,
		Name:             s.Name,
		Tags:             tagsOrEmpty(s.Tags),
		Ext:              s.Ext,
		Size:             s.Size,
		SizeHuman:        humanSize(s.Size),
		CreatedAt:        s.CreatedAt,
		AgeHuman:         humanAge(now.Sub(s.CreatedAt)),
		ExpiresAt:        s.ExpiresAt,
		ExpiresInSeconds: int64(s.ExpiresAt.Sub(now) / time.Second),
		ExpiresHuman:     humanExpiry(s.ExpiresAt.Sub(now)),
		Status:           statusString(classify(s, now)),
		OriginCwd:        s.OriginCwd,
		Secret:           secret,
		Pinned:           s.Pinned,
	}
}

// morgueJSON builds a MorgueJSON view of a soft-deleted scratch as of now.
func morgueJSON(r MorgueRow, now time.Time) MorgueJSON {
	del := deletedAt(r.Scratch)
	return MorgueJSON{
		ID:             r.Scratch.ID,
		Name:           r.Scratch.Name,
		Tags:           tagsOrEmpty(r.Scratch.Tags),
		Ext:            r.Scratch.Ext,
		Size:           r.Scratch.Size,
		SizeHuman:      humanSize(r.Scratch.Size),
		CreatedAt:      r.Scratch.CreatedAt,
		DeletedAt:      del,
		DeletedHuman:   humanAge(now.Sub(del)),
		PurgeAt:        r.PurgeAt,
		PurgeInSeconds: int64(r.PurgeAt.Sub(now) / time.Second),
		PurgeHuman:     humanPurge(r.PurgeAt.Sub(now)),
		Purgeable:      !now.Before(r.PurgeAt),
	}
}

// TableJSON writes live scratches to w as a JSON array of ScratchJSON records,
// newest-first to match the table ordering. An empty store emits "[]\n" rather
// than null, so consumers can always treat the output as an array. Unlike the
// table, this path is intentionally color- and personality-free.
func TableJSON(w io.Writer, scratches []index.Scratch, now time.Time) error {
	return TableMarkedJSON(w, scratches, nil, now)
}

// TableMarkedJSON is TableJSON with an optional per-scratch tripwire marker set:
// any id in markers gets "secret": true in its record. markers may be nil, in
// which case every record reports secret=false. As with TableMarked, the marker
// is a side map so the store/index never learn about the tripwire.
func TableMarkedJSON(w io.Writer, scratches []index.Scratch, markers map[string]bool, now time.Time) error {
	ordered := sortLive(scratches)
	records := make([]ScratchJSON, 0, len(ordered))
	for _, s := range ordered {
		records = append(records, scratchJSON(s, now, markers[s.ID]))
	}
	return writeJSON(w, records)
}

// MorgueTableJSON writes soft-deleted scratches to w as a JSON array of
// MorgueJSON records, newest-deleted-first. As with TableJSON, an empty morgue
// emits "[]\n".
func MorgueTableJSON(w io.Writer, rows []MorgueRow, now time.Time) error {
	ordered := sortMorgue(rows)
	records := make([]MorgueJSON, 0, len(ordered))
	for _, r := range ordered {
		records = append(records, morgueJSON(r, now))
	}
	return writeJSON(w, records)
}

// writeJSON encodes v as indented JSON with a trailing newline. SetEscapeHTML
// is disabled so paths and names with &, <, > round-trip literally instead of
// as \u escapes — friendlier for the file paths scratches carry.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// tagsOrEmpty returns a non-nil slice so tags always serialize as [] rather
// than null, keeping the JSON shape stable for scripts.
func tagsOrEmpty(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// DoctorJSON is the scriptable record for `sp doctor --json`: the store's
// footprint and record counts plus any drift between the index and the
// filesystem. It mirrors the human report's information without its wording or
// color, so `sp doctor --json | jq '.healthy'` (or `.orphans`) stays a stable,
// scriptable contract. Sizes carry both raw bytes and a human string, matching
// the ls views, and the slices are always non-nil so the shape never flips to
// null on a clean store.
type DoctorJSON struct {
	// Healthy is the one-field summary: true when there are no orphans and no
	// missing content, so a script can gate on `.healthy` without inspecting the
	// arrays.
	Healthy bool `json:"healthy"`
	// LiveCount and MorgueCount are how many index records are in each set.
	LiveCount   int `json:"liveCount"`
	MorgueCount int `json:"morgueCount"`
	// TrackedSize is the bytes held by content that has an index entry;
	// OrphanSize is the bytes held by orphaned files; TotalSize is their sum.
	// Each carries a human companion so both scripts and eyeballs are served.
	TrackedSize      int64  `json:"trackedSize"`
	TrackedSizeHuman string `json:"trackedSizeHuman"`
	OrphanSize       int64  `json:"orphanSize"`
	OrphanSizeHuman  string `json:"orphanSizeHuman"`
	TotalSize        int64  `json:"totalSize"`
	TotalSizeHuman   string `json:"totalSizeHuman"`
	// Orphans are content files with no index entry; Missing are index entries
	// with no content file. Both mirror the human report's sections.
	Orphans []DoctorOrphanJSON  `json:"orphans"`
	Missing []DoctorMissingJSON `json:"missing"`
}

// DoctorOrphanJSON is the scriptable view of an orphaned content file: bytes on
// disk the index has forgotten how to describe.
type DoctorOrphanJSON struct {
	Path      string `json:"path"`
	Area      string `json:"area"`
	Size      int64  `json:"size"`
	SizeHuman string `json:"sizeHuman"`
}

// DoctorMissingJSON is the scriptable view of an index entry whose content file
// is gone: the id/name to name it and where the content was expected.
type DoctorMissingJSON struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ExpectedPath string `json:"expectedPath"`
}

// DoctorReportJSON writes a DoctorReportData to w as a single DoctorJSON object.
// Like the ls JSON paths it is intentionally color- and personality-free: pure
// data so `sp doctor --json` is a stable scripting contract. The orphan/missing
// slices are always emitted as arrays (never null) so consumers can iterate
// unconditionally, and ordering is inherited from the store's already-sorted
// Diagnosis (orphans by path, missing by id).
func DoctorReportJSON(w io.Writer, d DoctorReportData) error {
	orphans := make([]DoctorOrphanJSON, 0, len(d.Orphans))
	for _, o := range d.Orphans {
		orphans = append(orphans, DoctorOrphanJSON{
			Path:      o.Path,
			Area:      o.Area,
			Size:      o.Size,
			SizeHuman: humanSize(o.Size),
		})
	}
	missing := make([]DoctorMissingJSON, 0, len(d.Missing))
	for _, m := range d.Missing {
		missing = append(missing, DoctorMissingJSON{
			ID:           m.ID,
			Name:         m.Name,
			ExpectedPath: m.ExpectedPath,
		})
	}
	rec := DoctorJSON{
		Healthy:          d.healthy(),
		LiveCount:        d.LiveCount,
		MorgueCount:      d.MorgueCount,
		TrackedSize:      d.TrackedSize,
		TrackedSizeHuman: humanSize(d.TrackedSize),
		OrphanSize:       d.OrphanSize,
		OrphanSizeHuman:  humanSize(d.OrphanSize),
		TotalSize:        d.totalSize(),
		TotalSizeHuman:   humanSize(d.totalSize()),
		Orphans:          orphans,
		Missing:          missing,
	}
	return writeJSON(w, rec)
}

// statusString maps a lifecycle bucket to its JSON status token.
func statusString(l lifecycle) string {
	switch l {
	case expired:
		return "expired"
	case soon:
		return "soon"
	default:
		return "fresh"
	}
}
