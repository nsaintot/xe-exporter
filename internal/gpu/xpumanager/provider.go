package xpumanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"xe-exporter/internal/metrics"
)

// xpumWarmup is the minimum time we let the XPUM internal sampler accumulate
// data after xpumInit before calling xpumGetStats / xpumGetEngineStats.
//
// XPUM collects hardware counter snapshots at ~100 ms intervals.
// Utilisation stats (GPU util, engine group util, EU active/stall/idle) are
// DELTA metrics — they require TWO consecutive snapshots before XPUM can
// report a valid value.  With a warmup shorter than one full collection cycle
// (~1000 ms), XPUM has only taken one snapshot and returns no utilisation
// entries at all, leaving xe_gpu_engine_util_percent completely absent.
//
// 1500 ms gives XPUM time for ≥2 snapshots (1000 ms cycle + 500 ms margin)
// while still leaving >18 s of GPU idle time per 20 s collect-interval.
const xpumWarmup = 1500 * time.Millisecond

// subprocessTimeout caps how long a single collect-once subprocess may run
// before the parent kills it.  xpumInit (~1 s) + warmup (1.5 s) + I/O leaves
// plenty of room within 60 s even on slow hardware.
const subprocessTimeout = 60 * time.Second

// Provider implements metrics.GPUProvider.
//
// Default mode — subprocess-per-cycle (thermal-friendly)
// -------------------------------------------------------
// xpumShutdown() does NOT release the Level Zero DRM file descriptors opened
// by xpumInit().  DeviceManager::close() and GPUDeviceStub::~GPUDeviceStub()
// are both empty stubs in the xpumanager source; zeInit/zesInit are called
// once and never un-called.  A long-lived process that calls xpumInit/Shutdown
// in a loop still holds L0 handles permanently → the GPU cannot power-gate →
// +12 °C persistent thermal floor.
//
// The only reliable way to release L0 resources is to let the OS close all file
// descriptors on process exit.  In default mode Provider therefore spawns
// os.Args[0] with -collect-once on each collect-interval tick.  The subprocess:
//   1. calls xpumInit()
//   2. runs the two-pass baseline+warmup
//   3. collects all device metrics
//   4. calls xpumShutdown() (via defer)
//   5. writes JSON to stdout and exits
//   6. OS closes all L0 FDs → GPU sleeps
//
// Daemon mode (-daemon-collector-mode=true)
// -----------------------------------------
// Reverts to the original single-process model: a background goroutine (pinned
// to one OS thread) runs the init→warmup→collect→shutdown cycle in-process.
// L0 handles stay open between cycles; expect the ~12 °C thermal floor.
// Use only when subprocess spawning is not viable (sandboxed containers, etc.).
type Provider struct {
	debug           bool
	collectInterval time.Duration
	daemonMode      bool // see -daemon-collector-mode flag

	mu      sync.RWMutex
	cache   []metrics.GPUDevice
	ready   bool  // true once the first successful collection completes
	lastErr error // last collection error (informational)

	cancel context.CancelFunc
	done   chan struct{}
}

// NewProvider creates a Provider and immediately starts the background
// collection goroutine.  When daemonMode is false (the default), it spawns a
// subprocess per cycle; when true it collects in-process.
func NewProvider(ctx context.Context, debug bool, collectInterval time.Duration, daemonMode bool) (*Provider, error) {
	if collectInterval <= 0 {
		collectInterval = 10 * time.Second
	}
	pCtx, cancel := context.WithCancel(ctx)
	p := &Provider{
		debug:           debug,
		collectInterval: collectInterval,
		daemonMode:      daemonMode,
		cancel:          cancel,
		done:            make(chan struct{}),
	}
	go p.loop(pCtx)
	return p, nil
}

// Close stops the background collection goroutine and waits for it to exit.
func (p *Provider) Close() {
	p.cancel()
	<-p.done
}

// Devices returns the most recently collected GPU snapshot.
// It never blocks on hardware I/O; if the first collection is not yet done it
// returns an error so the caller can retry on the next scrape.
func (p *Provider) Devices(_ context.Context) ([]metrics.GPUDevice, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.ready {
		return nil, fmt.Errorf("xpumanager: first collection not yet complete")
	}
	return p.cache, p.lastErr
}

