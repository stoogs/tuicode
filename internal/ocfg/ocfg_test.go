package ocfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMergeEmptyAddsModel(t *testing.T) {
	doc := minimalDoc()
	MergeOllama(doc, []ModelEntry{{Tag: "qwen3-coder:30b", DisplayName: "Qwen3 Coder 30B"}})
	if !doc.HasModel("qwen3-coder:30b") {
		t.Fatal("model not registered")
	}
	out, _ := Marshal(doc)
	var rt map[string]any
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	prov := rt["provider"].(map[string]any)["ollama"].(map[string]any)
	if prov["npm"] != providerNPM {
		t.Errorf("npm = %v", prov["npm"])
	}
	opts := prov["options"].(map[string]any)
	if opts["baseURL"] != baseURL {
		t.Errorf("baseURL = %v", opts["baseURL"])
	}
}

func TestMergePreservesOtherProviders(t *testing.T) {
	doc := Doc{
		"$schema": schemaURL,
		"provider": map[string]any{
			"openai": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "OpenAI",
			},
		},
		"theme": "tokyonight",
	}
	MergeOllama(doc, []ModelEntry{{Tag: "gemma3:270m", DisplayName: "Gemma 3 270M"}})

	prov := doc["provider"].(map[string]any)
	if _, ok := prov["openai"]; !ok {
		t.Fatal("openai provider dropped")
	}
	openai := prov["openai"].(map[string]any)
	if openai["name"] != "OpenAI" {
		t.Errorf("openai name mutated: %v", openai["name"])
	}
	if doc["theme"] != "tokyonight" {
		t.Errorf("top-level theme dropped: %v", doc["theme"])
	}
	if !doc.HasModel("gemma3:270m") {
		t.Error("ollama model not added")
	}
}

func TestMergePreservesCustomProviderName(t *testing.T) {
	doc := Doc{
		"provider": map[string]any{
			"ollama": map[string]any{"name": "My Local Rig"},
		},
	}
	MergeOllama(doc, []ModelEntry{{Tag: "x:1b"}})
	got := doc["provider"].(map[string]any)["ollama"].(map[string]any)["name"]
	if got != "My Local Rig" {
		t.Errorf("custom name overwritten: %v", got)
	}
}

func TestMarshalIdempotent(t *testing.T) {
	models := []ModelEntry{
		{Tag: "qwen3-coder:30b", DisplayName: "Qwen3 Coder 30B"},
		{Tag: "gemma3:270m", DisplayName: "Gemma 3 270M"},
	}
	doc1 := minimalDoc()
	MergeOllama(doc1, models)
	out1, _ := Marshal(doc1)

	// Re-read the output and merge again → must be byte-identical.
	var doc2 Doc
	json.Unmarshal(out1, &doc2)
	MergeOllama(doc2, models)
	out2, _ := Marshal(doc2)

	if string(out1) != string(out2) {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out1, out2)
	}
}

func TestMergeWritesModelLimit(t *testing.T) {
	doc := minimalDoc()
	MergeOllama(doc, []ModelEntry{{Tag: "tuicode/x:tuned", DisplayName: "X (ctx 32k)", Context: 32768, Output: 8192}})
	out, _ := Marshal(doc)
	var rt map[string]any
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	models := rt["provider"].(map[string]any)["ollama"].(map[string]any)["models"].(map[string]any)
	entry := models["tuicode/x:tuned"].(map[string]any)
	limit, ok := entry["limit"].(map[string]any)
	if !ok {
		t.Fatalf("limit not written: %v", entry)
	}
	if limit["context"].(float64) != 32768 || limit["output"].(float64) != 8192 {
		t.Errorf("limit = %v, want context 32768 / output 8192", limit)
	}

	// A zero context must NOT write a limit block.
	doc2 := minimalDoc()
	MergeOllama(doc2, []ModelEntry{{Tag: "y:1b", DisplayName: "Y"}})
	out2, _ := Marshal(doc2)
	if json.Valid(out2) {
		var rt2 map[string]any
		json.Unmarshal(out2, &rt2)
		e := rt2["provider"].(map[string]any)["ollama"].(map[string]any)["models"].(map[string]any)["y:1b"].(map[string]any)
		if _, has := e["limit"]; has {
			t.Errorf("limit should be omitted for zero context: %v", e)
		}
	}
}

func TestSetCompactionMergesPreservingUserKeys(t *testing.T) {
	doc := Doc{
		"$schema": schemaURL,
		"compaction": map[string]any{
			"auto":      false,  // user's value — tuicode overrides
			"customKey": "keep", // user's extra key — must survive
		},
	}
	SetCompaction(doc, map[string]any{"auto": true, "prune": true, "reserved": 8192})
	comp := doc["compaction"].(map[string]any)
	if comp["customKey"] != "keep" {
		t.Errorf("user key dropped: %v", comp)
	}
	if comp["auto"] != true || comp["prune"] != true || comp["reserved"] != 8192 {
		t.Errorf("managed keys not applied: %v", comp)
	}

	// A nil block must leave the existing compaction untouched.
	SetCompaction(doc, nil)
	if _, ok := doc["compaction"]; !ok {
		t.Error("nil block should not drop compaction")
	}
}

func TestWriteCompactionBlockIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	clock := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	mk := func() *Writer {
		return &Writer{
			BackupDir:    filepath.Join(dir, "backups"),
			Keep:         10,
			DefaultModel: "tuicode/x:tuned",
			Compaction:   map[string]any{"auto": true, "prune": true, "reserved": 8192},
			Now:          func() time.Time { clock = clock.Add(time.Second); return clock },
		}
	}
	models := []ModelEntry{{Tag: "tuicode/x:tuned", DisplayName: "X (ctx 32k)", Context: 32768, Output: 8192}}

	if _, err := mk().Write(path, models); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	comp, ok := doc["compaction"].(map[string]any)
	if !ok {
		t.Fatalf("compaction block not written: %v", doc)
	}
	if comp["auto"] != true || comp["prune"] != true || comp["reserved"].(float64) != 8192 {
		t.Errorf("compaction = %v", comp)
	}

	// Second write with identical inputs must be a no-op (idempotent).
	res, err := mk().Write(path, models)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("second write changed the file; compaction/limit not idempotent")
	}
}

func TestWriteIdempotentNoBackupSpam(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	backups := filepath.Join(dir, "backups")
	clock := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	w := &Writer{BackupDir: backups, Keep: 10, Now: func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}}
	models := []ModelEntry{{Tag: "qwen3-coder:30b", DisplayName: "Qwen3 Coder 30B"}}

	r1, err := w.Write(path, models)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Changed {
		t.Error("first write should change")
	}
	if r1.BackupPath != "" {
		t.Error("first write (no prior file) should not back up")
	}

	r2, err := w.Write(path, models)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Changed {
		t.Error("second identical write should be a no-op")
	}

	// No backups should have been created (no content change ever overwrote a file).
	if entries, _ := os.ReadDir(backups); len(entries) != 0 {
		t.Errorf("expected 0 backups, got %d", len(entries))
	}
}

func TestWriteBacksUpOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	backups := filepath.Join(dir, "backups")
	n := 0
	w := &Writer{BackupDir: backups, Keep: 10, Now: func() time.Time {
		n++
		return time.Date(2025, 1, 2, 3, 4, n, 0, time.UTC)
	}}

	if _, err := w.Write(path, []ModelEntry{{Tag: "a:1b"}}); err != nil {
		t.Fatal(err)
	}
	// Changing the model set forces a real overwrite → one backup.
	if _, err := w.Write(path, []ModelEntry{{Tag: "a:1b"}, {Tag: "b:2b"}}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(backups)
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(entries))
	}
}

func TestBackupRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	backups := filepath.Join(dir, "backups")
	sec := 0
	w := &Writer{BackupDir: backups, Keep: 10, Now: func() time.Time {
		sec++
		return time.Date(2025, 1, 1, 0, 0, 0, sec*1e6, time.UTC)
	}}
	// Write 15 distinct contents → 14 backups of prior files, pruned to 10.
	for i := 0; i < 15; i++ {
		models := make([]ModelEntry, i+1)
		for j := range models {
			models[j] = ModelEntry{Tag: "m" + itoa(j) + ":1b"}
		}
		if _, err := w.Write(path, models); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(backups)
	if len(entries) > 10 {
		t.Errorf("retention failed: %d backups (want ≤10)", len(entries))
	}
}

func TestWriteSetsDefaultModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	w := &Writer{
		BackupDir:    filepath.Join(dir, "backups"),
		DefaultModel: "qwen3-coder:30b",
		Now:          func() time.Time { return time.Unix(0, 0) },
	}
	if _, err := w.Write(path, []ModelEntry{{Tag: "qwen3-coder:30b", DisplayName: "Qwen3 Coder 30B"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "ollama/qwen3-coder:30b" {
		t.Errorf("model = %v, want ollama/qwen3-coder:30b", doc["model"])
	}
	// Still idempotent with the default-model set.
	r2, err := w.Write(path, []ModelEntry{{Tag: "qwen3-coder:30b", DisplayName: "Qwen3 Coder 30B"}})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Changed {
		t.Errorf("second write should be a no-op")
	}
}

func TestWriteDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	w := &Writer{BackupDir: filepath.Join(dir, "backups"), DryRun: true}
	r, err := w.Write(path, []ModelEntry{{Tag: "x:1b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Changed || !r.DryRun {
		t.Errorf("dry-run result = %+v", r)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dry-run wrote a file")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
