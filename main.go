package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type GTStats struct {
	LastIdleRes uint64
	LastTime    time.Time
}

type GPU struct {
	ID          string
	SysPath     string
	DebugPath   string
	HwmonPath   string
	GTs         map[string]*GTStats
	VRAMs       []string
	LastEnergy  float64
	LastEnergyT time.Time
	mu          sync.Mutex
}

func discoverGPUs() []*GPU {
	var gpus []*GPU
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]")
	for _, cardPath := range cards {
		cardID := filepath.Base(cardPath)
		gpu := &GPU{
			ID:      cardID,
			SysPath: cardPath,
			GTs:     make(map[string]*GTStats),
		}

		// Discover DebugPath
		// card0 -> /sys/kernel/debug/dri/0
		// card1 -> /sys/kernel/debug/dri/1
		idStr := strings.TrimPrefix(cardID, "card")
		debugPath := fmt.Sprintf("/sys/kernel/debug/dri/%s", idStr)
		if _, err := os.Stat(debugPath); err == nil {
			gpu.DebugPath = debugPath
		}

		// Discover VRAMs in debugfs
		if gpu.DebugPath != "" {
			vrams, _ := filepath.Glob(filepath.Join(gpu.DebugPath, "vram*_mm"))
			gpu.VRAMs = vrams
		}

		// Discover GTs in sysfs
		// /sys/class/drm/cardX/device/tileX/gtX
		gtPaths, _ := filepath.Glob(filepath.Join(cardPath, "device/tile*/gt*"))
		for _, gtp := range gtPaths {
			// Extract tileX/gtX
			parts := strings.Split(gtp, "/")
			if len(parts) >= 2 {
				gtID := parts[len(parts)-2] + "/" + parts[len(parts)-1]
				gpu.GTs[gtID] = &GTStats{}
			}
		}

		// Discover HWMON
		hwmonPaths, _ := filepath.Glob(filepath.Join(cardPath, "device/hwmon/hwmon*"))
		if len(hwmonPaths) > 0 {
			gpu.HwmonPath = hwmonPaths[0]
		}

		gpus = append(gpus, gpu)
	}
	return gpus
}

func readUint64(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return val
}

func readFloat64(path string) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	return val
}

func collectVRAM(debugFile string) (total, used uint64) {
	file, err := os.Open(debugFile)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "size:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				total, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			}
		}
		if strings.HasPrefix(line, "usage:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				used, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			}
		}
	}
	return
}