// loop is the background goroutine.  It collects immediately on start, then
// on every collectInterval tick, using either the subprocess or in-process path.
func (p *Provider) loop(ctx context.Context) {
	defer close(p.done)

	if p.daemonMode {
		// Pin to one OS thread for the lifetime of this goroutine: Level Zero
		// stores per-thread handles and can behave inconsistently if the goroutine
		// migrates between OS threads between CGo calls.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Initialise XPUM exactly once for the daemon lifetime.
		// Do NOT call xpumShutdown/xpumInit between cycles.
		// The xpumanager source shows DeviceManager::close() is an empty stub —
		// L0 FDs are never released regardless.  More critically, calling
		// xpumShutdown resets the monitoring-manager's session state; a subsequent
		// xpumInit restarts the sampler from a blank slate, and the second (and
		// all subsequent) collections then observe a ~0 ms Phase-1 window
		// (session 0 never previously read) followed by stale counter deltas,
		// which causes all metrics to appear frozen after the first cycle.
		if err := cInit(); err != nil {
			log.Printf("xpumanager daemon: xpumInit failed: %v\n", err)
			p.mu.Lock()
			p.lastErr = fmt.Errorf("xpumInit: %w", err)
			p.mu.Unlock()
			return
		}
		defer cShutdown()

		p.collectInProcess()
		ticker := time.NewTicker(p.collectInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.collectInProcess()
			}
		}
	} else {
		p.spawn()
		ticker := time.NewTicker(p.collectInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.spawn()
			}
		}
	}
}

// ── Subprocess path (default) ─────────────────────────────────────────────────

// spawn launches os.Args[0] -collect-once as a child process, reads the JSON
// result from its stdout, and updates the cache.
// Subprocess stderr is forwarded to the parent's stderr so debug log lines
// appear in the exporter's own log stream.
func (p *Provider) spawn() {
	args := []string{"-collect-once"}
	if p.debug {
		args = append(args, "-debug")
	}

	ctx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Stderr = os.Stderr

	start := time.Now()
	out, err := cmd.Output()
	if err != nil {
		log.Printf("xpumanager: collect-once subprocess failed: %v (stale data will be served)\n", err)
		p.mu.Lock()
		p.lastErr = fmt.Errorf("subprocess: %w", err)
		p.mu.Unlock()
		return
	}

	var devices []metrics.GPUDevice
	dec := json.NewDecoder(bytes.NewReader(out))
	if err := dec.Decode(&devices); err != nil {
		preview := out
		if len(preview) > 512 {
			preview = preview[:512]
		}
		log.Printf("xpumanager: failed to decode collect-once output: %v\nraw (first %d bytes): %s\n",
			err, len(preview), preview)
		p.mu.Lock()
		p.lastErr = fmt.Errorf("json decode: %w", err)
		p.mu.Unlock()
		return
	}

	p.mu.Lock()
	p.cache = devices
	p.lastErr = nil
	p.ready = true
	p.mu.Unlock()

	log.Printf("xpumanager: collected %d device(s) in %s\n", len(devices), time.Since(start).Round(time.Millisecond))
}

// ── In-process path (daemon mode) ────────────────────────────────────────────

// collectInProcess calls runMeasurement directly in the caller's goroutine and
// updates the cache.  Only used when -daemon-collector-mode=true.
// Assumes cInit() has already been called once by loop().
func (p *Provider) collectInProcess() {
	start := time.Now()
	devices, err := runMeasurement(p.debug)
	if err != nil {
		log.Printf("xpumanager: collection failed: %v (stale data will be served)\n", err)
		p.mu.Lock()
		p.lastErr = err
		p.mu.Unlock()
		return
	}

	p.mu.Lock()
	p.cache = devices
	p.lastErr = nil
	p.ready = true
	p.mu.Unlock()

	log.Printf("xpumanager: collected %d device(s) in %s\n", len(devices), time.Since(start).Round(time.Millisecond))
}

// ── Subprocess entry point ────────────────────────────────────────────────────

