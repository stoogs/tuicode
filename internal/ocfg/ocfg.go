// Package ocfg reads, merges, and writes opencode.json. Writes are non-
// destructive (deep-merge only provider.ollama), idempotent (same inputs →
// byte-identical output), and always backed up first.
package ocfg

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	schemaURL    = "https://opencode.ai/config.json"
	providerNPM  = "@ai-sdk/openai-compatible"
	providerName = "Ollama (local)"
	baseURL      = "http://localhost:11434/v1"
	providerID   = "ollama"
)

// Doc is a parsed opencode.json as a generic tree. Using maps preserves unknown
// keys and yields deterministic (alphabetically-keyed) output from json.Marshal,
// which is what makes writes idempotent.
type Doc map[string]any

// ModelEntry registers one Ollama model in opencode.json.
type ModelEntry struct {
	Tag         string // map key, e.g. "qwen3-coder:30b"
	DisplayName string // value {"name": ...}
}

// Read parses opencode.json at path. A missing file yields a minimal valid Doc.
func Read(path string) (Doc, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return minimalDoc(), nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return minimalDoc(), nil
	}
	var doc Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = minimalDoc()
	}
	return doc, nil
}

func minimalDoc() Doc {
	return Doc{"$schema": schemaURL}
}

// asMap returns m[key] as a map, creating it if absent or wrong-typed.
func asMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	nm := map[string]any{}
	m[key] = nm
	return nm
}

// MergeOllama deep-merges the ollama provider and the given model entries into
// doc, preserving every other provider and key. Returns the same doc for
// chaining. Calling it twice with the same models is a no-op on the second call.
func MergeOllama(doc Doc, models []ModelEntry) Doc {
	if doc == nil {
		doc = minimalDoc()
	}
	// Ensure schema.
	if _, ok := doc["$schema"].(string); !ok {
		doc["$schema"] = schemaURL
	}

	provider := asMap(doc, "provider")
	ollama := asMap(provider, providerID)

	// Required provider identity. npm + baseURL must be correct; preserve a
	// user-customized display name if one is already set.
	ollama["npm"] = providerNPM
	if _, ok := ollama["name"].(string); !ok {
		ollama["name"] = providerName
	}
	options := asMap(ollama, "options")
	options["baseURL"] = baseURL

	// Merge models map (add/update; never drop existing tags).
	modelsMap := asMap(ollama, "models")
	for _, m := range models {
		if m.Tag == "" {
			continue
		}
		entry := asMap(modelsMap, m.Tag)
		name := m.DisplayName
		if name == "" {
			name = m.Tag
		}
		entry["name"] = name
	}
	return doc
}

// Marshal renders the Doc to canonical, stable bytes (2-space indent, trailing
// newline). Map-keyed JSON is sorted by json.Marshal, ensuring idempotency.
func Marshal(doc Doc) ([]byte, error) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// HasModel reports whether the doc already registers the given tag.
func (d Doc) HasModel(tag string) bool {
	provider, ok := d["provider"].(map[string]any)
	if !ok {
		return false
	}
	ollama, ok := provider[providerID].(map[string]any)
	if !ok {
		return false
	}
	models, ok := ollama["models"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = models[tag]
	return ok
}

// DefaultTarget resolves the opencode.json path to use:
//
//	./opencode.json if it exists, else ~/.config/opencode/opencode.json.
func DefaultTarget(cwd string) (string, error) {
	local := filepath.Join(cwd, "opencode.json")
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "opencode", "opencode.json"), nil
}
