// stats.go renders `sp stats`: the store's fun-but-useful metrics as a
// tombstone-flavored report (human) or a stable object (--json).
//
// As with the doctor and ls renderers, render takes flattened plain data
// (StatsData) rather than importing the store package, keeping the dependency
// arrow one-way (cli/store → render). The JSON path carries no color and no
// personality — pure data so `sp stats --json | jq` stays a scripting contract.
package render

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// StatsTag is a render-facing tag/count pair for the breakdown line.
type StatsTag struct {
	Tag   string
	Count int
}

// StatsData is the flattened metrics view render prints. The cli layer adapts
// the store's Stats into this so render never learns the store's types.
type StatsData struct {
	LiveCount      int
	LiveBytes      int64
	MorgueCount    int
	MorgueBytes    int64
	PurgeableCount int
	TotalBytes     int64

	// OldestID is empty when the store has no live scratches.
	OldestID   string
	OldestName string
	OldestAge  time.Duration

	Tags  []StatsTag
	Grace time.Duration
}

// empty reports whether the store holds nothing at all (no live, no morgue).
func (d StatsData) empty() bool {
	return d.LiveCount == 0 && d.MorgueCount == 0
}

// StatsReport writes the human-readable, optionally-colored stats report to w.
// An empty store gets a friendly zero-state rather than a wall of zeros. When
// color is set the headline leans on the fresh/soon palette for a little life.
func StatsReport(w io.Writer, d StatsData, color bool) error {
	pal := defaultPalette()
	var b strings.Builder

	if d.empty() {
		writeLine(&b, "the crypt is empty — no scratches living or dead. "+
			"Nothing has rotted, because nothing exists yet. `sp new` to begin.",
			color, pal.fresh)
		_, err := io.WriteString(w, b.String())
		return err
	}

	// Headline: the store's whole footprint — bytes rescued from a lonely death
	// in /tmp.
	writeLine(&b, fmt.Sprintf("scratchpatch is guarding %s across %s live and %s in the morgue "+
		"(%s that isn't rotting loose in /tmp)",
		humanSize(d.TotalBytes), countScratches(d.LiveCount),
		countScratches(d.MorgueCount), humanSize(d.TotalBytes)),
		color, pal.header)

	// The living.
	writeLine(&b, fmt.Sprintf("living: %s, %s on disk",
		countScratches(d.LiveCount), humanSize(d.LiveBytes)), color, pal.fresh)

	// Oldest survivor — the scratch that has dodged the reaper longest.
	if d.OldestID != "" {
		writeLine(&b, fmt.Sprintf("oldest survivor: %s %s, clinging on for %s",
			d.OldestID, nameOrDash(d.OldestName), humanAge(d.OldestAge)),
			color, pal.fresh)
	}

	// The dead (recoverable) — and how many are already on death row.
	morgueLine := fmt.Sprintf("morgue: %s, %s recoverable",
		countScratches(d.MorgueCount), humanSize(d.MorgueBytes))
	if d.PurgeableCount > 0 {
		morgueLine += fmt.Sprintf(" — %d past the %s grace window (reaping will hard-delete them)",
			d.PurgeableCount, humanAge(d.Grace))
	}
	writeLine(&b, morgueLine, color, pal.soon)

	// Tag breakdown, top-N by count.
	if len(d.Tags) > 0 {
		parts := make([]string, 0, len(d.Tags))
		for _, t := range d.Tags {
			parts = append(parts, fmt.Sprintf("%s (%d)", t.Tag, t.Count))
		}
		writeLine(&b, "top tags: "+strings.Join(parts, ", "), color, pal.header)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// StatsTagJSON is the scriptable tag/count pair.
type StatsTagJSON struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// StatsJSON is the scriptable object for `sp stats --json`. It mirrors the
// human report's information without wording or color, with raw bytes plus a
// human companion for each size and an oldest-survivor sub-object that is null
// when the store has no live scratches. Slices are always non-nil so the shape
// never flips to null.
type StatsJSON struct {
	LiveCount        int    `json:"liveCount"`
	LiveBytes        int64  `json:"liveBytes"`
	LiveBytesHuman   string `json:"liveBytesHuman"`
	MorgueCount      int    `json:"morgueCount"`
	MorgueBytes      int64  `json:"morgueBytes"`
	MorgueBytesHuman string `json:"morgueBytesHuman"`
	PurgeableCount   int    `json:"purgeableCount"`
	TotalBytes       int64  `json:"totalBytes"`
	TotalBytesHuman  string `json:"totalBytesHuman"`
	GraceSeconds     int64  `json:"graceSeconds"`

	// Oldest is the oldest living scratch, or null when there are none.
	Oldest *StatsOldestJSON `json:"oldest"`

	Tags []StatsTagJSON `json:"tags"`
}

// StatsOldestJSON describes the oldest surviving scratch for the JSON view.
type StatsOldestJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AgeSeconds int64  `json:"ageSeconds"`
	AgeHuman   string `json:"ageHuman"`
}

// StatsReportJSON writes a StatsData to w as a single StatsJSON object. Like the
// other --json paths it is color- and personality-free.
func StatsReportJSON(w io.Writer, d StatsData) error {
	tags := make([]StatsTagJSON, 0, len(d.Tags))
	for _, t := range d.Tags {
		tags = append(tags, StatsTagJSON{Tag: t.Tag, Count: t.Count})
	}

	rec := StatsJSON{
		LiveCount:        d.LiveCount,
		LiveBytes:        d.LiveBytes,
		LiveBytesHuman:   humanSize(d.LiveBytes),
		MorgueCount:      d.MorgueCount,
		MorgueBytes:      d.MorgueBytes,
		MorgueBytesHuman: humanSize(d.MorgueBytes),
		PurgeableCount:   d.PurgeableCount,
		TotalBytes:       d.TotalBytes,
		TotalBytesHuman:  humanSize(d.TotalBytes),
		GraceSeconds:     int64(d.Grace / time.Second),
		Tags:             tags,
	}
	if d.OldestID != "" {
		rec.Oldest = &StatsOldestJSON{
			ID:         d.OldestID,
			Name:       d.OldestName,
			AgeSeconds: int64(d.OldestAge / time.Second),
			AgeHuman:   humanAge(d.OldestAge),
		}
	}
	return writeJSON(w, rec)
}
