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
// renderings can never disagree about age, expiry phrasing, or status.
func scratchJSON(s index.Scratch, now time.Time) ScratchJSON {
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
	ordered := sortLive(scratches)
	records := make([]ScratchJSON, 0, len(ordered))
	for _, s := range ordered {
		records = append(records, scratchJSON(s, now))
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
