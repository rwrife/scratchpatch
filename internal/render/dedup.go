// dedup.go renders `sp dedup`: the human, tombstone-flavored duplicate report
// and its color/personality-free `--json` counterpart.
//
// Like the doctor views, render never imports the store package. The cli layer
// flattens store.DedupReport into the plain DedupData below, so this file works
// purely from render-owned types. The JSON path carries no color and no
// flavor — same stable-contract rule as `sp ls --json` / `sp doctor --json`.
package render

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// DedupMemberData is one scratch in a duplicate cluster, flattened for render.
type DedupMemberData struct {
	ID        string
	Name      string
	Size      int64
	CreatedAt time.Time
	Canonical bool
}

// DedupClusterData is a group of byte-identical scratches. Members are ordered
// canonical-first. WastedBytes is what the redundant copies cost.
type DedupClusterData struct {
	Digest      string
	Members     []DedupMemberData
	WastedBytes int64
}

// DedupData is the plain summary `sp dedup` renders.
type DedupData struct {
	Clusters     []DedupClusterData
	TotalWasted  int64
	ScannedCount int
	// Collapsed, when non-nil, means a --collapse run happened: it names what
	// moved to the morgue so the report can confirm the action.
	Collapsed *DedupCollapsedData
}

// DedupCollapsedData records the outcome of a --collapse run for the report.
type DedupCollapsedData struct {
	MovedIDs       []string
	ReclaimedBytes int64
}

func (d DedupData) clean() bool { return len(d.Clusters) == 0 }

// countClusters pluralizes a cluster count for the human report.
func countClusters(n int) string {
	if n == 1 {
		return "1 cluster"
	}
	return fmt.Sprintf("%d clusters", n)
}

