// transfer.go implements `sp export` / `sp import`: snapshotting the whole
// store to a single .tar.gz and restoring it elsewhere.
//
// The store is "just files" by design (an index.json plus per-scratch content
// under scratches/ and morgue/), so a portable snapshot is a tarball of a
// self-describing manifest plus the content files. We deliberately do NOT ship
// the raw on-disk index.json: exporting a manifest of exactly the scratches we
// bundle keeps import's merge/replace semantics honest even if the source
// store changes between listing and archiving.
//
// stdlib only: archive/tar + compress/gzip, no new dependencies.
package store

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// manifestName is the tar entry holding the exported metadata. It is read
// first on import; content entries follow under scratches/ and morgue/.
const manifestName = "scratchpatch.json"

// manifestSchema versions the export format so a future breaking change to the
// tarball layout can be detected and rejected rather than silently mishandled.
const manifestSchema = 1

// manifest is the JSON object stored at manifestName inside the tarball. It
// carries the exported scratch records plus a little provenance.
type manifest struct {
	Schema     int             `json:"schema"`
	ExportedAt time.Time       `json:"exportedAt"`
	Scratches  []index.Scratch `json:"scratches"`
}

// ExportOptions controls what Export bundles.
type ExportOptions struct {
	// IncludeMorgue also archives soft-deleted (morgued) scratches. When
	// false (default), only live scratches are exported.
	IncludeMorgue bool
}

