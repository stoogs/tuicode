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
┌─────┬────────────────────┬───────┬─────────────┬─────┬────────┬──────┬──────────────┬────────┐
│ ST  │ MODEL              │ SIZE  │ PARAMS      │ ON  │ CTX    │ GPU  │ PRESET       │ UNTIL  │
├─────┼────────────────────┼───────┼─────────────┼─────┼────────┼──────┼──────────────┼────────┤
│  ●  │ qwen3-coder:30b    │ 9.9GB │ 30B Q4_K_M  │ GPU │ 64k    │ auto │ Coding       │ 15m    │
│  ○  │ llama3.2:1b        │ 1.2GB │ 1B Q8_0     │ —   │ default│ cpu  │ Balanced     │ —      │
└─────┴────────────────────┴───────┴─────────────┴─────┴────────┴──────┴──────────────┴────────┘
RAM   ▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░░░░  8.6 / 62.7 GB
VRAM  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░░░░░  9.9 / 16 GB

SESSION  OpenCode not running   [o] open OpenCode on qwen3-coder:30b

[↑↓] model  [←→/tab] column  [,.] change  [⏎/l] load → open  [esc/u] unload  [q] quit
```

Everything lives on **one page**. Every model on disk is a row: `●` green = running,
`◐` yellow = loading, `●` red = stopping/deleting, `○` grey = stopped. The live
columns (`ON` placement, `UNTIL` auto-unload countdown) update as Ollama
loads/unloads; the editable columns (`CTX`, `GPU`, `PRESET`) are changed inline.
`PARAMS` shows parameter size + quant (e.g. `30B Q4_K_M`).

### Keys

- `↑`/`↓` (or `j`/`k`) — select a model row.
- `←`/`→` (or `Tab`/`Shift-Tab`) — move across the editable columns
  (`CTX`, `GPU`, `PRESET`).
- `,` / `.` — decrease / increase the focused column's value (saved instantly).
  `CTX` steps in 8k; `GPU` sets GPU-offload layers (`auto`/`cpu`/N/`all`).
- `Enter` or `l` — load the model. `Enter` is **progressive**: once the model is
  loaded, press it again to open OpenCode on it.
- `esc` / `u` — unload the selected model.
- `del`/`backspace` — delete the model from disk (asks to confirm; default no).
- `o` — open OpenCode (same as a second `Enter`) · `c` — full configure screen ·
  `d` — cycle device mode (auto/cpu-only/gpu-only) · `p` — pull a model ·
  `s` — settings · `r` — refresh.
- `q` (or `ctrl+c`) — **unload all resident models, prune derived models, then
  quit**. Press again to force-quit immediately.

**Single model at a time.** Loading a model first unloads any other resident
model (you'll see them flash red `●` = stopping, while the new one is yellow `◐` =
loading, with an animated progress bar in the INFO zone). This keeps VRAM
predictable for a one-model-at-a-time coding workflow.

**Memory:** the INFO zone shows a projected memory estimate that updates live as
you change `CTX` (`weights + ctx KV`), with a fit check against the active pool.
Below the table, the **RAM** bar (system memory) sits above the **VRAM** bar so
you can watch both. `d` flips the estimation source between GPU and RAM for
testing.

**Pulling models:** the pull screen (`p`, or first-run onboarding) lists the top
trending, tool-capable models that **fit in your detected memory at Q4_K_M** —
largest-that-fits first. `↑`/`↓` to pick, `Enter` to pull.

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

tuicode needs **OpenCode and Ollama, installed separately** — installing OpenCode
does *not* install a model server. `nvidia-smi` is optional (better VRAM
estimates; absent → clean RAM fallback). tuicode checks all three on every launch.

| | OpenCode | Ollama |
|---|---|---|
| **Arch** | `sudo pacman -S opencode` | `sudo pacman -S ollama-cuda` + `sudo systemctl enable --now ollama` |
| **Fedora** | `curl -fsSL https://opencode.ai/install \| bash` | `curl -fsSL https://ollama.com/install.sh \| sh` |
| **Ubuntu/Debian** | `curl -fsSL https://opencode.ai/install \| bash` | `curl -fsSL https://ollama.com/install.sh \| sh` |

(On Arch, use `ollama-rocm` for AMD or `ollama` for CPU-only.)

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
  sampler preset).
- `~/.config/tuicode/backups/opencode.<timestamp>.json` — backups before every
  `opencode.json` write (last 10 kept).

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
- **OpenCode opened on the wrong model**: tuicode now writes the top-level
  `"model": "ollama/<tag>"` when you press `o`, so OpenCode opens on the model you
  launched. If you still see another, check for a conflicting `model` in a
  higher-priority `opencode.json` (project vs `~/.config/opencode`).
- **Ollama daemon down** (`Ollama ○` in the header): `sudo systemctl start ollama`
  (or `ollama serve`). Check with `systemctl status ollama`.
- **OpenCode can't auth to Ollama**: `opencode auth login` → "Other" → provider id
  `ollama` → any non-empty key (Ollama doesn't validate local keys).
- **Can't reach 64k context / model spilled to CPU**: after loading, the dashboard
  shows the GPU/CPU split. A CPU split means the context was too ambitious — lower
  it in Configure (`c`).
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

Layout: `internal/server` (Ollama backend + parsers), `internal/deps` (prereq +
distro detection), `internal/hw` (GPU/RAM detection + context estimate),
`internal/store` (app + per-model config), `internal/ocfg` (opencode.json
merge/backup/write), `internal/tui` (Bubble Tea screens).
