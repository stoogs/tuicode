package server

import (
	"encoding/json"
	"strings"
	"time"
)

// modelDetails is the nested "details" object common to /api/tags and /api/ps.
type modelDetails struct {
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// ParseTags parses a /api/tags (`ollama list`) response body.
func ParseTags(body []byte) ([]DiskModel, error) {
	var r struct {
		Models []struct {
			Name     string       `json:"name"`
			Model    string       `json:"model"`
			Modified time.Time    `json:"modified_at"`
			Size     int64        `json:"size"`
			Details  modelDetails `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := make([]DiskModel, 0, len(r.Models))
	for _, m := range r.Models {
		tag := m.Name
		if tag == "" {
			tag = m.Model
		}
		out = append(out, DiskModel{
			Tag:       tag,
			Size:      m.Size,
			Modified:  m.Modified,
			ParamSize: m.Details.ParameterSize,
			Quant:     m.Details.QuantizationLevel,
			Family:    m.Details.Family,
		})
	}
	return out, nil
}

// ParsePS parses a /api/ps (`ollama ps`) response body.
func ParsePS(body []byte) ([]LoadedModel, error) {
	var r struct {
		Models []struct {
			Name      string       `json:"name"`
			Model     string       `json:"model"`
			Size      int64        `json:"size"`
			SizeVRAM  int64        `json:"size_vram"`
			Context   int          `json:"context_length"`
			ExpiresAt time.Time    `json:"expires_at"`
			Details   modelDetails `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := make([]LoadedModel, 0, len(r.Models))
	for _, m := range r.Models {
		tag := m.Name
		if tag == "" {
			tag = m.Model
		}
		out = append(out, LoadedModel{
			Tag:       tag,
			Size:      m.Size,
			SizeVRAM:  m.SizeVRAM,
			Context:   m.Context,
			ExpiresAt: m.ExpiresAt,
			ParamSize: m.Details.ParameterSize,
			Quant:     m.Details.QuantizationLevel,
		})
	}
	return out, nil
}

// ParseShow parses a /api/show response body into ModelDetails.
func ParseShow(body []byte) (ModelDetails, error) {
	var r struct {
		Details      modelDetails   `json:"details"`
		ModelInfo    map[string]any `json:"model_info"`
		Capabilities []string       `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ModelDetails{}, err
	}
	d := ModelDetails{
		ParamSize:    r.Details.ParameterSize,
		Quant:        r.Details.QuantizationLevel,
		Family:       r.Details.Family,
		Capabilities: r.Capabilities,
	}
	// model_info holds "<arch>.context_length" / "<arch>.block_count" — but
	// multimodal models (e.g. gemma4) ALSO carry ".vision.block_count" and
	// ".audio.block_count". Use the main text-model values: prefer the exact
	// "<general.architecture>.<key>", and never pick a vision/audio submodule.
	arch, _ := r.ModelInfo["general.architecture"].(string)
	mainInt := func(suffix string) int {
		if arch != "" {
			if n, ok := r.ModelInfo[arch+suffix].(float64); ok {
				return int(n)
			}
		}
		for k, v := range r.ModelInfo {
			if !strings.HasSuffix(k, suffix) || strings.Contains(k, ".vision.") || strings.Contains(k, ".audio.") {
				continue
			}
			if n, ok := v.(float64); ok {
				return int(n)
			}
		}
		return 0
	}
	d.ContextLength = mainInt(".context_length")
	d.BlockCount = mainInt(".block_count")
	d.SlidingWindow = mainInt(".attention.sliding_window")
	return d, nil
}
