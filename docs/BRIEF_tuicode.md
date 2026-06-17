# tuicode — TUI Dashboard for OpenCode + Ollama

**Version:** 0.1.0
**Status:** MVP Brief
**Language:** Go
**Target platform:** Linux (Arch-first; Fedora/Ubuntu supported)
**Backend (MVP):** Ollama only

---

## 1. Summary

`tuicode` is a terminal dashboard for running local LLMs with OpenCode, backed by Ollama. From one always-on screen you see which models are loaded and their VRAM use, load/unload/swap models, set context and residency, write a valid `opencode.json`, and launch OpenCode against the active model.

Think of it as a **control panel for your local-LLM rig**: the dashboard is home, models are resources you start and stop (via Ollama), and OpenCode is a session you launch on top of a loaded model.

**MVP is Ollama-only by deliberate choice.** Ollama is a pure CLI + daemon — no GUI dependency — which is exactly right for a headless terminal tool. LM Studio is GUI-first (its `lms` CLI needs the desktop app/daemon running), heavier, and a worse fit; it is explicitly out of scope for v0.1. The `server` package is built behind a `Backend` interface so a second backend can be added later without reworking the UI.

---

## 2. The architecture that matters (read this first)

A misconception worth killing up front: **OpenCode does not load or run models.** OpenCode is a *client* that sends API calls to a local OpenAI-compatible endpoint. The thing that holds a model in VRAM is the **Ollama daemon**.

So tuicode manages **two independent layers**:

| Layer | "Running" means | Controlled via | Survives OpenCode quitting? |
|---|---|---|---|
| **Model** | weights loaded in VRAM/RAM | Ollama (`ollama` CLI / API) | Yes — the daemon keeps it loaded |
| **Session** | an OpenCode instance talking to Ollama | spawning/exiting an OpenCode process | n/a — it *is* OpenCode |

Consequences:
- **Quitting OpenCode frees no VRAM.** The model stays loaded in Ollama. Run a model → quit OpenCode → start another, and the first is *still loaded* until Ollama's keep-alive expires or tuicode unloads it. OpenCode never owned the model and won't clean it up.
- **The dashboard's real power is the model layer** — load/unload/swap operate on Ollama, not OpenCode.
- **The session layer is just "is OpenCode open against this model."** Launching it is a convenience; the model can serve many sessions or none.

tuicode's "stop" therefore means **unload the model from Ollama** (frees VRAM). Closing an OpenCode session is separate and lighter.

### Ollama primitives tuicode drives (verified)
- `ollama list` — models on disk, with tags + sizes (this is discovery; never scan the filesystem — models are content-addressed blobs)
- `ollama ps` — currently loaded models: name, size, processor split (GPU/CPU %), context, time-until-unload
- `ollama run <tag>` — loads + serves (auto-pulls if missing)
- `ollama stop <tag>` — force-unload now
- `ollama pull <tag>` — download a model (used by onboarding)
- `ollama show <tag>` — model details
- Residency via `keep_alive` (per request / API) or `OLLAMA_KEEP_ALIVE` (daemon-wide): duration string (`"20m"`), `-1` keep forever, `0` unload immediately. Default is 5m.
- Endpoint: `http://localhost:11434/v1` (OpenAI-compatible path) and native API on `:11434`.

For load + residency control, prefer the **HTTP API** over shelling `ollama run` (which is interactive): POST `/api/generate` with `{"model": "<tag>", "keep_alive": "<policy>"}` and an empty prompt to warm-load with a chosen residency; POST with `keep_alive: 0` to unload. `ollama ps` / `ollama list` can stay as CLI calls, or use `/api/ps` and `/api/tags`. Decide one transport and be consistent; API is cleaner for programmatic control.

---

## 3. opencode.json facts (verified)

