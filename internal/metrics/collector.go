package metrics

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// GPUProvider is implemented by backends that can supply GPU telemetry snapshots.
type GPUProvider interface {
	// Devices returns a snapshot of all visible GPU devices.
	Devices(ctx context.Context) ([]GPUDevice, error)
}

// GPUDevice represents a logical GPU and its current metrics.
type GPUDevice struct {
	ID   string
	UUID string
	Name string // device model name (e.g. "Intel Data Center GPU Max 1100")

	// Overall utilisation
	GPUUtilPercent float64 // XPUM_STATS_GPU_UTILIZATION

	// EU array state (N/A on many consumer / older Gen GPUs)
	EUActivePercent float64
	EUStallPercent  float64
	EUIdlePercent   float64

	// Per-engine utilisation (from xpumGetEngineStats)
	Engines []EngineMetrics

	// Power & frequency
	PowerWatts        float64
	GPUFrequencyMHz   float64
	MediaFrequencyMHz float64

	// Temperature
	CoreTempC   float64
	MemoryTempC float64

	// Memory
	MemoryReadKBps      float64
	MemoryWriteKBps     float64
	MemoryBandwidthUtil float64
	MemoryUsedMiB       float64
	MemoryUtilPercent   float64

	// Xe-Link (fabric) throughput
	XeLinkThroughputKBps float64

	// RAS error counters (cumulative)
	ErrorsReset              float64
	ErrorsProgramming        float64
	ErrorsDriver             float64
	ErrorsCacheCorrectable   float64
	ErrorsCacheUncorrectable float64
	ErrorsMemCorrectable     float64
	ErrorsMemUncorrectable   float64
}

// ── Engine metrics ────────────────────────────────────────────────────────────

type EngineType string

const (
	EngineTypeCompute EngineType = "compute"
	EngineTypeRender  EngineType = "render"
	EngineTypeMedia   EngineType = "media"
	EngineTypeDecoder EngineType = "decoder"
	EngineTypeEncoder EngineType = "encoder"
	EngineTypeCopy    EngineType = "copy"
	EngineTypeMediaEM EngineType = "media_em"
	EngineTypeUnknown EngineType = "unknown"
)

type EngineMetrics struct {
	Type        EngineType
	Index       int
	UtilPercent float64
}

// ── Collector ─────────────────────────────────────────────────────────────────

// Collector registers OTel observable instruments and wires them to a
// GPUProvider via a single callback per collection cycle.
type Collector struct {
	meter    metric.Meter
	provider GPUProvider
	debug    bool

	// Overall GPU utilisation
	gpuUtil metric.Float64ObservableGauge

	// EU array state
	euActive metric.Float64ObservableGauge
	euStall  metric.Float64ObservableGauge
	euIdle   metric.Float64ObservableGauge

	// Per-engine utilisation (labelled by engine_type + engine_index)
	engineUtil metric.Float64ObservableGauge

	// Power, frequency, temperature
	powerWatts   metric.Float64ObservableGauge
	gpuFreqMHz   metric.Float64ObservableGauge
	mediaFreqMHz metric.Float64ObservableGauge
	coreTempC    metric.Float64ObservableGauge
	memTempC     metric.Float64ObservableGauge

	// Memory
	memReadKBps      metric.Float64ObservableGauge
	memWriteKBps     metric.Float64ObservableGauge
	memBandwidthUtil metric.Float64ObservableGauge
	memUsedMiB       metric.Float64ObservableGauge
	memUtilPercent   metric.Float64ObservableGauge

	// Xe-Link
	xelinkThroughput metric.Float64ObservableGauge

	// RAS errors (monotonically increasing counters)
	errorsReset              metric.Float64ObservableCounter
	errorsProgramming        metric.Float64ObservableCounter
	errorsDriver             metric.Float64ObservableCounter
	errorsCacheCorrectable   metric.Float64ObservableCounter
	errorsCacheUncorrectable metric.Float64ObservableCounter
	errorsMemCorrectable     metric.Float64ObservableCounter
	errorsMemUncorrectable   metric.Float64ObservableCounter
}

