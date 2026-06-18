package store

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
)

// TrendingModel is one entry in the curated/cached trending pull list.
type TrendingModel struct {
	Tag     string  `json:"tag"`
	ParamsB float64 `json:"params_b"`
	Note    string  `json:"note,omitempty"`
}

// RecModel is one benchmark-reference entry: the memory footprint and CPU/GPU
// split observed for a model (on the reference GPU). tok/s is intentionally
// omitted — it varies too much across hardware to be useful here.
type RecModel struct {
	Tag        string  `json:"tag"`
	MemGB      float64 `json:"mem_gb"`      // RAM+VRAM used
	GPUPercent int     `json:"gpu_percent"` // share of the model on the GPU (100 = all GPU)
	ParamsB    float64 `json:"params_b,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// Recommended is the editable benchmark reference file (recommended.json).
type Recommended struct {
	Comment  string     `json:"_comment,omitempty"`
	RefGPUGB float64    `json:"ref_gpu_gb,omitempty"` // GPU the figures were measured on
	Models   []RecModel `json:"models"`
}

//go:embed data/trending.json
var defaultTrendingJSON []byte

//go:embed data/recommended.json
var defaultRecommendedJSON []byte

func (s *Store) trendingPath() string    { return filepath.Join(s.Dir, "trending.json") }
func (s *Store) recommendedPath() string { return filepath.Join(s.Dir, "recommended.json") }

// LoadTrending reads trending.json, seeding it from the embedded default on
// first run. trending.json is a cache: it may be reordered by the daily
// popularity refresh, so hand edits are not preserved.
func (s *Store) LoadTrending() ([]TrendingModel, error) {
	data, err := s.seedAndRead(s.trendingPath(), defaultTrendingJSON)
	if err != nil {
		data = defaultTrendingJSON
	}
	var list []TrendingModel
	if err := json.Unmarshal(data, &list); err != nil {
		// Corrupt user file → fall back to the embedded default.
		_ = json.Unmarshal(defaultTrendingJSON, &list)
	}
	return list, nil
}

// SaveTrending overwrites the trending.json cache (used by the daily refresh).
func (s *Store) SaveTrending(list []TrendingModel) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.trendingPath(), append(data, '\n'), 0o644)
}

// LoadRecommended reads recommended.json, seeding it from the embedded default
// on first run. The file is user-editable and never overwritten on disk, but new
// curated picks from the embedded default are topped up in memory at load (by
// tag) so existing installs gain models added in later versions — e.g. the
// small-machine entries — without losing edits to entries they already have.
func (s *Store) LoadRecommended() (Recommended, error) {
	data, err := s.seedAndRead(s.recommendedPath(), defaultRecommendedJSON)
	if err != nil {
		data = defaultRecommendedJSON
	}
	var rec Recommended
	if err := json.Unmarshal(data, &rec); err != nil {
		_ = json.Unmarshal(defaultRecommendedJSON, &rec)
	}
	return topUpRecommended(rec), nil
}

// topUpRecommended appends embedded-default entries whose tag isn't already
// present, so a previously-seeded file picks up newly-curated picks. User entries
// win on tag collision (we never replace an existing tag).
func topUpRecommended(rec Recommended) Recommended {
	var def Recommended
	if err := json.Unmarshal(defaultRecommendedJSON, &def); err != nil {
		return rec
	}
	have := make(map[string]bool, len(rec.Models))
	for _, m := range rec.Models {
		have[m.Tag] = true
	}
	for _, m := range def.Models {
		if !have[m.Tag] {
			rec.Models = append(rec.Models, m)
		}
	}
	return rec
}

// seedAndRead writes def to path if path doesn't exist yet, then reads path.
func (s *Store) seedAndRead(path string, def []byte) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(s.Dir, 0o755); mkErr == nil {
			_ = writeFileAtomic(path, def, 0o644)
		}
	}
	return os.ReadFile(path)
}
