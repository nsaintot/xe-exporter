package xpumanager

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"xe-exporter/internal/metrics"
)

// Provider implements metrics.GPUProvider via direct CGo calls into libxpum.so.
// It calls xpumInit on construction and xpumShutdown on Close.
type Provider struct {
	debug bool
}

// NewProvider initialises the XPUM library and returns a ready Provider.
// The caller must call Close() when done.
func NewProvider(debug bool) (*Provider, error) {
	if err := cInit(); err != nil {
		return nil, fmt.Errorf("xpumanager: init failed: %w", err)
	}
	return &Provider{debug: debug}, nil
}

// Close shuts down the XPUM library.  Safe to call more than once.
func (p *Provider) Close() { cShutdown() }

// Devices queries the XPUM library for all GPU devices and their current
// telemetry, returning one GPUDevice per physical device.
func (p *Provider) Devices(ctx context.Context) ([]metrics.GPUDevice, error) {
	_ = ctx

	basicInfos, err := cGetDeviceList()
	if err != nil {
		return nil, fmt.Errorf("device list: %w", err)
	}

	out := make([]metrics.GPUDevice, 0, len(basicInfos))
	for _, info := range basicInfos {
		dev, err := p.collectDevice(info)
		if err != nil {
			if p.debug {
				log.Printf("xpumanager: device %d collection error: %v\n", info.DeviceID, err)
			}
			continue // skip this device but continue with others
		}
		out = append(out, dev)
	}
	return out, nil
}

// collectDevice assembles a fully-populated GPUDevice from the three XPUM
// stat calls: xpumGetStats, xpumGetEngineStats, xpumGetFabricThroughputStats.
func (p *Provider) collectDevice(info cDeviceBasicInfo) (metrics.GPUDevice, error) {
	dev := metrics.GPUDevice{
		ID:   strconv.Itoa(int(info.DeviceID)),
		UUID: info.UUID,
		Name: info.DeviceName,
	}

	// ── 1. Device-level stats (power, freq, temp, memory, errors, EU…) ────────
	stats, err := cGetStats(info.DeviceID)
	if err != nil {
		return dev, fmt.Errorf("stats: %w", err)
	}
	applyDeviceStats(&dev, stats)

	// ── 2. Per-engine utilisation ─────────────────────────────────────────────
	engines, err := cGetEngineStats(info.DeviceID)
	if err != nil {
		// Non-fatal: engine stats may be unavailable on some driver versions.
		if p.debug {
			log.Printf("xpumanager: engine stats device %d: %v\n", info.DeviceID, err)
		}
	} else {
		dev.Engines = buildEngineMetrics(engines)
	}

	// ── 3. Xe-Link / fabric throughput ───────────────────────────────────────
	fabric, err := cGetFabricStats(info.DeviceID)
	if err != nil && p.debug {
		log.Printf("xpumanager: fabric stats device %d: %v\n", info.DeviceID, err)
	}
	dev.XeLinkThroughputKBps = sumFabricThroughput(fabric)

	return dev, nil
}

// applyDeviceStats reads each cStatEntry and sets the matching GPUDevice field.
// Unknown metric types are silently ignored so new XPUM versions don't break us.
func applyDeviceStats(dev *metrics.GPUDevice, stats []cStatEntry) {
	for _, s := range stats {
		// For counters (RAS errors) we use the cumulative accumulated value so
		// Prometheus sees a monotonically increasing counter.
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
			dev.MemoryUsedMiB = v
		case statMemUtil:
			dev.MemoryUtilPercent = v

		// RAS errors ───────────────────────────────────────────────────────────
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

// buildEngineMetrics converts cEngineEntry slices into metrics.EngineMetrics.
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

// engineTypeToString maps XPUM engine type integer to a human-readable label.
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
		return metrics.EngineTypeRender // 3D maps to render group
	default:
		return metrics.EngineTypeUnknown
	}
}

// sumFabricThroughput aggregates all received+transmitted Xe-Link entries into
// a single combined kB/s figure.  Returns 0 when no Xe-Link is present.
func sumFabricThroughput(entries []cFabricEntry) float64 {
	var total float64
	for _, f := range entries {
		if f.FabricType == fabricTypeReceived || f.FabricType == fabricTypeTransmitted {
			total += f.Value
		}
	}
	return total
}