func NewCollector(meter metric.Meter, provider GPUProvider, debug bool) (*Collector, error) {
	c := &Collector{
		meter:    meter,
		provider: provider,
		debug:    debug,
	}

	var err error

	// Overall GPU utilisation (XPUM_STATS_GPU_UTILIZATION)
	if c.gpuUtil, err = meter.Float64ObservableGauge(
		"xe_gpu_utilization_percent",
		metric.WithDescription("Overall GPU utilization in percent (XPUM_STATS_GPU_UTILIZATION)"),
	); err != nil {
		return nil, err
	}

	// EU array state
	if c.euActive, err = meter.Float64ObservableGauge(
		"xe_gpu_eu_active_percent",
		metric.WithDescription("EU Array Active utilization in percent"),
	); err != nil {
		return nil, err
	}
	if c.euStall, err = meter.Float64ObservableGauge(
		"xe_gpu_eu_stall_percent",
		metric.WithDescription("EU Array Stall utilization in percent"),
	); err != nil {
		return nil, err
	}
	if c.euIdle, err = meter.Float64ObservableGauge(
		"xe_gpu_eu_idle_percent",
		metric.WithDescription("EU Array Idle utilization in percent"),
	); err != nil {
		return nil, err
	}

	// Per-engine utilisation (engine_type + engine_index labels)
	if c.engineUtil, err = meter.Float64ObservableGauge(
		"xe_gpu_engine_util_percent",
		metric.WithDescription("Per-engine utilization in percent"),
	); err != nil {
		return nil, err
	}

	// Power
	if c.powerWatts, err = meter.Float64ObservableGauge(
		"xe_gpu_power_watts",
		metric.WithDescription("GPU power consumption in Watts"),
	); err != nil {
		return nil, err
	}

	// Frequency
	if c.gpuFreqMHz, err = meter.Float64ObservableGauge(
		"xe_gpu_frequency_gpu_mhz",
		metric.WithDescription("GPU core frequency in MHz"),
	); err != nil {
		return nil, err
	}
	if c.mediaFreqMHz, err = meter.Float64ObservableGauge(
		"xe_gpu_frequency_media_mhz",
		metric.WithDescription("Media engine frequency in MHz"),
	); err != nil {
		return nil, err
	}

	// Temperature
	if c.coreTempC, err = meter.Float64ObservableGauge(
		"xe_gpu_temperature_core_celsius",
		metric.WithDescription("GPU core temperature in Celsius"),
	); err != nil {
		return nil, err
	}
	if c.memTempC, err = meter.Float64ObservableGauge(
		"xe_gpu_temperature_memory_celsius",
		metric.WithDescription("GPU memory temperature in Celsius"),
	); err != nil {
		return nil, err
	}

	// Memory
	if c.memReadKBps, err = meter.Float64ObservableGauge(
		"xe_gpu_memory_read_kbytes_per_second",
		metric.WithDescription("GPU memory read throughput in kB/s"),
	); err != nil {
		return nil, err
	}
	if c.memWriteKBps, err = meter.Float64ObservableGauge(
		"xe_gpu_memory_write_kbytes_per_second",
		metric.WithDescription("GPU memory write throughput in kB/s"),
	); err != nil {
		return nil, err
	}
	if c.memBandwidthUtil, err = meter.Float64ObservableGauge(
		"xe_gpu_memory_bandwidth_util_percent",
		metric.WithDescription("GPU memory bandwidth utilization in percent"),
	); err != nil {
		return nil, err
	}
	if c.memUsedMiB, err = meter.Float64ObservableGauge(
		"xe_gpu_memory_used_mebibytes",
		metric.WithDescription("GPU memory used in MiB"),
	); err != nil {
		return nil, err
	}
	if c.memUtilPercent, err = meter.Float64ObservableGauge(
		"xe_gpu_memory_util_percent",
		metric.WithDescription("GPU memory utilization in percent"),
	); err != nil {
		return nil, err
	}

	// Xe-Link
	if c.xelinkThroughput, err = meter.Float64ObservableGauge(
		"xe_gpu_xelink_throughput_kbytes_per_second",
		metric.WithDescription("Xe Link combined throughput in kB/s (received + transmitted)"),
	); err != nil {
		return nil, err
	}

	// RAS error counters
	if c.errorsReset, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_reset_total",
		metric.WithDescription("Total number of GPU resets (RAS)"),
	); err != nil {
		return nil, err
	}
	if c.errorsProgramming, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_programming_total",
		metric.WithDescription("Total number of GPU programming errors (RAS)"),
	); err != nil {
		return nil, err
	}
	if c.errorsDriver, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_driver_total",
		metric.WithDescription("Total number of GPU driver errors (RAS)"),
	); err != nil {
		return nil, err
	}
	if c.errorsCacheCorrectable, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_cache_correctable_total",
		metric.WithDescription("Total number of correctable cache errors (RAS)"),
	); err != nil {
		return nil, err
	}
	if c.errorsCacheUncorrectable, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_cache_uncorrectable_total",
		metric.WithDescription("Total number of uncorrectable cache errors (RAS)"),
	); err != nil {
		return nil, err
	}
	if c.errorsMemCorrectable, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_memory_correctable_total",
		metric.WithDescription("Total number of correctable non-compute errors (RAS)"),
	); err != nil {
		return nil, err
	}
	if c.errorsMemUncorrectable, err = meter.Float64ObservableCounter(
		"xe_gpu_errors_memory_uncorrectable_total",
		metric.WithDescription("Total number of uncorrectable non-compute errors (RAS)"),
	); err != nil {
		return nil, err
	}

	return c, nil
}

