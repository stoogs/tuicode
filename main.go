// Command tuicode is a terminal dashboard for running local LLMs with OpenCode,
// backed by Ollama. See docs/BRIEF_tuicode.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/deps"
	"tuicode/internal/hw"
	"tuicode/internal/ocfg"
	"tuicode/internal/server"
	"tuicode/internal/store"
	"tuicode/internal/tui"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	var (
		flagVersion   = flag.Bool("version", false, "print version and exit")
		flagCPUOnly   = flag.Bool("cpu-only", false, "force RAM-based estimation (laptops)")
		flagGPUOnly   = flag.Bool("gpu-only", false, "force GPU-based estimation")
		flagOpencode  = flag.String("opencode-json", "", "target a specific opencode.json")
		flagConfigDir = flag.String("config-dir", "", "override ~/.config/tuicode (testing)")
		flagDryRun    = flag.Bool("dry-run", false, "show writes/loads without performing them")
		flagVerbose   = flag.Bool("verbose", false, "log detection/API/CLI calls to stderr")
		flagEndpoint  = flag.String("endpoint", server.DefaultEndpoint, "Ollama endpoint")
		flagPrintDet  = flag.Bool("print-detection", false, "print detected hardware/deps and exit")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Println("tuicode", version)
		return
	}

	logf := func(string) {}
	if *flagVerbose {
		logger := log.New(os.Stderr, "tuicode: ", log.Ltime)
		logf = func(s string) { logger.Println(s) }
	}

	// --- store / config ---
	st, err := store.New(*flagConfigDir)
	if err != nil {
		fatal("config dir: %v", err)
	}
	appCfg, err := st.LoadAppConfig()
	if err != nil {
		logf(fmt.Sprintf("warning: load config: %v", err))
		appCfg = store.DefaultAppConfig()
	}

	// --- backend ---
	be := server.NewOllama(*flagEndpoint)
	be.DryRun = *flagDryRun
	if *flagVerbose {
		be.Log = logf
	}

	// --- device mode (flags override sticky config) ---
	mode := hw.DeviceMode(appCfg.DeviceMode)
	if mode == "" {
		mode = hw.Auto
	}
	switch {
	case *flagCPUOnly:
		mode = hw.CPUOnly
	case *flagGPUOnly:
		mode = hw.GPUOnly
	}
	appCfg.DeviceMode = string(mode)

	// --- dependency check ---
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	report := deps.Check(ctx, be)
	cancel()

	// --- print detection and exit (debugging / validating the OS code path) ---
	if *flagPrintDet {
		dctx, dcancel := context.WithTimeout(context.Background(), 8*time.Second)
		det := hw.Detect(dctx, mode)
		dcancel()
		printDetection(os.Stdout, mode, det, report)
		return
	}

	// --- resolve opencode.json + working dir ---
	cwd, _ := os.Getwd()
	target := *flagOpencode
	if target == "" {
		target = appCfg.OpencodeJSON
	}
	if target == "" {
		target, err = ocfg.DefaultTarget(cwd)
		if err != nil {
			logf(fmt.Sprintf("warning: resolve opencode.json: %v", err))
			target = "opencode.json"
		}
	}
	workdir := appCfg.WorkingDir
	if workdir == "" {
		workdir = cwd
	}

	opencodeBin := "opencode"
	if report.OpenCode.Path != "" {
		opencodeBin = report.OpenCode.Path
	}

	// Pull-screen lists: a curated/cached trending list and the editable
	// benchmark reference. Both seed from embedded defaults on first run.
	trending, err := st.LoadTrending()
	if err != nil {
		logf(fmt.Sprintf("warning: load trending: %v", err))
	}
	recommended, err := st.LoadRecommended()
	if err != nil {
		logf(fmt.Sprintf("warning: load recommended: %v", err))
	}

	opts := tui.Options{
		Backend:      be,
		Store:        st,
		AppConfig:    appCfg,
		Deps:         report,
		DeviceMode:   mode,
		OpencodeJSON: target,
		WorkingDir:   workdir,
		OpencodeBin:  opencodeBin,
		Trending:     trending,
		Recommended:  recommended,
		DryRun:       *flagDryRun,
		Verbose:      *flagVerbose,
		Logf:         logf,
	}

	m := tui.New(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fatal("tui: %v", err)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "tuicode: "+format+"\n", a...)
	os.Exit(1)
}

// printDetection writes a human-readable summary of detected hardware and
// dependencies, then returns. Used by --print-detection to validate the per-OS
// code path (e.g. unified vs RAM-only) without launching the TUI.
func printDetection(w io.Writer, mode hw.DeviceMode, det hw.Detection, report deps.Report) {
	const gib = 1024 * 1024 * 1024
	mem := det.Authoritative()
	kind := mem.Source
	if det.Unified {
		kind = "unified (Apple Silicon — one pool, no CPU/GPU split)"
	}

	fmt.Fprintf(w, "tuicode %s — detection\n", version)
	fmt.Fprintf(w, "  OS:         %s (%s)\n", report.Distro.Label(), report.Distro.Family)
	fmt.Fprintf(w, "  Device:     %s\n", mode)
	fmt.Fprintf(w, "  Memory:     %s\n", kind)
	fmt.Fprintf(w, "    total     %.1f GB\n", mem.TotalGB())
	fmt.Fprintf(w, "    free      %.1f GB\n", mem.FreeGB())
	fmt.Fprintf(w, "    reserve   %.1f GB (headroom)\n", float64(det.Reserve())/gib)
	if det.GPU != nil {
		tag := "discrete VRAM"
		if det.Unified {
			tag = "unified"
		}
		fmt.Fprintf(w, "  GPU:        %s (%s)\n", det.GPUName, tag)
	} else {
		fmt.Fprintf(w, "  GPU:        none — RAM is authoritative\n")
	}
	fmt.Fprintf(w, "  OpenCode:   %s\n", toolLine(report.OpenCode))
	fmt.Fprintf(w, "  Ollama:     %s  · daemon %s\n", toolLine(report.Ollama), daemonState(report))
	if report.NvidiaSMI.Found {
		fmt.Fprintf(w, "  nvidia-smi: %s\n", report.NvidiaSMI.Path)
	}
}

func toolLine(t deps.Tool) string {
	if !t.Found {
		return "not found"
	}
	if t.Version != "" {
		return fmt.Sprintf("%s (%s)", t.Path, t.Version)
	}
	return t.Path
}

func daemonState(r deps.Report) string {
	switch {
	case !r.Ollama.Found:
		return "n/a"
	case r.Daemon.Reachable:
		return "up"
	default:
		return "down"
	}
}
