package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultEndpoint is Ollama's native API base URL.
const DefaultEndpoint = "http://localhost:11434"

// Ollama implements Backend over the native :11434 HTTP API.
type Ollama struct {
	Endpoint string
	HTTP     *http.Client
	// DryRun, when true, skips mutating calls (Load/Unload/Pull) and reports success.
	DryRun bool
	// Log, if set, receives a line for each API call (for --verbose).
	Log func(string)
}

// NewOllama builds an Ollama backend. endpoint defaults to DefaultEndpoint.
func NewOllama(endpoint string) *Ollama {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Ollama{
		Endpoint: strings.TrimRight(endpoint, "/"),
		HTTP:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (o *Ollama) logf(format string, a ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(format, a...))
	}
}

func (o *Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// --- Status ---

type versionResp struct {
	Version string `json:"version"`
}

func (o *Ollama) Status(ctx context.Context) DaemonStatus {
	st := DaemonStatus{Endpoint: o.Endpoint}
	o.logf("GET %s/api/version", o.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Endpoint+"/api/version", nil)
	if err != nil {
		st.Err = err.Error()
		return st
	}
	resp, err := o.client().Do(req)
	if err != nil {
		st.Err = "daemon not reachable on " + o.Endpoint
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.Err = fmt.Sprintf("daemon returned %d", resp.StatusCode)
		return st
	}
	var v versionResp
	_ = json.NewDecoder(resp.Body).Decode(&v)
	st.Reachable = true
	st.Version = v.Version
	return st
}

// --- List (/api/tags) ---

func (o *Ollama) List(ctx context.Context) ([]DiskModel, error) {
	o.logf("GET %s/api/tags", o.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseTags(body)
}

// --- Loaded (/api/ps) ---

func (o *Ollama) Loaded(ctx context.Context) ([]LoadedModel, error) {
	o.logf("GET %s/api/ps", o.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Endpoint+"/api/ps", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("loaded models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loaded models: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParsePS(body)
}

// --- Load (/api/generate, empty prompt) ---

type generateReq struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	Stream    bool           `json:"stream"`
	KeepAlive any            `json:"keep_alive,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

func (o *Ollama) Load(ctx context.Context, tag string, keepAlive string) error {
	if o.DryRun {
		o.logf("DRY-RUN load %s keep_alive=%s", tag, keepAlive)
		return nil
	}
	body := generateReq{
		Model:     tag,
		Prompt:    "",
		Stream:    false,
		KeepAlive: parseKeepAlive(keepAlive),
	}
	o.logf("POST %s/api/generate (load %s keep_alive=%v)", o.Endpoint, tag, body.KeepAlive)
	// Loading can take a while for large models.
	return o.postJSON(ctx, "/api/generate", body, 5*time.Minute)
}

// --- Create (derived model with baked parameters) ---

type createReq struct {
	Model      string         `json:"model"`
	From       string         `json:"from"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Stream     bool           `json:"stream"`
}

func (o *Ollama) Create(ctx context.Context, name, from string, params map[string]any) error {
	if o.DryRun {
		o.logf("DRY-RUN create %s from %s params=%v", name, from, params)
		return nil
	}
	o.logf("POST %s/api/create (%s from %s params=%v)", o.Endpoint, name, from, params)
	body := createReq{Model: name, From: from, Parameters: params, Stream: false}
	return o.postJSON(ctx, "/api/create", body, 2*time.Minute)
}

// --- Delete ---

type deleteReq struct {
	Model string `json:"model"`
}

func (o *Ollama) Delete(ctx context.Context, tag string) error {
	if o.DryRun {
		o.logf("DRY-RUN delete %s", tag)
		return nil
	}
	o.logf("DELETE %s/api/delete (%s)", o.Endpoint, tag)
	buf, err := json.Marshal(deleteReq{Model: tag})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodDelete, o.Endpoint+"/api/delete", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete %s: status %d: %s", tag, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// --- Unload (/api/generate, keep_alive 0) ---

func (o *Ollama) Unload(ctx context.Context, tag string) error {
	if o.DryRun {
		o.logf("DRY-RUN unload %s", tag)
		return nil
	}
	body := generateReq{Model: tag, Prompt: "", Stream: false, KeepAlive: 0}
	o.logf("POST %s/api/generate (unload %s keep_alive=0)", o.Endpoint, tag)
	return o.postJSON(ctx, "/api/generate", body, 30*time.Second)
}

// --- Show (/api/show) ---

type showReq struct {
	Model string `json:"model"`
}

func (o *Ollama) Show(ctx context.Context, tag string) (ModelDetails, error) {
	o.logf("POST %s/api/show (%s)", o.Endpoint, tag)
	buf, err := json.Marshal(showReq{Model: tag})
	if err != nil {
		return ModelDetails{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Endpoint+"/api/show", bytes.NewReader(buf))
	if err != nil {
		return ModelDetails{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return ModelDetails{}, fmt.Errorf("show %s: %w", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ModelDetails{}, fmt.Errorf("show %s: status %d", tag, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ModelDetails{}, err
	}
	d, err := ParseShow(raw)
	if err != nil {
		return ModelDetails{}, err
	}
	d.Tag = tag
	return d, nil
}

// --- Pull (/api/pull, streaming) ---

type pullReq struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func (o *Ollama) Pull(ctx context.Context, tag string, progress func(PullProgress)) error {
	if o.DryRun {
		o.logf("DRY-RUN pull %s", tag)
		if progress != nil {
			progress(PullProgress{Status: "dry-run: would pull " + tag})
		}
		return nil
	}
	o.logf("POST %s/api/pull (%s)", o.Endpoint, tag)
	buf, err := json.Marshal(pullReq{Model: tag, Stream: true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Endpoint+"/api/pull", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Pulls are long; use a client without the short default timeout.
	cl := &http.Client{}
	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("pull %s: %w", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull %s: status %d: %s", tag, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var p struct {
			Status    string `json:"status"`
			Completed int64  `json:"completed"`
			Total     int64  `json:"total"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(line, &p); err != nil {
			continue
		}
		if p.Error != "" {
			return fmt.Errorf("pull %s: %s", tag, p.Error)
		}
		if progress != nil {
			progress(PullProgress{Status: p.Status, Completed: p.Completed, Total: p.Total})
		}
	}
	return sc.Err()
}

// postJSON posts a JSON body and discards the response, honoring a timeout.
func (o *Ollama) postJSON(ctx context.Context, path string, body any, timeout time.Duration) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, o.Endpoint+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// parseKeepAlive converts a keep_alive policy string into the JSON value Ollama
// expects: an integer (seconds, or -1 for forever, 0 for immediate) or a
// duration string like "20m".
func parseKeepAlive(s string) any {
	s = strings.TrimSpace(s)
	switch s {
	case "", "0":
		return 0
	case "-1", "forever":
		return -1
	default:
		return s // duration string, e.g. "20m"
	}
}

var _ Backend = (*Ollama)(nil)
