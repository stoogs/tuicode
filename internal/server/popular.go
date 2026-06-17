package server

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"time"
)

// popularURL is the Ollama library, sorted by popularity. Each model is a
// <li x-test-model> block carrying a <div x-test-model-title title="NAME"> and
// zero or more <span x-test-capability>…</span> markers.
const popularURL = "https://ollama.com/library?sort=popular"

var (
	reTitle      = regexp.MustCompile(`x-test-model-title title="([^"]+)"`)
	reCapability = regexp.MustCompile(`x-test-capability[^>]*>\s*([a-zA-Z]+)`)
)

// FetchPopularNames scrapes the Ollama library for tool-capable model base
// names in popularity order (e.g. "qwen3", "qwen2.5-coder"). Best-effort: it is
// HTML scraping and may return an error if the page layout changes. Callers
// should fall back to their cached/curated list on any error.
func FetchPopularNames(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, popularURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tuicode")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	html := string(body)

	// Walk each model card; keep those advertising the "tools" capability, in
	// page order (already popularity-sorted).
	var names []string
	seen := map[string]bool{}
	idx := reTitle.FindAllStringIndex(html, -1)
	for i, loc := range idx {
		end := len(html)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		block := html[loc[0]:end]
		name := reTitle.FindStringSubmatch(block)
		if name == nil {
			continue
		}
		tools := false
		for _, c := range reCapability.FindAllStringSubmatch(block, -1) {
			if c[1] == "tools" {
				tools = true
				break
			}
		}
		if tools && !seen[name[1]] {
			seen[name[1]] = true
			names = append(names, name[1])
		}
	}
	return names, nil
}