// Register wires the OTel callback that collects all metrics on each scrape.
func (c *Collector) Register(ctx context.Context) error {
	_, err := c.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			start := time.Now()
			devices, err := c.provider.Devices(ctx)
			if err != nil {
				if c.debug {
					log.Printf("xpumanager provider error: %v", err)
				}
				return nil
			}

			for _, d := range devices {
				base := attribute.NewSet(
					attribute.String("gpu_id", d.ID),
					attribute.String("gpu_uuid", d.UUID),
					attribute.String("gpu_name", d.Name),
				)

				// Overall GPU utilisation
				o.ObserveFloat64(c.gpuUtil, d.GPUUtilPercent, metric.WithAttributeSet(base))

				// EU array state (may be 0 if unsupported by driver)
				o.ObserveFloat64(c.euActive, d.EUActivePercent, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.euStall, d.EUStallPercent, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.euIdle, d.EUIdlePercent, metric.WithAttributeSet(base))

				// Per-engine utilisation
				for _, e := range d.Engines {
					eAttrs := attribute.NewSet(append(
						base.ToSlice(),
						attribute.String("engine_type", string(e.Type)),
						attribute.Int("engine_index", e.Index),
					)...)
					o.ObserveFloat64(c.engineUtil, e.UtilPercent, metric.WithAttributeSet(eAttrs))
				}

				// Power, frequency, temperature
				o.ObserveFloat64(c.powerWatts, d.PowerWatts, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.gpuFreqMHz, d.GPUFrequencyMHz, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.mediaFreqMHz, d.MediaFrequencyMHz, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.coreTempC, d.CoreTempC, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.memTempC, d.MemoryTempC, metric.WithAttributeSet(base))

				// Memory
				o.ObserveFloat64(c.memReadKBps, d.MemoryReadKBps, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.memWriteKBps, d.MemoryWriteKBps, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.memBandwidthUtil, d.MemoryBandwidthUtil, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.memUsedMiB, d.MemoryUsedMiB, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.memUtilPercent, d.MemoryUtilPercent, metric.WithAttributeSet(base))

				// Xe-Link
				o.ObserveFloat64(c.xelinkThroughput, d.XeLinkThroughputKBps, metric.WithAttributeSet(base))

				// RAS errors
				o.ObserveFloat64(c.errorsReset, d.ErrorsReset, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.errorsProgramming, d.ErrorsProgramming, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.errorsDriver, d.ErrorsDriver, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.errorsCacheCorrectable, d.ErrorsCacheCorrectable, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.errorsCacheUncorrectable, d.ErrorsCacheUncorrectable, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.errorsMemCorrectable, d.ErrorsMemCorrectable, metric.WithAttributeSet(base))
				o.ObserveFloat64(c.errorsMemUncorrectable, d.ErrorsMemUncorrectable, metric.WithAttributeSet(base))
			}

			if c.debug {
				log.Printf("metric collection completed in %s for %d device(s)", time.Since(start), len(devices))
			}
			return nil
		},
		// All instruments must be listed here so OTel knows to trigger the callback.
		c.gpuUtil,
		c.euActive, c.euStall, c.euIdle,
		c.engineUtil,
		c.powerWatts,
		c.gpuFreqMHz, c.mediaFreqMHz,
		c.coreTempC, c.memTempC,
		c.memReadKBps, c.memWriteKBps, c.memBandwidthUtil, c.memUsedMiB, c.memUtilPercent,
		c.xelinkThroughput,
		c.errorsReset, c.errorsProgramming, c.errorsDriver,
		c.errorsCacheCorrectable, c.errorsCacheUncorrectable,
		c.errorsMemCorrectable, c.errorsMemUncorrectable,
	)
	return err
}