// Export writes a .tar.gz snapshot of the store to w. It archives a manifest
// of the exported scratches followed by each scratch's content file. Live
// scratches always go in; morgued ones only when opts.IncludeMorgue is set.
func (s *Store) Export(w io.Writer, opts ExportOptions) error {
	all, err := s.idx.List()
	if err != nil {
		return fmt.Errorf("export: read index: %w", err)
	}

	var picked []index.Scratch
	for _, sc := range all {
		if sc.Morgued() && !opts.IncludeMorgue {
			continue
		}
		picked = append(picked, sc)
	}

	// Stable, deterministic ordering (id) so exports are reproducible.
	sort.Slice(picked, func(i, j int) bool { return picked[i].ID < picked[j].ID })

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	m := manifest{
		Schema:     manifestSchema,
		ExportedAt: time.Now().UTC(),
		Scratches:  picked,
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("export: encode manifest: %w", err)
	}
	if err := writeTarBytes(tw, manifestName, mb); err != nil {
		return err
	}

	for _, sc := range picked {
		src := s.LivePath(sc)
		data, err := os.ReadFile(src)
		if err != nil {
			// A missing content file is a real inconsistency; surface it
			// rather than shipping a manifest entry with no bytes.
			return fmt.Errorf("export: read content for %s: %w", sc.ID, err)
		}
		if err := writeTarBytes(tw, tarEntryName(sc), data); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("export: finalize tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("export: finalize gzip: %w", err)
	}
	return nil
}

// ImportMode selects how Import reconciles incoming scratches with the
// existing store.
type ImportMode int

const (
	// ImportMerge adds incoming scratches; on id collision it keeps the
	// existing scratch and records the incoming one as skipped. It never
	// clobbers existing content. This is the default.
	ImportMerge ImportMode = iota

	// ImportReplace backs up the current store, then replaces it with the
	// tarball's contents. Destructive and therefore must be explicit.
	ImportReplace
)

// ImportResult reports what Import did.
type ImportResult struct {
	// Added are the ids of scratches written into the store.
	Added []string
	// Skipped are incoming ids that collided with an existing scratch
	// (merge mode only).
	Skipped []string
	// BackupPath is where the pre-replace backup was written (replace mode).
	BackupPath string
}

// Import restores scratches from a .tar.gz produced by Export. See ImportMode
// for the reconciliation rules.
func (s *Store) Import(r io.Reader, mode ImportMode) (ImportResult, error) {
	if mode == ImportReplace {
		return s.importReplace(r)
	}
	return s.importMerge(r)
}

// importMerge adds incoming scratches without ever overwriting existing ids.
func (s *Store) importMerge(r io.Reader) (ImportResult, error) {
	var res ImportResult

	existing := map[string]bool{}
	cur, err := s.idx.List()
	if err != nil {
		return res, fmt.Errorf("import: read index: %w", err)
	}
	for _, sc := range cur {
		existing[sc.ID] = true
	}

	m, contents, err := readArchive(r)
	if err != nil {
		return res, err
	}

	for _, sc := range m.Scratches {
		if existing[sc.ID] {
			res.Skipped = append(res.Skipped, sc.ID)
			continue
		}
		data, ok := contents[tarEntryName(sc)]
		if !ok {
			return res, fmt.Errorf("import: tarball missing content for %s", sc.ID)
		}
		if err := s.writeScratch(sc, data); err != nil {
			return res, err
		}
		res.Added = append(res.Added, sc.ID)
		existing[sc.ID] = true // guard against duplicate ids within one tarball
	}

	sort.Strings(res.Added)
	sort.Strings(res.Skipped)
	return res, nil
}

// importReplace backs up the current store to a timestamped tarball, wipes the
// live/morgue content and index, then imports everything from the incoming
// archive. The backup makes the destructive path recoverable.
func (s *Store) importReplace(r io.Reader) (ImportResult, error) {
	var res ImportResult

	// Read the incoming archive fully before touching anything on disk, so a
	// malformed tarball can't leave us half-wiped.
	m, contents, err := readArchive(r)
	if err != nil {
		return res, err
	}

	// Back up the current store (including morgue) next to the store root.
	backup := fmt.Sprintf("%s-backup-%s.tar.gz", s.cfg.Home, time.Now().UTC().Format("20060102-150405"))
	bf, err := os.Create(backup)
	if err != nil {
		return res, fmt.Errorf("import: create backup: %w", err)
	}
	if err := s.Export(bf, ExportOptions{IncludeMorgue: true}); err != nil {
		_ = bf.Close()
		return res, fmt.Errorf("import: write backup: %w", err)
	}
	if err := bf.Close(); err != nil {
		return res, fmt.Errorf("import: close backup: %w", err)
	}
	res.BackupPath = backup

	// Wipe existing content and reset the index.
	cur, err := s.idx.List()
	if err != nil {
		return res, fmt.Errorf("import: read index: %w", err)
	}
	for _, sc := range cur {
		if err := os.Remove(s.LivePath(sc)); err != nil && !os.IsNotExist(err) {
			return res, fmt.Errorf("import: remove old content for %s: %w", sc.ID, err)
		}
		if err := s.idx.Delete(sc.ID); err != nil {
			return res, fmt.Errorf("import: reset index for %s: %w", sc.ID, err)
		}
	}

	seen := map[string]bool{}
	for _, sc := range m.Scratches {
		if seen[sc.ID] {
			continue
		}
		data, ok := contents[tarEntryName(sc)]
		if !ok {
			return res, fmt.Errorf("import: tarball missing content for %s", sc.ID)
		}
		if err := s.writeScratch(sc, data); err != nil {
			return res, err
		}
		res.Added = append(res.Added, sc.ID)
		seen[sc.ID] = true
	}

	sort.Strings(res.Added)
	return res, nil
}

// writeScratch persists one incoming scratch: its content to the right
// directory (scratches/ or morgue/ depending on Morgued) and its metadata to
// the index.
func (s *Store) writeScratch(sc index.Scratch, data []byte) error {
	dst := s.LivePath(sc)
	if err := os.WriteFile(dst, data, filePerm); err != nil {
		return fmt.Errorf("import: write content for %s: %w", sc.ID, err)
	}
	if err := s.idx.Put(sc); err != nil {
		return fmt.Errorf("import: index %s: %w", sc.ID, err)
	}
	return nil
}

// tarEntryName is the in-tarball path for a scratch's content: it mirrors the
// on-disk layout (scratches/ or morgue/) so the archive is self-explanatory.
func tarEntryName(sc index.Scratch) string {
	name := sc.ID
	if sc.Ext != "" {
		name += "." + sc.Ext
	}
	dir := "scratches"
	if sc.Morgued() {
		dir = "morgue"
	}
	return path.Join(dir, name)
}

// writeTarBytes writes a single regular-file entry into tw.
func writeTarBytes(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    int64(filePerm),
		Size:    int64(len(data)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("export: write header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("export: write body %s: %w", name, err)
	}
	return nil
}

// readArchive decodes a .tar.gz into its manifest and a map of content-entry
// name → bytes. It validates the manifest schema and requires the manifest to
// be present.
func readArchive(r io.Reader) (manifest, map[string][]byte, error) {
	var m manifest
	contents := map[string][]byte{}
	haveManifest := false

	gz, err := gzip.NewReader(r)
	if err != nil {
		return m, nil, fmt.Errorf("import: open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, nil, fmt.Errorf("import: read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return m, nil, fmt.Errorf("import: read entry %s: %w", hdr.Name, err)
		}
		if hdr.Name == manifestName {
			if err := json.Unmarshal(data, &m); err != nil {
				return m, nil, fmt.Errorf("import: parse manifest: %w", err)
			}
			haveManifest = true
			continue
		}
		contents[path.Clean(hdr.Name)] = data
	}

	if !haveManifest {
		return m, nil, fmt.Errorf("import: not a scratchpatch export (missing %s)", manifestName)
	}
	if m.Schema != manifestSchema {
		return m, nil, fmt.Errorf("import: unsupported export schema %d (want %d)", m.Schema, manifestSchema)
	}
	return m, contents, nil
}