// shortDigest trims a full sha256 to a glanceable prefix for the human report;
// the JSON path keeps the full digest.
func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// DedupReport writes the human, tombstone-flavored duplicate report to w.
// A unique store gets a clean, reassuring headline; a store with duplicates
// gets the clusters, each showing the canonical keep-copy and the redundant
// extras with the bytes they waste. When d.Collapsed is set, it also confirms
// what was moved to the morgue.
func DedupReport(w io.Writer, d DedupData, color bool) error {
	pal := defaultPalette()
	var b strings.Builder

	if d.clean() {
		headline := fmt.Sprintf("no duplicates — every one of the %s living scratches is one of a kind",
			countScratches(d.ScannedCount))
		writeLine(&b, headline, color, pal.fresh)
		_, err := io.WriteString(w, b.String())
		return err
	}

	headline := fmt.Sprintf("found %s of identical scratches — %s haunting the store in duplicate",
		countClusters(len(d.Clusters)), humanSize(d.TotalWasted))
	writeLine(&b, headline, color, pal.header)
	writeLine(&b, fmt.Sprintf("scanned %s live", countScratches(d.ScannedCount)), color, pal.header)

	for _, c := range d.Clusters {
		fmt.Fprintf(&b, "\ncluster %s — %d copies, %s reclaimable:\n",
			shortDigest(c.Digest), len(c.Members), humanSize(c.WastedBytes))
		for _, m := range c.Members {
			if m.Canonical {
				line := fmt.Sprintf("  %s  %s  %s  (canonical — the original, kept)",
					m.ID, nameOrDash(m.Name), humanSize(m.Size))
				writeLine(&b, line, color, pal.fresh)
				continue
			}
			line := fmt.Sprintf("  %s  %s  %s  (redundant)",
				m.ID, nameOrDash(m.Name), humanSize(m.Size))
			writeLine(&b, line, color, pal.soon)
		}
	}

	if d.Collapsed != nil {
		fmt.Fprintf(&b, "\ncollapsed %s to the morgue, %s reclaimed. "+
			"Nothing was hard-deleted — `sp resurrect` brings any copy back.\n",
			countScratches(len(d.Collapsed.MovedIDs)), humanSize(d.Collapsed.ReclaimedBytes))
	} else {
		fmt.Fprintf(&b, "\ndedup only reports; nothing was moved. "+
			"Re-run with --collapse to send the redundant copies to the morgue (never hard-deleted).\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// --- JSON views ------------------------------------------------------------

// DedupJSON is the scriptable record for `sp dedup --json`. Slices are always
// non-nil so the shape never flips to null on a clean store, mirroring the ls /
// doctor JSON contracts. No color, no flavor — pure data.
type DedupJSON struct {
	// Clean is the one-field summary so a script can gate on `.clean`.
	Clean            bool               `json:"clean"`
	ScannedCount     int                `json:"scannedCount"`
	ClusterCount     int                `json:"clusterCount"`
	TotalWasted      int64              `json:"totalWasted"`
	TotalWastedHuman string             `json:"totalWastedHuman"`
	Clusters         []DedupClusterJSON `json:"clusters"`
	// Collapsed is null unless a --collapse run happened, in which case it
	// reports what moved to the morgue.
	Collapsed *DedupCollapsedJSON `json:"collapsed"`
}

// DedupClusterJSON is the scriptable view of one duplicate cluster.
type DedupClusterJSON struct {
	Digest      string            `json:"digest"`
	Count       int               `json:"count"`
	WastedBytes int64             `json:"wastedBytes"`
	WastedHuman string            `json:"wastedHuman"`
	Members     []DedupMemberJSON `json:"members"`
}

// DedupMemberJSON is the scriptable view of one cluster member.
type DedupMemberJSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SizeHuman string    `json:"sizeHuman"`
	CreatedAt time.Time `json:"createdAt"`
	Canonical bool      `json:"canonical"`
}

// DedupCollapsedJSON is the scriptable view of a --collapse outcome.
type DedupCollapsedJSON struct {
	MovedIDs       []string `json:"movedIds"`
	ReclaimedBytes int64    `json:"reclaimedBytes"`
	ReclaimedHuman string   `json:"reclaimedHuman"`
}

// DedupReportJSON writes a DedupData to w as a single DedupJSON object.
// Like the other --json paths it is intentionally color- and personality-free,
// and every slice is emitted as an array (never null).
func DedupReportJSON(w io.Writer, d DedupData) error {
	clusters := make([]DedupClusterJSON, 0, len(d.Clusters))
	for _, c := range d.Clusters {
		members := make([]DedupMemberJSON, 0, len(c.Members))
		for _, m := range c.Members {
			members = append(members, DedupMemberJSON{
				ID:        m.ID,
				Name:      m.Name,
				Size:      m.Size,
				SizeHuman: humanSize(m.Size),
				CreatedAt: m.CreatedAt,
				Canonical: m.Canonical,
			})
		}
		clusters = append(clusters, DedupClusterJSON{
			Digest:      c.Digest,
			Count:       len(c.Members),
			WastedBytes: c.WastedBytes,
			WastedHuman: humanSize(c.WastedBytes),
			Members:     members,
		})
	}

	var collapsed *DedupCollapsedJSON
	if d.Collapsed != nil {
		moved := d.Collapsed.MovedIDs
		if moved == nil {
			moved = []string{}
		}
		collapsed = &DedupCollapsedJSON{
			MovedIDs:       moved,
			ReclaimedBytes: d.Collapsed.ReclaimedBytes,
			ReclaimedHuman: humanSize(d.Collapsed.ReclaimedBytes),
		}
	}

	rec := DedupJSON{
		Clean:            d.clean(),
		ScannedCount:     d.ScannedCount,
		ClusterCount:     len(d.Clusters),
		TotalWasted:      d.TotalWasted,
		TotalWastedHuman: humanSize(d.TotalWasted),
		Clusters:         clusters,
		Collapsed:        collapsed,
	}
	return writeJSON(w, rec)
}