// RunCollectOnce is called by the subprocess (when -collect-once is set).
// It runs one full init → baseline → warmup → gather → shutdown cycle,
// writes the result as JSON to stdout, and returns.
//
// It must not be called from the long-lived server process.  Running it in a
// subprocess guarantees that all Level Zero file descriptors are released by
// the OS on exit, allowing the GPU to power-gate between collection cycles.
func RunCollectOnce(debug bool) error {
	// Pin this goroutine to one OS thread so Level Zero per-thread handles
	// remain consistent across all CGo calls in this function.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Redirect C's buffered stdout (fd 1) to stderr (fd 2) before any CGo
	// call.  The XPUM C++ library writes diagnostic output directly to C's
	// stdio stdout, which would otherwise appear on the pipe the parent reads
	// for JSON and corrupt the payload.  We save a copy of fd 1, point it at
	// fd 2 for the duration of the CGo work, then restore it just before we
	// write the JSON result so the parent sees clean output.
	savedStdout, saveErr := syscall.Dup(1)
	if saveErr == nil {
		syscall.CloseOnExec(savedStdout)
		_ = syscall.Dup2(2, 1) // fd 1 now → stderr
	}

	devices, collectErr := runOneCycle(debug)

	// Restore fd 1 before writing JSON — regardless of collectErr.
	if saveErr == nil {
		_ = syscall.Dup2(savedStdout, 1)
		syscall.Close(savedStdout)
	}

	if collectErr != nil {
		return collectErr
	}
	return json.NewEncoder(os.Stdout).Encode(devices)
}

// ── Core collection cycle ─────────────────────────────────────────────────────

// runOneCycle is the subprocess entry point: init → measure → shutdown.
// Used only by RunCollectOnce (the short-lived child process).
func runOneCycle(debug bool) ([]metrics.GPUDevice, error) {
	if err := cInit(); err != nil {
		return nil, fmt.Errorf("xpumInit: %w", err)
	}
	defer cShutdown()
	return runMeasurement(debug)
}

// runMeasurement performs the two-pass baseline → warmup → collect cycle.
// XPUM must already be initialised (cInit called) before calling this function.
// It is invoked by runOneCycle (subprocess path, where cInit wraps each call)
// and directly by collectInProcess (daemon-mode path, where cInit is called
// once for the lifetime of the process in loop()).
//
// Two-pass session design
// -----------------------
// XPUM's monitor manager is a process-level singleton.  Its internal session
// state (the "last read" timestamp for each sessionId) persists across
// xpumShutdown/xpumInit cycles.  Without intervention, a single call to
// xpumGetStats(sessionId=0) after warmup returns the delta from the END of
// the PREVIOUS cycle's warmup to now — a window of collectInterval+warmup
// (~21 s) instead of the intended warmup window (~1.5 s).  Under steady load
// the averaged values are indistinguishable → metrics appear "frozen".
//
// Fix: call each stat function TWICE per cycle using the stable sessionId=0:
//  1. Baseline call (before warmup) — re-anchors "session 0 last read at T₀".
//     Result discarded; we only care about moving the window start to now.
//  2. Data call (after warmup) — returns delta [T₀, T₀+warmup].
func runMeasurement(debug bool) ([]metrics.GPUDevice, error) {
	basicInfos, err := cGetDeviceList()
	if err != nil {
		return nil, fmt.Errorf("device list: %w", err)
	}

	// sessionID=0 is the only session identifier that remains valid across
	// xpumShutdown/xpumInit cycles.
	const sessionID uint64 = 0

	// ── Phase 1: establish measurement baselines ──────────────────────────────
	for _, info := range basicInfos {
		_, b1, e1, err := cGetStats(info.DeviceID, sessionID)
		if err != nil {
			log.Printf("xpumanager: device %d Phase-1 baseline error (delta window not anchored): %v\n", info.DeviceID, err)
		} else if debug {
			log.Printf("xpumanager: device %d Phase-1 window: begin=%d end=%d span=%d\n", info.DeviceID, b1, e1, e1-b1)
		}
		baselineEngines, b1e, e1e, baselineEngErr := cGetEngineStats(info.DeviceID, sessionID)
		if debug {
			if baselineEngErr != nil {
				log.Printf("xpumanager: device %d Phase-1 engine baseline error (ignored): %v\n", info.DeviceID, baselineEngErr)
			} else {
				log.Printf("xpumanager: device %d Phase-1 engine baseline: %d entries returned (window span=%d)\n", info.DeviceID, len(baselineEngines), e1e-b1e)
				for _, be := range baselineEngines {
					et := engineTypeToString(be.EngineType)
					log.Printf("xpumanager:   Phase-1 engine type=%-10s index=%d util=%.2f%% (raw_avg=%d raw_cur=%d scale=%d)\n",
						et, be.EngineIndex, be.Value, be.RawValue, be.RawCur, be.RawScale)
				}
			}
		}
	}

	// ── Phase 2: warmup ───────────────────────────────────────────────────────
	time.Sleep(xpumWarmup)

	// ── Phase 3: data call — delta over [T₀, T₀+warmup] ─────────────────────
	out := make([]metrics.GPUDevice, 0, len(basicInfos))
	for _, info := range basicInfos {
		dev, err := collectDevice(info, sessionID, debug)
		if err != nil {
			log.Printf("xpumanager: device %d collection error: %v (skipping device)\n", info.DeviceID, err)
			continue
		}
		out = append(out, dev)
	}

	return out, nil
}