- Top-level `"$schema": "https://opencode.ai/config.json"`. Providers under `provider.<id>` with `npm: "@ai-sdk/openai-compatible"`, `name`, `options.baseURL`, and a `models` map keyed by the **Ollama tag**, value `{ "name": "Display Name" }`.
- Ollama block → baseURL `http://localhost:11434/v1`.
- OpenCode + local models wants **≥64k context** for reliable tool-calling; aim there.
- Some local providers need a dummy credential: `opencode auth login` → "Other" → provider id `ollama` → any non-empty key. Ollama doesn't validate local keys. Document in troubleshooting.

Output shape:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (local)",
      "options": { "baseURL": "http://localhost:11434/v1" },
      "models": { "qwen3-coder:30b": { "name": "Qwen3 Coder 30B" } }
    }
  }
}
```

---

## 2a. Prerequisites & startup dependency check

**Nothing is assumed installed.** Installing OpenCode pulls in *no* model server. The user needs **OpenCode + Ollama**, installed separately.

Required vs optional:
- **OpenCode** — required (the thing tuicode launches).
- **Ollama** — required (the model backend), with its daemon running.
- **`nvidia-smi`** — optional (better VRAM estimates; absence → RAM fallback).

**On every launch, before the dashboard, run a dependency check:**
- `opencode` on PATH? (capture version)
- `ollama` on PATH? and daemon reachable on `:11434`? (`/api/ps` or `ollama ps` succeeds)
- `nvidia-smi` present?

**Behaviour:**
- **All good:** proceed; report findings briefly in the header, e.g. `OpenCode 1.17 · Ollama :11434 ● · GPU 16GB`.
- **Ollama installed but daemon down:** don't block — show "Ollama installed, daemon not running" with the fix (`sudo systemctl enable --now ollama`, or `ollama serve`) and re-check on the next poll.
- **Missing a hard requirement** (no OpenCode, or no Ollama): show a clean, distro-aware prerequisites screen with copy-pasteable install commands (detect distro via `/etc/os-release`), then quit. Don't limp on and fail later.

**Missing-prerequisites screen (example — Arch detected):**
```
tuicode can't start — missing prerequisites

  ✓ OpenCode   found (1.17.3)
  ✗ Ollama     not found

tuicode needs OpenCode and Ollama. Install Ollama:

  Arch Linux:
    sudo pacman -S ollama-cuda          # NVIDIA GPU
    sudo systemctl enable --now ollama
    ollama pull gemma3:270m             # tiny starter model

Re-run tuicode once Ollama is installed and running.
```

Detect distro from `/etc/os-release` (`ID` / `ID_LIKE`); show the matching block. Default to Arch when ambiguous (primary target).

**Install matrix (prereq screen + README):**

| | OpenCode | Ollama |
|---|---|---|
| **Arch** | `sudo pacman -S opencode` (or `paru -S opencode-bin` for latest) | `sudo pacman -S ollama-cuda` (NVIDIA) / `ollama-rocm` (AMD) / `ollama` (CPU), then `sudo systemctl enable --now ollama` |
| **Fedora** | `curl -fsSL https://opencode.ai/install \| bash` (or `npm i -g opencode-ai`) | `curl -fsSL https://ollama.com/install.sh \| sh` |
| **Ubuntu/Debian** | `curl -fsSL https://opencode.ai/install \| bash` (or `npm i -g opencode-ai`) | `curl -fsSL https://ollama.com/install.sh \| sh` |

