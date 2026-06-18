package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestEmbeddedCatalogsParse(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	tr, err := s.LoadTrending()
	if err != nil || len(tr) == 0 {
		t.Fatalf("trending: err=%v n=%d", err, len(tr))
	}
	if tr[0].Tag == "" || tr[0].ParamsB == 0 {
		t.Errorf("trending entry malformed: %+v", tr[0])
	}
	rec, err := s.LoadRecommended()
	if err != nil || len(rec.Models) == 0 {
		t.Fatalf("recommended: err=%v n=%d", err, len(rec.Models))
	}
	if rec.RefGPUGB != 16 {
		t.Errorf("ref_gpu_gb = %v, want 16", rec.RefGPUGB)
	}
	var gotSplit bool
	for _, m := range rec.Models {
		if m.Tag == "qwen3-coder:30b" {
			gotSplit = true
			if m.GPUPercent != 75 || m.MemGB != 20 {
				t.Errorf("qwen3-coder:30b = %+v, want gpu 75 mem 20", m)
			}
		}
	}
	if !gotSplit {
		t.Error("qwen3-coder:30b missing from recommended")
	}
	// Seeded files should now exist on disk and reload identically.
	if _, err := s.LoadTrending(); err != nil {
		t.Errorf("reload trending: %v", err)
	}
}

func TestRecommendedTopsUpExistingFile(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	// Simulate an older install: a seeded file with only large models and one
	// user-edited entry.
	old := Recommended{RefGPUGB: 16, Models: []RecModel{
		{Tag: "qwen3:14b", MemGB: 99, GPUPercent: 50, Note: "my edit"},
		{Tag: "gpt-oss:120b", MemGB: 66, GPUPercent: 22},
	}}
	data, _ := json.MarshalIndent(old, "", "  ")
	if err := os.WriteFile(s.recommendedPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := s.LoadRecommended()
	if err != nil {
		t.Fatal(err)
	}
	byTag := map[string]RecModel{}
	for _, m := range rec.Models {
		byTag[m.Tag] = m
	}
	// New small curated picks are topped up so small machines have options.
	if _, ok := byTag["qwen2.5-coder:7b"]; !ok {
		t.Error("expected small curated pick qwen2.5-coder:7b to be topped up")
	}
	// User edits to an existing tag are preserved (not replaced by the default).
	if e := byTag["qwen3:14b"]; e.MemGB != 99 || e.Note != "my edit" {
		t.Errorf("user edit to qwen3:14b not preserved: %+v", e)
	}
	// No duplicate tags after top-up.
	seen := map[string]bool{}
	for _, m := range rec.Models {
		if seen[m.Tag] {
			t.Errorf("duplicate tag after top-up: %s", m.Tag)
		}
		seen[m.Tag] = true
	}
}