// ── Per-device collection ─────────────────────────────────────────────────────

// collectDevice assembles a fully-populated GPUDevice from the three XPUM
// stat calls: xpumGetStats, xpumGetEngineStats, xpumGetFabricThroughputStats.
func collectDevice(info cDeviceBasicInfo, sessionID uint64, debug bool) (metrics.GPUDevice, error) {
	dev := metrics.GPUDevice{
		ID:   strconv.Itoa(int(info.DeviceID)),
		UUID: info.UUID,
		Name: info.DeviceName,
	}

	// ── 1. Device-level stats (power, freq, temp, memory, errors, EU…) ────────
	stats, begin, end, err := cGetStats(info.DeviceID, sessionID)
	if err != nil {
		return dev, fmt.Errorf("stats: %w", err)
	}
	if debug {
		log.Printf("xpumanager: device %d Phase-3 window: begin=%d end=%d span=%d\n", info.DeviceID, begin, end, end-begin)
	}
	applyDeviceStats(&dev, stats)

	// ── 2. Per-engine utilisation ─────────────────────────────────────────────
	engines, engBegin, engEnd, err := cGetEngineStats(info.DeviceID, sessionID)
	if err != nil {
		log.Printf("xpumanager: device %d xpumGetEngineStats unavailable (%v); falling back to group-aggregate engine stats\n", info.DeviceID, err)
	}
	if len(engines) > 0 {
		dev.Engines = buildEngineMetrics(engines)
		if debug {
			log.Printf("xpumanager: device %d engines Phase-3 window: begin=%d end=%d span=%d\n", info.DeviceID, engBegin, engEnd, engEnd-engBegin)
			log.Printf("xpumanager: device %d engines: %d entries from xpumGetEngineStats\n", info.DeviceID, len(dev.Engines))
			for i, e := range dev.Engines {
				raw := engines[i]
				log.Printf("xpumanager:   engine type=%-10s index=%d util=%.2f%% (raw_avg=%d raw_cur=%d scale=%d)\n",
					e.Type, e.Index, e.UtilPercent, raw.RawValue, raw.RawCur, raw.RawScale)
			}
		}
	} else {
		dev.Engines = engineGroupsFromStats(stats)
		if debug {
			log.Printf("xpumanager: device %d engines Phase-3 window: begin=%d end=%d span=%d\n", info.DeviceID, engBegin, engEnd, engEnd-engBegin)
			log.Printf("xpumanager: device %d engines: %d entries from group-aggregate fallback\n", info.DeviceID, len(dev.Engines))
			for _, e := range dev.Engines {
				log.Printf("xpumanager:   engine type=%-10s index=%d util=%.2f%%\n", e.Type, e.Index, e.UtilPercent)
			}
		}
	}

	// ── 3. Xe-Link / fabric throughput ───────────────────────────────────────
	fabric, err := cGetFabricStats(info.DeviceID, sessionID)
	if err != nil && debug {
		log.Printf("xpumanager: fabric stats device %d: %v\n", info.DeviceID, err)
	}
	dev.XeLinkThroughputKBps = sumFabricThroughput(fabric)

	if debug {
		log.Printf("xpumanager: device %d values: util=%.1f%% power=%.1fW temp=%.1f°C freq=%.0fMHz mem=%.0fMiB\n",
			info.DeviceID, dev.GPUUtilPercent, dev.PowerWatts, dev.CoreTempC, dev.GPUFrequencyMHz, dev.MemoryUsedMiB)
	}

	return dev, nil
}