(OpenCode is in Arch `extra`. Ollama's official script covers Fedora/Ubuntu; Arch has native `ollama` / `ollama-cuda` / `ollama-rocm`.)

---

## 4. Scope

### In scope (MVP)
1. Startup dependency check (OpenCode + Ollama + nvidia-smi); guide/quit if essentials missing (§2a)
2. **Dashboard home** — live loaded models, VRAM, residency countdown, session + daemon status
3. First-run onboarding (detect models; if none, offer `ollama pull` of a small starter)
4. Model discovery via `ollama list`
5. Live model state via `ollama ps` / `/api/ps` (polled)
6. **Model lifecycle**: load, unload, swap — via Ollama
7. **Residency policy**: auto-unload-after-idle (default) / keep-warm / manual, via `keep_alive`
8. Context/parameter config per model
9. `opencode.json` read / merge / write with backups
10. **Launch OpenCode** against the active model (suspend-and-return; §7)
11. Device mode (auto / CPU-only / GPU-only) for estimation
12. Config cleanup; single install script + README

### Out of scope (future)
- **LM Studio backend** (behind the same `Backend` interface)
- In-app model **deletion** (`ollama rm`) and **download UI** beyond the onboarding pull
- Custom `OLLAMA_MODELS` management (tuicode can *show* the configured path; changing it is a systemd-override task — link, don't automate)
- AMD GPU detection (`rocm-smi`); multi-GPU aggregation
- Remote/cloud Ollama hosts; multiple concurrent OpenCode sessions in-app
- Benchmarking, preset export/sharing, Modelfile editing

---

## 5. Where models live (and why tuicode doesn't manage paths)

Users **download models with `ollama pull`**, and Ollama stores them itself — the user never hand-places files. The storage path depends on install method:
- **Arch / systemd service:** Ollama runs as the `ollama` system user → models in `/usr/share/ollama/.ollama/models`.
- **User-run / `ollama serve` as your user:** `~/.ollama/models`.
- **Custom:** wherever `OLLAMA_MODELS` points (set via systemd override).

Because tuicode discovers models through `ollama list` (not the filesystem), **it doesn't need to know or hardcode the path** — this Just Works regardless of install method. 

**Onboarding guidance to the user:** "download models with `ollama pull <name>` — Ollama manages storage for you." If they want models on a different drive, tuicode's settings screen *shows* the active `OLLAMA_MODELS` location (read from the daemon/env) and links to the one-time systemd-override steps; it does not edit system files itself. A **custom directory** for the user therefore means "set `OLLAMA_MODELS` to your big drive," surfaced as guidance, not a managed feature.

The onboarding "no models" path offers to run `ollama pull gemma3:270m` (a ~290MB starter) directly, so a fresh user gets a working model without leaving the TUI.

---

## 6. Dashboard home (primary screen)

Opens after the prereq check. The hub everything returns to.

```
┌─ tuicode ───────────────────────────────── device: auto · GPU 16GB · Ollama ● ─┐
│                                                                                 │
│  LOADED MODELS                                              [l] load  [u] unload│
│  ───────────────────────────────────────────────────────────────────────────  │
│  ● qwen3-coder:30b     9.9GB   ctx 65536   GPU 100%   idle 4m · unloads 16m     │
│  ○ (none other loaded)                                                          │
│                                                                                 │
│  VRAM  ▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░  9.9 / 16 GB                                          │
│                                                                                 │
│  SESSION                                                                        │
│  ───────────────────────────────────────────────────────────────────────────  │
│  OpenCode: not running              [o] open OpenCode on qwen3-coder:30b        │
│                                                                                 │
│  [m] models   [c] configure   [j] opencode.json   [s] settings   [q] quit       │
└─────────────────────────────────────────────────────────────────────────────────┘
```

- **Live polling** every ~2s: refresh `ollama ps` / `/api/ps`; update loaded list, VRAM bar, residency countdowns without user action.
- **VRAM bar** from `nvidia-smi` (or summed footprints if smi unavailable).
- **Status dots:** ● loaded/up, ○ free/down. Daemon status in header.
- Single-key actions: load `l`, unload `u`, open OpenCode `o`, models `m`, configure `c`, opencode.json `j`, settings `s`.
- If the daemon is down, grey the loaded panel and show `[sudo systemctl start ollama]`.

---

## 6a. Model residency / keep-alive policy

Ollama's default is **auto-unload after 5 minutes idle**. tuicode lets the user pick a policy explicitly and enforces it at load time via `keep_alive`, so behaviour is predictable.

**Three modes** (per-model, with a global default in settings):
1. **Auto-unload after idle** *(default — N minutes, default 20)* — stays warm while you work, frees VRAM on its own after inactivity. The "walk away and forget" case.
2. **Keep warm** — resident indefinitely (`keep_alive: -1`) for instant relaunch; shows a persistent "held" indicator so it isn't forgotten.
3. **Manual only** — long keep-alive; user unloads explicitly.

**Implementation:** set `keep_alive` on the warm-load request (`"20m"` / `-1`). Manual unload (`u`) posts `keep_alive: 0` (or `ollama stop <tag>`) and is always available regardless of mode — the "free it now" button. `ollama ps` reports time-until-unload; surface it live (`idle 4m · unloads 16m`); show `held` for keep-warm. When auto-unload fires, the row drops from LOADED and the VRAM bar falls on the next poll — no user action.

So: **the model does not require a manual stop by default** — it auto-unloads after idle. Keep-warm holds it; manual unload kills it instantly.

---

## 7. OpenCode session (launch behaviour)

**Decision: suspend-and-return in the current terminal.** When the user opens OpenCode, tuicode suspends its TUI, runs `opencode` as a foreground child in the *same* terminal, and redraws the dashboard when OpenCode exits.

Why: no terminal-emulator detection, no `$TERMINAL` guesswork, no non-portable new-tab APIs, no detached/double-forked PID tracking. The child is a normal foreground process; exit is detected cleanly. Mirrors how TUI tools hand off to `$EDITOR`.

Trade-off: the dashboard isn't visible *while* OpenCode is open — acceptable, since the model keeps running in Ollama and the dashboard's live state is still true on return. (Optional new-window mode is future scope.)

**Flow:**
1. From home, `o` opens OpenCode against the active/selected loaded model.
2. tuicode ensures opencode.json points at that model (merge/write if needed, §9), in the chosen working dir.
3. Suspend TUI → exec `opencode` in CWD → user works → quits OpenCode.
4. tuicode resumes, repolls Ollama, shows: "Session closed. qwen3-coder:30b still loaded (unloads in 18m)." Model stays per its residency policy.

If no model is loaded when `o` is pressed, prompt to load one first (or auto-load the configured default, then launch).

---

## 8. Discovery, detection, estimation

### 8.1 Model discovery
- **`ollama list`** (or `/api/tags`) is the single source of truth. Parse tag + size. **Never scan the filesystem** — Ollama models are content-addressed blobs.
- Refresh on `r` and after a `pull`.
- Tags already carry useful info (`qwen3-coder:30b`); param size often in the tag/size. Use `ollama show <tag>` for details (context length, quant) when needed.

### 8.2 Hardware detection & context estimate
Ollama has no pre-load memory estimator, so tuicode estimates:
- **GPU:** `nvidia-smi --query-gpu=memory.total,memory.free --format=csv,noheader,nounits` (MiB). Missing/error → RAM fallback, silent.
- **RAM fallback:** `MemAvailable` from `/proc/meminfo`.
- **Device mode** (§10) picks the authoritative source.
- **Footprint:** params × bytes/param by quant (rough, generous): Q4 ≈ 0.55, Q5 ≈ 0.65, Q6 ≈ 0.8, Q8 ≈ 1.0 GB per billion params. Pull params/quant from the tag or `ollama show`.
- **Context:** `budget = (mem - footprint - reserve) * 0.9`; `gb_per_1k ≈ params_billions * 0.0021` (calibrate — see gotchas); `max_tokens = budget/gb_per_1k * 1000`, rounded to 8k/16k/32k/64k/128k. `reserve` = 2GB (GPU) / 4GB (RAM).
- **Aim ≥64k; warn below ~32k** (tool-calling gets flaky). Cap at the model's known max from `ollama show`.
- **Calibration:** after a load, read `ollama ps` (it reports the loaded context + GPU/CPU split). If the model spilled to CPU, the context was too ambitious — surface that and suggest lowering. This real feedback loop substitutes for a pre-load estimator.

---

## 9. opencode.json integration
- **Target:** prefer `./opencode.json` if present, else `~/.config/opencode/opencode.json`. Override via menu or `--opencode-json <path>`.
- **Merge, never clobber.** Deep-merge only `provider.ollama` and its `models` map; preserve all other providers and keys. Writing twice with the same inputs → byte-identical (idempotent).
- Model key = Ollama tag (`qwen3-coder:30b`).
- **Backup before every write:** `~/.config/tuicode/backups/opencode.<timestamp>.json`, keep last 10. Create a minimal valid file if none exists.

> Honest caveat: OpenCode's per-model param surface is limited; real `num_ctx` and residency are governed by **Ollama at load time** (`keep_alive`, the model's loaded context), not opencode.json. tuicode applies context/residency when loading the model and writes opencode.json mainly for provider/model registration. Don't imply opencode.json governs inference params it doesn't.

---

## 10. Config, presets, device mode

**Per-model config** `~/.config/tuicode/models/<alias>.json`:
```json
{
  "alias": "qwen3-coder-30b",
  "display_name": "Qwen3 Coder 30B",
  "model_tag": "qwen3-coder:30b",
  "params_billions": 30,
  "quant": "Q4_K_M",
  "context_length": 65536,
  "residency": { "mode": "auto_unload", "idle_minutes": 20 },
  "parameters": { "temperature": 0.6, "top_p": 0.95, "top_k": 40 }
}
```
- `context_length` + `residency` are applied at **load time** via Ollama.
- `residency.mode`: `auto_unload` (with `idle_minutes`) | `keep_warm` | `manual`.

**Presets** (temperature / top_p / top_k): Coding 0.6/0.95/40 (default), Balanced 0.7/0.9/40, Creative 0.9/0.98/50, Deterministic 0.2/0.9/20. Custom edits all of the above plus context + residency.

**Device mode:** `--cpu-only` / `--gpu-only`, default auto (sticky once used). Affects which memory source feeds §8.2 and the reserve value. Shown in the header.

---

## 11. CLI flags
```
tuicode                          launch dashboard (auto device mode)
tuicode --cpu-only | --gpu-only  force estimation source
tuicode --opencode-json <path>   target a specific opencode.json
tuicode --config-dir <path>      override ~/.config/tuicode (testing)
tuicode --dry-run                show writes/loads without performing them
tuicode --verbose                log detection/API/CLI calls to stderr
tuicode --version
```
Develop without touching real config via `--config-dir /tmp/tuicode-test --opencode-json /tmp/oc.json`.

---

## 12. Project layout
```
tuicode/
├── main.go
├── internal/
│   ├── tui/
│   │   ├── app.go          root model, routing, header
│   │   ├── prereq.go       missing-prerequisites screen
│   │   ├── dashboard.go    home: loaded models, VRAM, session, daemon
│   │   ├── onboarding.go   first-run (+ optional ollama pull)
│   │   ├── models.go       models list, load/unload/residency actions
│   │   ├── configure.go    presets/custom params, context, residency
│   │   └── settings.go     device mode, opencode.json target, OLLAMA_MODELS info
│   ├── server/             backend layer (interface + ollama impl)
│   │   ├── backend.go      Backend interface (List/Loaded/Load/Unload/Pull/Show)
│   │   ├── ollama.go       Ollama impl over the :11434 API (+ ollama CLI where simpler)
│   │   └── types.go        LoadedModel, DiskModel, DaemonStatus
│   ├── deps/
│   │   └── check.go        detect opencode/ollama/nvidia-smi + distro
│   ├── hw/
│   │   └── detect.go       nvidia-smi + /proc/meminfo + estimate
│   ├── ocfg/
│   │   ├── read.go  merge.go  write.go     opencode.json (idempotent + backup)
│   └── store/
│       ├── config.go  models.go            app + per-model config
├── install.sh
├── go.mod
└── README.md
```
**Deps:** `bubbletea`, `bubbles`, `lipgloss`. Rest stdlib (`net/http`, `encoding/json`, `os/exec`, `bufio`, `syscall`).

---

## 13. Gotchas (read before coding)

1. **Two layers, not one.** Unloading a model ≠ closing OpenCode. The dashboard's stop/unload targets Ollama. Don't conflate them.
2. **OpenCode leaves models loaded.** After a session quits, the model is still resident until keep-alive expires. Reflect this truthfully and make unload easy.
3. **Ollama has no scannable gguf files.** `ollama list` / `/api/tags` only — never walk `~/.ollama/models`.
4. **Model storage path varies by install** (`/usr/share/ollama/.ollama/models` for the systemd service vs `~/.ollama/models` user-run). Don't hardcode it — discovery is via the API regardless. Only *show* `OLLAMA_MODELS` in settings.
5. **`ollama run` is interactive.** For programmatic load, use the HTTP API (`/api/generate` with empty prompt + `keep_alive`), not `ollama run`. Use `ollama stop` / `keep_alive: 0` to unload.
6. **No pre-load memory estimator.** Calibrate the §8.2 heuristic and use the post-load `ollama ps` GPU/CPU split as the real feedback signal (CPU spill = context too high).
7. **opencode.json merge is the highest-risk write.** Back up first; test against a multi-provider config and assert other providers survive; assert idempotency.
8. **Suspend-and-return correctly.** Leave the alt-screen / restore cooked mode before exec'ing OpenCode, restore on return. Use Bubble Tea's process-suspension support so the screen isn't corrupted.
9. **No GPU is normal** (laptop). `nvidia-smi` absent → clean RAM fallback, not an error dump.
10. **Daemon down ≠ not installed.** `ollama` on PATH but `/api/ps` failing means the service isn't running — show the start command, don't treat it as missing.
11. **Polling cost:** hitting `/api/ps` every 2s is fine; debounce and skip a tick rather than stacking calls if one is slow.
12. **Port:** Ollama `11434`.

---

## 14. Build order

1. **`server` (Backend interface + Ollama impl) + `deps` check** — wrap the Ollama API/CLI: list, loaded, load-with-keep_alive, unload, pull, show; detect opencode/ollama/nvidia-smi + distro. This is the engine. Unit-test parsers/detection against fixture output.
2. **`hw`** — nvidia-smi + /proc/meminfo + context estimate. Headless, fixture-tested.
3. **`store` + `ocfg`** — per-model config (incl. residency) + opencode.json merge/backup/write. Hammer idempotency + multi-provider preservation.
4. **`tui` prereq + dashboard + models + configure** — wire verified logic into the live screen; polling loop; load/unload/residency actions.
5. **session launch (suspend-and-return)** — exec OpenCode, restore TUI on exit.
6. **onboarding (+ pull), install.sh, README** — last.

Steps 1–3 are pure logic, cheap to test. Step 4 is the bulk of wall-clock time. Step 5 is small but finicky around terminal state.

---

## 15. Testing

**Unit (fixtures):**
- Parse `ollama list` / `/api/tags` (normal, empty) and `ollama ps` / `/api/ps` (loaded, empty, daemon-down).
- `deps` detection: each of opencode/ollama/nvidia-smi present/absent; distro from sample `/etc/os-release` files.
- Context estimate: monotonic, respects model max, right memory source per device mode.
- opencode.json merge: empty→add, existing-provider→add-model, second provider preserved, run-twice identical; backup made; retention prunes to 10.

**Integration (`--config-dir /tmp/... --opencode-json /tmp/oc.json`, stub `ollama` on PATH):**
- Prereq screen when opencode/ollama missing; proceed when present.
- Onboarding on a fixture model list; "no models" → pull flow (stubbed).
- Configure → write → opencode.json valid + backup present.
- Dashboard nav; load/unload/residency issue correct API calls (mock the daemon).
- Device-mode toggle persists; `--dry-run` performs no writes/loads.

**Manual (real machines):**
- Desktop (RTX): `ollama pull` a small + a large model; load 30B, watch VRAM bar + `ollama ps` live; open OpenCode, quit, confirm "still loaded (unloads in N)"; unload, watch VRAM drop; let auto-unload fire and confirm the row clears.
- Laptop `--cpu-only`: small model, RAM-based estimate, session launch.
- Calibrate estimate vs the post-load `ollama ps` GPU/CPU split.
- Suspend-and-return leaves the terminal clean after OpenCode exits.

---

## 16. README outline

- What it is (dashboard for Ollama + OpenCode) + asciinema.
- The two-layer idea in two sentences (model = Ollama/VRAM; session = OpenCode), so users get why "unload" lives in tuicode and quitting OpenCode doesn't free VRAM.
- **Prerequisites (prominent):** OpenCode **and** Ollama, installed separately; installing OpenCode does not install Ollama. `nvidia-smi` optional. Install matrix:

  | | OpenCode | Ollama |
  |---|---|---|
  | Arch | `sudo pacman -S opencode` | `sudo pacman -S ollama-cuda` + `sudo systemctl enable --now ollama` |
  | Fedora | `curl -fsSL https://opencode.ai/install \| bash` | `curl -fsSL https://ollama.com/install.sh \| sh` |
  | Ubuntu/Debian | `curl -fsSL https://opencode.ai/install \| bash` | `curl -fsSL https://ollama.com/install.sh \| sh` |

- Install tuicode: one-liner + `go install`.
- Quick start: `tuicode` → prereq check → (no models? pull one) → load → open OpenCode → quit → model auto-unloads after idle (or unload now).
- **Models:** download with `ollama pull <name>`; Ollama manages storage, you don't place files. Want them on another drive? set `OLLAMA_MODELS` via a systemd override (link to steps); tuicode shows the active path in settings.
- **Residency:** three modes; default auto-unloads after ~20 min idle so VRAM frees itself; keep-warm holds it; manual unload is one key.
- Device modes (`--cpu-only` for laptops).
- File locations (`~/.config/tuicode/...`).
- Troubleshooting: Ollama daemon down (`systemctl status ollama`); OpenCode auth ("Other" + provider `ollama` + dummy key); can't reach 64k context; model spilled to CPU (lower context); VRAM full after switching (unload old model or wait for auto-unload).

---

## 17. Definition of done

- Startup dependency check detects OpenCode + Ollama + nvidia-smi; missing essentials → friendly distro-aware install screen + clean quit; daemon-down flagged with the start command.
- Dashboard shows live loaded models + VRAM + residency countdown, polled, reflecting real Ollama state.
- Load / unload / swap work via Ollama; VRAM bar moves; auto-unload clears rows unattended.
- Residency enforced: default frees VRAM after idle; keep-warm holds; manual unload is one keystroke.
- Open OpenCode against a loaded model (suspend-and-return); on exit, model remains per residency and is shown as such.
- opencode.json writes are backed up, idempotent, non-destructive.
- Onboarding can `ollama pull` a starter model so a fresh user reaches a working state in-TUI.
- CPU-only laptop path works via `--cpu-only`.
- Install script works on clean Arch/Fedora/Ubuntu; README explains the two-layer model + prereqs.
- `server` is behind a `Backend` interface so LM Studio (or others) can be added later without UI rework.

**Rough effort:** ~10–13 hours. Dropping to a single backend removes a whole parallel code path; the live polling dashboard and the suspend-and-return launch are the main time sinks.
