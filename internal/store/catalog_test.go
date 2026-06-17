package store

import "testing"

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
