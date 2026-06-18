# tuicode

A terminal dashboard for running local LLMs with [OpenCode](https://opencode.ai),
backed by [Ollama](https://ollama.com). From one always-on screen you see which
models are loaded and their VRAM use, load/unload/swap models, set context and
GPU offload, write a valid `opencode.json`, and launch OpenCode against the active
model.

Think of it as a **control panel for your local-LLM rig**: the dashboard is home,
models are resources you start and stop (via Ollama), and OpenCode is a session
you launch on top of a loaded model.

```
tuicode                                            device: auto · GPU 16GB · Ollama ●
────────────────────────────────────────────────────────────────────────────────────
┌──┬────────────────────┬───────┬─────────────┬─────┬────────┬──────┬──────────────┐
│ ★│ MODEL              │ SIZE  │ PARAMS      │ ON  │ CTX    │ GPU  │ PRESET       │
├──┼────────────────────┼───────┼─────────────┼─────┼────────┼──────┼──────────────┤
│ ★│ qwen3-coder:30b    │ 9.9GB │ 30B Q4_K_M  │ GPU │ 64k    │ auto │ Coding       │  ← green (loaded)
│  │ llama3.2:1b        │ 1.2GB │ 1B Q8_0     │ —   │ default│ cpu  │ Balanced     │
└──┴────────────────────┴───────┴─────────────┴─────┴────────┴──────┴──────────────┘

INFO  qwen3-coder:30b
  est. mem  14.8GB   weights 8.4 + ctx 64k ≈ 6.0   ✗ needs 16.8, only 14.0 free GPU
  split     25%/75% CPU/GPU · ~20GB  (ref 16GB GPU)
  params     Coding · temp 0.60 · top_p 0.95 · top_k 40

RAM   ▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░░░░  8.6 / 62.7 GB
VRAM  ▓▓▓▓▓▓▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  1.9 / 15.9 GB   ▒ +14.8 to load

[↑↓] model  [←→/tab] column  [,.] change  [⏎/l] load → open  [c] continue  [esc/u] unload
```

Everything lives on **one page**. Every model on disk is a row, and the **row
colour shows its state**: green = loaded, yellow = loading, red =
stopping/deleting, normal = stopped (so there's no separate status column). The
`ON` placement column updates as Ollama loads/unloads; the editable columns
(`CTX`, `GPU`, `PRESET`) are changed inline (the focused cell is a bright silver
cursor). `PARAMS` shows parameter size + quant
(e.g. `30B Q4_K_M`); the `★` column marks your **favourite** (pre-selected on
startup).

The **INFO** zone estimates the model's memory footprint (weights from the actual
file size + KV cache for the chosen context) and checks it against *free* VRAM.
Once a model is **loaded** it shows the **measured** resident size from
`ollama ps` instead of the estimate — ground truth beats a guess. The pre-load
estimate is also **sliding-window aware**: models like Gemma cache only a small
local window on most layers (flagged `(sliding-window)`), so their long-context
KV is a fraction of a naive `ctx × params` guess. Estimates are still rough across
wildly different architectures — treat them as a fit check, not a promise.
Its header also shows the model's **capabilities** (from `ollama show`): `✓ tools`
plus any of `vision`/`audio`/`thinking`. If a model **lacks `tools`** it's flagged
`⚠ no tools — OpenCode needs tool-calling`, since OpenCode won't work without it.
In the memory bars below, the loaded model's own share of RAM/VRAM is drawn in
**green** (memory used by other things stays neutral white), so you can see at a
glance how much the model itself is taking. It also shows a **`split`** line — the
CPU/GPU split as a percentage plus VRAM in use. Before loading it **predicts** the split from the model's layer count
(`ollama show`) and your `GPU` setting — so as you change the `GPU` column with
`,`/`.` you see the likely `~NN%/MM% CPU/GPU · ~X.XGB VRAM (est, G/L layers)`
*before* committing. Once loaded it switches to the *live* placement from
`ollama ps`. (With no layer data it falls back to the benchmark-reference split;
see [recommended.json](#recommendedjson--the-benchmark-reference).) The
**VRAM bar** shows already-used memory as white `▓` blocks and previews the
selected model's footprint as `▒` (red if it would overflow), so you can see
whether it'll fit before loading.

**Managing the CPU/GPU split.** The split is set by how many model layers run on
the GPU — the `GPU` column (`auto`/`cpu`/N layers/`all`), adjusted with `,`/`.`.
Fewer GPU layers ⇒ more on CPU ⇒ lower VRAM but slower; `all` keeps everything on
the GPU (fastest, if it fits). The `split` line tells you the resulting balance
and VRAM cost so you can dial it in to fit.

### Keys

- `↑`/`↓` (or `j`/`k`) — select a model row.
- `←`/`→` (or `Tab`/`Shift-Tab`) — move across the editable columns
  (`CTX`, `GPU`, `PRESET`).
- `,` / `.` — decrease / increase the focused column's value (saved instantly).
  `CTX` steps in 4k (so you can fine-tune KV-cache footprint when VRAM is tight);
  `GPU` sets GPU-offload layers (`auto`/`cpu`/N/`all`), stepped 4 layers at a time.
- `f` — mark the selected model as the **favourite** (the `★`); it's pre-selected
  on startup. Press again to clear.
- `Enter` or `l` — load the model. After it loads you get a *"press Enter to
  continue in OpenCode"* prompt; `Enter` opens it, any other key stays.
  - **Changing a VRAM-affecting setting (`CTX` or `GPU`) on an already-loaded
    model and pressing `Enter` reloads it to apply the change and stops** — it does
    *not* jump straight into OpenCode. The INFO `split` line then shows the new
    live CPU/GPU split + VRAM, so you can confirm it fits; press `Enter` again to
    open OpenCode. (Sampler `PRESET` changes don't touch VRAM, so they never force
    a reload.)
- `c` — **continue** the selected model's last OpenCode session (`opencode -s
  <id>`). tuicode records the session id each time OpenCode exits and shows it in
  the **SESSION** zone; `c` loads the model first if it isn't resident.
- `C` — **continue the most recent session across all models** (the `global`
  line in the SESSION zone). Loads that session's model first if needed.
- `esc` / `u` — unload the selected model.
- `del`/`backspace` — delete the model from disk (asks to confirm; default no).
- `o` — **`ollama pull`** a model (trending list) · `p` — model **preferences**
  (the full configure screen) · `d` — cycle device mode (auto/cpu-only/gpu-only)
  · `s` — settings · `r` — refresh.

  (A fresh session opens with `Enter`/`l` on a loaded model — there's no separate
  "open" key.)
- `q` (or `ctrl+c`) — **unload all resident models, prune derived models, then
  quit**. Press again to force-quit immediately.

**Single model at a time.** Loading a model first unloads any other resident
model (the outgoing row turns red while stopping, the incoming row turns yellow
and the INFO zone shows a calm `LOADING…`). This keeps VRAM predictable for a
one-model-at-a-time coding workflow.

**Memory:** the INFO zone shows a projected memory estimate that updates live as
you change `CTX` (`weights + ctx KV`), with a fit check against the active pool.
Below the table, the **RAM** bar (system memory) sits above the **VRAM** bar so
you can watch both. `d` flips the estimation source between GPU and RAM for
testing.

**Pulling models:** the pull screen (`o` = `ollama pull`, or first-run onboarding)
has two lists, switched with `←`/`→`:

- **Trending** — popular, tool-capable models that **run on your machine at
  Q4_K_M**, largest first. Fit is measured against **system RAM**, not just VRAM:
  models bigger than VRAM still appear (they run on a CPU/GPU split) and are marked
  `CPU split (spills past VRAM)`. This list is refreshed from Ollama's popularity
  ranking once a day (best-effort; if the fetch fails it keeps the cached list).
- **Recommended** — curated from a benchmark table (footprint + CPU/GPU split),
  fastest-likely first. Sourced from
  [recommended.json](#recommendedjson--the-benchmark-reference), which you can
  edit.
- **Manual add** — a text field: type or paste any Ollama model tag
  (e.g. `qwen3.6:35b-a3b`) and press `Enter` to pull it directly, even if it
  isn't in either list.

`↑`/`↓` to pick, `Enter` to pull. The download bar runs red→green as it fills.

## The two-layer model (read this)

tuicode manages **two independent layers**:

- **Model** = weights loaded in VRAM/RAM. Controlled by the **Ollama daemon**.
  Survives OpenCode quitting.
- **Session** = an OpenCode instance talking to Ollama. It *is* the OpenCode
  process; it never owns the model.

**Quitting OpenCode frees no VRAM** — the model stays loaded in Ollama until its
keep-alive expires or you unload it. That's why "unload" lives in tuicode (it
targets Ollama) and quitting OpenCode doesn't free VRAM.

## Prerequisites

tuicode runs on **Linux** and **macOS** (Apple Silicon and Intel). It needs
**OpenCode and Ollama, installed separately** — installing OpenCode does *not*
install a model server. tuicode checks both on every launch and tailors its
install/start hints to your OS.

| | OpenCode | Ollama |
|---|---|---|
| **macOS** | `brew install sst/tap/opencode` | `brew install ollama` + `brew services start ollama` |
| **Arch** | `sudo pacman -S opencode` | `sudo pacman -S ollama-cuda` + `sudo systemctl enable --now ollama` |
| **Fedora** | `curl -fsSL https://opencode.ai/install \| bash` | `curl -fsSL https://ollama.com/install.sh \| sh` |
| **Ubuntu/Debian** | `curl -fsSL https://opencode.ai/install \| bash` | `curl -fsSL https://ollama.com/install.sh \| sh` |

(On Arch, use `ollama-rocm` for AMD or `ollama` for CPU-only.)

**Memory detection by platform.** On Linux, VRAM is read from `nvidia-smi`
(optional — absent → clean RAM fallback) and RAM from `/proc/meminfo`. On Apple
Silicon, the GPU uses **unified memory** (one pool shared with the CPU), detected
via `sysctl`/`vm_stat` and shown as a single `Mem` bar rather than separate
RAM/VRAM bars. Fit estimates keep **~30% of unified memory free** for the OS,
browser, and CPU-side work — which also tracks Metal's default ~70% GPU wired
limit. Intel Macs fall back to RAM-based estimation (Ollama runs on CPU).

**No CPU/GPU split on Apple Silicon.** A CPU/GPU split is a discrete-GPU concept:
overflow layers spill into *separate* system RAM when a model is bigger than VRAM.
Unified memory is one pool, so moving a layer to the CPU saves no memory — Ollama
runs the whole model on the GPU (Metal). The only ceiling is total memory and
Metal's ~70% wired limit; past it, layers fall back to the CPU (slower, same
memory) unless you raise `iogpu.wired_limit_mb`. So on M-series tuicode shows GPU
placement (`place`), not a split; lowering the dashboard's `GPU` offload column
only forces work onto the CPU (slower, with no memory saved).

## Install tuicode

```sh
# from a clone
./install.sh                      # builds to ~/.local/bin/tuicode

# or with Go directly
go install .                      # needs Go 1.23+
```

## Quick start

```
tuicode
```

1. Startup check confirms OpenCode + Ollama (+ optional GPU).
2. No models yet? Onboarding offers to `ollama pull llama3.2:1b` (a ~1.3GB
   tool-capable starter — OpenCode needs tool support, so ultra-tiny models like
   `gemma3:270m` won't work with it).
3. Select a model row (`↑`/`↓`) and press `Enter` → it loads into Ollama. Tune
   `CTX`/`GPU`/`PRESET` inline first with `←`/`→` to pick a column and `,`/`.`
   to change it.
4. Once it's loaded, press `Enter` again (or `o`) → tuicode writes `opencode.json`,
   suspends, and runs OpenCode against that model in the same terminal.
5. Quit OpenCode → tuicode returns and shows the model still loaded (`UNTIL`
   column counts down to auto-unload). Press `u`/`esc` to free VRAM now.
6. Quit tuicode with `q` → it unloads everything before exiting.

## Models & storage

Download models with `ollama pull <name>` — **Ollama manages storage, you don't
place files.** tuicode discovers models via `ollama list`, so it works regardless
of where Ollama keeps them.

Want models on another drive? Set `OLLAMA_MODELS` via a one-time
[systemd override](https://github.com/ollama/ollama/blob/main/docs/faq.md#how-do-i-set-the-models-directory)
on the ollama service. tuicode **shows** the active path in Settings but does not
edit system files.

## Model lifecycle

Loaded models **auto-unload after ~20 min idle** (Ollama's keep-alive), and the
`UNTIL` column counts that down. Unload now with `u`/`esc`, and **quitting tuicode
(`q`) unloads everything first** — so you don't leave models holding VRAM after
you're done. Only one model is resident at a time: loading a new one frees the
previous.

(An earlier per-model "residency" control was removed — OpenCode's `/v1` requests
reset keep-alive to the daemon default anyway, so it added config without
reliable effect. Auto-unload-after-idle + unload-on-quit covers the real need.)

## Device modes

```
tuicode --cpu-only      # laptops / no GPU: RAM-based estimates
tuicode --gpu-only      # force GPU as the estimation source
```

Device mode is sticky once set and shown in the header.

## CLI flags

```
tuicode                          launch dashboard (auto device mode)
tuicode --cpu-only | --gpu-only  force estimation source
tuicode --opencode-json <path>   target a specific opencode.json
tuicode --config-dir <path>      override ~/.config/tuicode (testing)
tuicode --dry-run                show writes/loads without performing them
tuicode --verbose                log detection/API/CLI calls to stderr
tuicode --version
```

Develop without touching real config:
`tuicode --config-dir /tmp/tuicode-test --opencode-json /tmp/oc.json --dry-run`.

## File locations

- `~/.config/tuicode/config.json` — app config (device mode, default residency).
- `~/.config/tuicode/models/<alias>.json` — per-model config (context, residency,
  sampler preset, last OpenCode session).
- `~/.config/tuicode/recommended.json` — **the benchmark reference** (editable;
  see below). Seeded once from a built-in default, then never overwritten.
- `~/.config/tuicode/trending.json` — the trending pull list. A **cache**: it's
  reordered by the daily popularity refresh, so don't hand-edit it (edits won't
  survive). To curate the pull screen, use `recommended.json`.
- `~/.config/tuicode/backups/opencode.<timestamp>.json` — backups before every
  `opencode.json` write (last 10 kept).

### recommended.json — the benchmark reference

A small JSON file you can edit to drive the **Recommended** pull tab and the
dashboard's `split` reference line:

```json
{
  "ref_gpu_gb": 16,
  "models": [
    {"tag": "qwen3-coder:30b", "mem_gb": 20, "gpu_percent": 75, "note": "coding · agentic"},
    {"tag": "gpt-oss:20b",     "mem_gb": 14, "gpu_percent": 100}
  ]
}
```

- `mem_gb` — total RAM+VRAM the model uses.
- `gpu_percent` — share of the model that sits on the GPU (`100` = fully on GPU;
  lower means it spills to CPU). tuicode renders this as e.g. `25%/75% CPU/GPU`.
- `ref_gpu_gb` — the GPU the figures were measured on (shown as `ref 16GB GPU`),
  so you know the numbers are a guide, not a promise for your exact hardware.

The shipped defaults come from
[glukhov.org's 16GB-VRAM benchmarks](https://www.glukhov.org/llm-performance/benchmarks/choosing-best-llm-for-ollama-on-16gb-vram-gpu/).
Update the file with your own measurements any time.

## opencode.json

tuicode **merges, never clobbers**: it deep-merges only `provider.ollama` and its
`models` map, preserving every other provider and key. Writes are idempotent
(same inputs → byte-identical) and backed up first.

### How context & GPU settings actually reach OpenCode

Ollama's OpenAI-compatible `/v1` endpoint (which OpenCode uses) **ignores
per-request `num_ctx`/`num_gpu` and resets them to the daemon defaults** — so
simply warm-loading a model at 64k context doesn't stick: OpenCode's first
request reloads it at the default (often 4096), and large prompts overflow.

To pin them reliably, when a model has a non-default `CTX` or `GPU` setting,
tuicode creates a lightweight **derived model** (`tuicode/<base>:c<ctx>g<gpu>`)
with `num_ctx`/`num_gpu` baked into its Modelfile, loads *that*, and points
OpenCode at it. Derived models share the base's blobs (cheap) and are hidden from
the table. They're created on demand and reused. Unused ones are pruned
automatically on quit (`q`), or on demand via **Settings → Prune derived**.

- Setting `GPU` to `cpu` (or running `--cpu-only`) bakes `num_gpu: 0`, so the
  model genuinely runs on the CPU even under OpenCode.
- Changing `CTX`/`GPU` after a model is loaded makes `Enter`/`o` **reload** it
  with the new settings before launching OpenCode.

> Keep-alive can't be pinned for OpenCode either — its `/v1` requests reset it to
> the daemon default. tuicode loads models with a ~20 min idle keep-alive for the
> dashboard; once OpenCode is driving the model, Ollama's default applies. (This is
> why per-model residency config was dropped — see *Model lifecycle*.)

> Bigger context VRAM savings come from **flash attention** + **KV-cache
> quantization** (`OLLAMA_FLASH_ATTENTION=1`, `OLLAMA_KV_CACHE_TYPE=q8_0`). These
> are daemon-level env vars (set via a systemd override); Settings shows their
> status and the exact commands.

## Troubleshooting

- **`<model> does not support tools`** (from OpenCode): the model has no
  tool-calling template. OpenCode requires tools, so pick a tool-capable model
  (`llama3.2:1b/3b`, `qwen2.5:*`, `qwen3-coder:*`, …). Ultra-tiny models like
  `gemma3:270m` won't work with OpenCode.
- **OpenCode opened on the wrong model**: tuicode writes the top-level
  `"model": "ollama/<tag>"` when you open OpenCode (`Enter`/`l`), so it opens on the
  model you launched. If you still see another, check for a conflicting `model` in a
  higher-priority `opencode.json` (project vs `~/.config/opencode`).
- **Ollama daemon down** (`Ollama ○` in the header): `sudo systemctl start ollama`
  (or `ollama serve`). Check with `systemctl status ollama`.
- **OpenCode can't auth to Ollama**: `opencode auth login` → "Other" → provider id
  `ollama` → any non-empty key (Ollama doesn't validate local keys).
- **Can't reach 64k context / model spilled to CPU**: after loading, the INFO
  `split` line shows the live GPU/CPU split + VRAM. A CPU-heavy split means the
  model+context was too big for VRAM — lower `CTX`, or raise GPU layers / lower the
  model size. Preferences (`p`) has the full controls.
- **VRAM full after switching models**: the old model is still loaded (OpenCode
  doesn't free it). Unload it (`u`) or wait for auto-unload.

## Backend

The model backend lives behind a `Backend` interface (`internal/server`). The MVP
is **Ollama-only** by deliberate choice — a pure CLI + daemon, the right fit for a
headless terminal tool. A second backend (e.g. LM Studio) can be added without UI
rework.

## Development

```sh
go build ./...
go test ./...
go vet ./...
```

Layout: `internal/server` (Ollama backend + parsers, incl. the popularity
scraper), `internal/deps` (prereq + distro detection), `internal/hw` (GPU/RAM
detection + context estimate), `internal/store` (app + per-model config, plus the
embedded `data/{trending,recommended}.json` seeds), `internal/ocfg` (opencode.json
merge/backup/write), `internal/tui` (Bubble Tea screens).
