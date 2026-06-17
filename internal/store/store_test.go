package store

import (
	"testing"
)

func TestAliasFor(t *testing.T) {
	cases := map[string]string{
		"qwen3-coder:30b": "qwen3-coder-30b",
		"gemma3:270m":     "gemma3-270m",
		"library/foo:1b":  "library-foo-1b",
	}
	for in, want := range cases {
		if got := AliasFor(in); got != want {
			t.Errorf("AliasFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayNameFor(t *testing.T) {
	cases := map[string]string{
		"qwen3-coder:30b": "Qwen3 Coder 30B",
		"gemma3:270m":     "Gemma3 270M",
	}
	for in, want := range cases {
		if got := DisplayNameFor(in); got != want {
			t.Errorf("DisplayNameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResidencyKeepAlive(t *testing.T) {
	cases := []struct {
		r    Residency
		want string
	}{
		{Residency{Mode: AutoUnload, IdleMinutes: 20}, "20m"},
		{Residency{Mode: AutoUnload}, "20m"}, // default minutes
		{Residency{Mode: KeepWarm}, "-1"},
		{Residency{Mode: Manual}, "24h"},
	}
	for _, c := range cases {
		if got := c.r.KeepAlive(); got != c.want {
			t.Errorf("%+v KeepAlive() = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestAppConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Dir: dir}
	cfg := DefaultAppConfig()
	cfg.DeviceMode = "cpu-only"
	cfg.DefaultResidency = Residency{Mode: KeepWarm}
	if err := s.SaveAppConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadAppConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceMode != "cpu-only" {
		t.Errorf("device mode = %q", got.DeviceMode)
	}
	if got.DefaultResidency.Mode != KeepWarm {
		t.Errorf("residency = %q", got.DefaultResidency.Mode)
	}
}

func TestAppConfigDefaultsWhenMissing(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	cfg, err := s.LoadAppConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceMode != "auto" {
		t.Errorf("default device mode = %q", cfg.DeviceMode)
	}
}

func TestModelConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Dir: dir}
	cfg := DefaultModelConfig("qwen3-coder:30b", DefaultResidency())
	cfg.ContextLength = 65536
	cfg.ParamsBillions = 30
	cfg.Quant = "Q4_K_M"
	if err := s.SaveModelConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LoadModelConfig("qwen3-coder:30b")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.ContextLength != 65536 {
		t.Errorf("context = %d", got.ContextLength)
	}
	if got.Alias != "qwen3-coder-30b" {
		t.Errorf("alias = %q", got.Alias)
	}
}

func TestGetOrDefaultModelConfig(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	cfg, err := s.GetOrDefaultModelConfig("new:1b", DefaultResidency())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelTag != "new:1b" {
		t.Errorf("tag = %q", cfg.ModelTag)
	}
	if cfg.Parameters != PresetCoding {
		t.Errorf("params = %+v, want coding preset", cfg.Parameters)
	}
}
