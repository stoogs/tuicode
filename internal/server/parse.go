package server

import (
	"encoding/json"
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
		Details   modelDetails   `json:"details"`
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ModelDetails{}, err
	}
	d := ModelDetails{
		ParamSize: r.Details.ParameterSize,
		Quant:     r.Details.QuantizationLevel,
		Family:    r.Details.Family,
	}
	// model_info holds "<family>.context_length" among many keys.
	for k, v := range r.ModelInfo {
		if len(k) > len(".context_length") && k[len(k)-len(".context_length"):] == ".context_length" {
			switch n := v.(type) {
			case float64:
				d.ContextLength = int(n)
			}
		}
	}
	return d, nil
}
