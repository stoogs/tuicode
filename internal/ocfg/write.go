package ocfg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Writer writes opencode.json with backups.
type Writer struct {
	// BackupDir is where timestamped backups are kept (e.g. ~/.config/tuicode/backups).
	BackupDir string
	// Keep is how many backups to retain (default 10).
	Keep int
	// DryRun, when true, performs no filesystem changes.
	DryRun bool
	// DefaultModel, when set, also writes the top-level "model" field as
	// "ollama/<tag>" so OpenCode opens with this model selected.
	DefaultModel string
	// Now returns the current time (overridable in tests).
	Now func() time.Time
	// Log receives action lines (for --verbose / --dry-run).
	Log func(string)
}

func (w *Writer) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Writer) logf(format string, a ...any) {
	if w.Log != nil {
		w.Log(fmt.Sprintf(format, a...))
	}
}

// WriteResult reports what a write did.
type WriteResult struct {
	Path       string
	BackupPath string // empty if no prior file existed or dry-run
	Changed    bool   // false when the on-disk content was already identical
	DryRun     bool
}

// Write merges the given models into the opencode.json at path and writes it,
// backing up any existing file first. It is idempotent: if the merged result
// equals the current file contents byte-for-byte, no write or backup occurs.
func (w *Writer) Write(path string, models []ModelEntry) (WriteResult, error) {
	res := WriteResult{Path: path, DryRun: w.DryRun}

	existing, err := os.ReadFile(path)
	hadFile := err == nil
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}

	doc, err := Read(path)
	if err != nil {
		return res, fmt.Errorf("read %s: %w", path, err)
	}
	MergeOllama(doc, models)
	if w.DefaultModel != "" {
		// Make OpenCode default to this model on open.
		doc["model"] = providerID + "/" + w.DefaultModel
	}
	out, err := Marshal(doc)
	if err != nil {
		return res, err
	}

	// Idempotency: skip if unchanged.
	if hadFile && string(existing) == string(out) {
		w.logf("opencode.json unchanged: %s", path)
		res.Changed = false
		return res, nil
	}
	res.Changed = true

	if w.DryRun {
		w.logf("DRY-RUN would write opencode.json: %s", path)
		if hadFile {
			w.logf("DRY-RUN would back up existing file first")
		}
		return res, nil
	}

	// Backup the existing file before writing.
	if hadFile {
		bpath, err := w.backup(existing)
		if err != nil {
			return res, fmt.Errorf("backup: %w", err)
		}
		res.BackupPath = bpath
		w.logf("backed up opencode.json → %s", bpath)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return res, err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return res, err
	}
	w.logf("wrote opencode.json: %s", path)
	return res, nil
}

// backup copies content to BackupDir/opencode.<timestamp>.json and prunes to Keep.
func (w *Writer) backup(content []byte) (string, error) {
	dir := w.BackupDir
	if dir == "" {
		return "", fmt.Errorf("no backup dir configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ts := w.now().Format("20060102-150405.000")
	bpath := filepath.Join(dir, "opencode."+ts+".json")
	if err := writeFileAtomic(bpath, content, 0o644); err != nil {
		return "", err
	}
	w.prune()
	return bpath, nil
}

// prune keeps only the newest Keep backups.
func (w *Writer) prune() {
	keep := w.Keep
	if keep <= 0 {
		keep = 10
	}
	entries, err := os.ReadDir(w.BackupDir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "opencode.") && strings.HasSuffix(n, ".json") {
			names = append(names, n)
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names) // timestamped names sort chronologically
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(w.BackupDir, n))
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