func main() {
	promPort := flag.String("prom-port", "9101", "Prometheus metrics port")
	enableProm := flag.Bool("enable-prom", true, "Enable Prometheus /metrics endpoint")
	otlpEndpoint := flag.String("otlp-endpoint", "", "OTLP gRPC endpoint")
	debug := flag.Bool("debug", false, "Enable verbose collection logging")
	flag.Parse()

	ctx := context.Background()
	res, _ := resource.New(ctx, resource.WithAttributes(semconv.ServiceNameKey.String("xe-exporter")))

	var options []sdkmetric.Option
	options = append(options, sdkmetric.WithResource(res))

	if *enableProm {
		promExporter, _ := prometheus.New()
		options = append(options, sdkmetric.WithReader(promExporter))
	}

	if *otlpEndpoint != "" {
		otlpExporter, _ := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(*otlpEndpoint), otlpmetricgrpc.WithInsecure())
		options = append(options, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(otlpExporter, sdkmetric.WithInterval(10*time.Second))))
	}

	provider := sdkmetric.NewMeterProvider(options...)
	defer provider.Shutdown(ctx)
	otel.SetMeterProvider(provider)

	meter := provider.Meter("xe-exporter")

	// Gauges
	vramUsedGauge, _ := meter.Float64ObservableGauge("xe_gpu_vram_used_bytes", metric.WithDescription("Used VRAM in bytes"))
	vramTotalGauge, _ := meter.Float64ObservableGauge("xe_gpu_vram_total_bytes", metric.WithDescription("Total VRAM in bytes"))
	gtUsageGauge, _ := meter.Float64ObservableGauge("xe_gpu_gt_usage_percent", metric.WithDescription("GPU GT utilization percent"))
	freqActualGauge, _ := meter.Float64ObservableGauge("xe_gpu_frequency_actual_mhz", metric.WithDescription("Actual GPU frequency in MHz"))
	freqRequestedGauge, _ := meter.Float64ObservableGauge("xe_gpu_frequency_requested_mhz", metric.WithDescription("Requested GPU frequency in MHz"))
	powerGauge, _ := meter.Float64ObservableGauge("xe_gpu_power_watts", metric.WithDescription("Approximate GPU power consumption in Watts"))
	fanGauge, _ := meter.Float64ObservableGauge("xe_gpu_fan_rpm", metric.WithDescription("GPU Fan Speed in RPM"))

	gpus := discoverGPUs()
	log.Printf("Discovered %d GPUs", len(gpus))

	_, _ = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		start := time.Now()
		if *debug { log.Println("Metric collection started...") }
		for _, gpu := range gpus {
			gpu.mu.Lock()
			cardAttr := attribute.String("card", gpu.ID)

			// VRAM
			for _, vramFile := range gpu.VRAMs {
				vramID := filepath.Base(vramFile)
				total, used := collectVRAM(vramFile)
				vramAttr := attribute.NewSet(cardAttr, attribute.String("vram", vramID))
				o.ObserveFloat64(vramTotalGauge, float64(total), metric.WithAttributeSet(vramAttr))
				o.ObserveFloat64(vramUsedGauge, float64(used), metric.WithAttributeSet(vramAttr))
			}

			// GTs and Frequencies
			for gtID, stats := range gpu.GTs {
				gtPath := filepath.Join(gpu.SysPath, "device", gtID)
				gtAttr := attribute.NewSet(cardAttr, attribute.String("gt", gtID))

				// Frequency
				actual := readUint64(filepath.Join(gtPath, "freq0/act_freq"))
				requested := readUint64(filepath.Join(gtPath, "freq0/cur_freq"))
				o.ObserveFloat64(freqActualGauge, float64(actual), metric.WithAttributeSet(gtAttr))
				o.ObserveFloat64(freqRequestedGauge, float64(requested), metric.WithAttributeSet(gtAttr))

				// Usage
				idlePath := filepath.Join(gtPath, "gtidle/idle_residency_ms")
				currentIdle := readUint64(idlePath)
				now := time.Now()
				if !stats.LastTime.IsZero() {
					dt := float64(now.Sub(stats.LastTime).Milliseconds())
					didle := float64(currentIdle - stats.LastIdleRes)
					if dt > 0 {
						usage := 100.0 * (1.0 - (didle / dt))
						if usage < 0 { usage = 0 }
						if usage > 100 { usage = 100 }
						o.ObserveFloat64(gtUsageGauge, usage, metric.WithAttributeSet(gtAttr))
					}
				}
				stats.LastIdleRes = currentIdle
				stats.LastTime = now
			}

			// HWMON (Power and Fans)
			if gpu.HwmonPath != "" {
				// Power
				e1Path := filepath.Join(gpu.HwmonPath, "energy1_input")
				now := time.Now()
				e1 := readFloat64(e1Path)
				if !gpu.LastEnergyT.IsZero() {
					dt := now.Sub(gpu.LastEnergyT).Seconds()
					de := e1 - gpu.LastEnergy
					if dt > 0 && de >= 0 {
						power := (de / 1000000.0) / dt
						o.ObserveFloat64(powerGauge, power, metric.WithAttributes(cardAttr, attribute.String("type", "card")))
					}
				}
				gpu.LastEnergy = e1
				gpu.LastEnergyT = now

				// Fans
				fans, _ := filepath.Glob(filepath.Join(gpu.HwmonPath, "fan*_input"))
				for _, fanPath := range fans {
					fanID := filepath.Base(fanPath)
					rpm := readUint64(fanPath)
					o.ObserveFloat64(fanGauge, float64(rpm), metric.WithAttributes(cardAttr, attribute.String("fan", fanID)))
				}
			}
			gpu.mu.Unlock()
		}
		if *debug { log.Printf("Metric collection completed in %s", time.Since(start)) }
		return nil
	}, vramTotalGauge, vramUsedGauge, gtUsageGauge, freqActualGauge, freqRequestedGauge, powerGauge, fanGauge)

	if *enableProm {
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			log.Printf("Prometheus metrics at :%s/metrics", *promPort)
			if err := http.ListenAndServe(":"+*promPort, nil); err != nil {
				log.Fatal(err)
			}
		}()
	}

	select {}
}