// ── Stat helpers ──────────────────────────────────────────────────────────────

func applyDeviceStats(dev *metrics.GPUDevice, stats []cStatEntry) {
	for _, s := range stats {
		v := s.Value
		if s.IsCounter {
			v = s.Accumulated
		}
		switch s.MetricType {
		case statGPUUtilization:
			dev.GPUUtilPercent = v
		case statEUActive:
			dev.EUActivePercent = v
		case statEUStall:
			dev.EUStallPercent = v
		case statEUIdle:
			dev.EUIdlePercent = v
		case statPower:
			dev.PowerWatts = v
		case statGPUFrequency:
			dev.GPUFrequencyMHz = v
		case statMediaFrequency:
			dev.MediaFrequencyMHz = v
		case statCoreTempC:
			dev.CoreTempC = v
		case statMemTempC:
			dev.MemoryTempC = v
		case statMemReadTP:
			dev.MemoryReadKBps = v
		case statMemWriteTP:
			dev.MemoryWriteKBps = v
		case statMemBandwidth:
			dev.MemoryBandwidthUtil = v
		case statMemUsed:
			dev.MemoryUsedMiB = v / (1024 * 1024) // XPUM reports bytes (scale=1); convert to MiB
		case statMemUtil:
			dev.MemoryUtilPercent = v
		case statRasReset:
			dev.ErrorsReset = v
		case statRasProgramming:
			dev.ErrorsProgramming = v
		case statRasDriver:
			dev.ErrorsDriver = v
		case statRasCacheCorr:
			dev.ErrorsCacheCorrectable = v
		case statRasCacheUncorr:
			dev.ErrorsCacheUncorrectable = v
		case statRasNonCompCorr:
			dev.ErrorsMemCorrectable = v
		case statRasNonCompUncorr:
			dev.ErrorsMemUncorrectable = v
		}
	}
}

func buildEngineMetrics(entries []cEngineEntry) []metrics.EngineMetrics {
	out := make([]metrics.EngineMetrics, 0, len(entries))
	for _, e := range entries {
		out = append(out, metrics.EngineMetrics{
			Type:        engineTypeToString(e.EngineType),
			Index:       int(e.EngineIndex),
			UtilPercent: e.Value,
		})
	}
	return out
}

func engineTypeToString(t int32) metrics.EngineType {
	switch t {
	case engineTypeCompute:
		return metrics.EngineTypeCompute
	case engineTypeRender:
		return metrics.EngineTypeRender
	case engineTypeDecode:
		return metrics.EngineTypeDecoder
	case engineTypeEncode:
		return metrics.EngineTypeEncoder
	case engineTypeCopy:
		return metrics.EngineTypeCopy
	case engineTypeMediaEM:
		return metrics.EngineTypeMediaEM
	case engineType3D:
		return metrics.EngineTypeRender
	default:
		return metrics.EngineTypeUnknown
	}
}

func engineGroupsFromStats(stats []cStatEntry) []metrics.EngineMetrics {
	var out []metrics.EngineMetrics
	for _, s := range stats {
		if s.IsCounter {
			continue
		}
		var et metrics.EngineType
		switch s.MetricType {
		case statEngineGroupCompute:
			et = metrics.EngineTypeCompute
		case statEngineGroupMedia:
			et = metrics.EngineTypeMedia
		case statEngineGroupCopy:
			et = metrics.EngineTypeCopy
		case statEngineGroupRender:
			et = metrics.EngineTypeRender
		default:
			continue
		}
		out = append(out, metrics.EngineMetrics{
			Type:        et,
			Index:       0,
			UtilPercent: s.Value,
		})
	}
	return out
}

func sumFabricThroughput(entries []cFabricEntry) float64 {
	var total float64
	for _, f := range entries {
		if f.FabricType == fabricTypeReceived || f.FabricType == fabricTypeTransmitted {
			total += f.Value
		}
	}
	return total
}
