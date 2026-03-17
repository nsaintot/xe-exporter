# xe-exporter

> Tested on an Intel Arc B60.

![Grafana Dashboard](assets/usage.png)

A high-performance Prometheus and OpenTelemetry exporter for Intel Arc GPUs (Battlemage and Alchemist) using the modern Linux `xe` kernel driver and [XPU Manager](https://github.com/intel/xpumanager) via CGo.

## Features

- **OTel Native:** Built using the OpenTelemetry Go SDK for unified metrics.
- **Dual Export:** Supports both Prometheus (`/metrics` endpoint) and OTLP gRPC push.
- **Rich Telemetry:** Utilization, EU array state, per-engine breakdown, power, frequency, temperature, memory, Xe-Link throughput, and RAS error counters.
- **Container Ready:** Rootless image published to GHCR.

## Quick Start (container)

```bash
docker pull ghcr.io/nsaintot/xe-exporter:latest

docker run -d --name xe-exporter \
  --device /dev/dri \
  --cap-add SYS_ADMIN \
  -v /sys/kernel/debug:/sys/kernel/debug:ro \
  -p 9101:9101 \
  ghcr.io/nsaintot/xe-exporter:latest
```

| Flag / mount | Why it's needed |
|---|---|
| `--device /dev/dri` | Exposes the GPU render and card nodes to the container |
| `--cap-add SYS_ADMIN` | Required by XPU Manager / Level Zero Sysman for perf counters |
| `-v /sys/kernel/debug:/sys/kernel/debug:ro` | EU array & engine metrics read from debugfs |
| `-p 9101:9101` | Prometheus scrape port |

## Metrics

All metrics carry the labels `gpu_id`, `gpu_uuid`, and `gpu_name`.

### Utilization

| Metric | Description |
|---|---|
| `xe_gpu_utilization_percent` | Overall GPU utilization |
| `xe_gpu_eu_active_percent` | EU array: active (doing work) |
| `xe_gpu_eu_stall_percent` | EU array: stalled (waiting on memory/dependency) |
| `xe_gpu_eu_idle_percent` | EU array: idle (no work assigned) |
| `xe_gpu_engine_util_percent` | Per-engine utilization — extra labels: `engine_type` (`compute`, `render`, `media`, `decoder`, `encoder`, `copy`), `engine_index` |

### Power & Frequency

| Metric | Description |
|---|---|
| `xe_gpu_power_watts` | Power draw in Watts |
| `xe_gpu_frequency_gpu_mhz` | GPU core clock in MHz |
| `xe_gpu_frequency_media_mhz` | Media engine clock in MHz |

### Temperature

| Metric | Description |
|---|---|
| `xe_gpu_temperature_core_celsius` | GPU core temperature in °C |
| `xe_gpu_temperature_memory_celsius` | Memory temperature in °C |

### Memory

| Metric | Description |
|---|---|
| `xe_gpu_memory_used_mebibytes` | VRAM used in MiB |
| `xe_gpu_memory_util_percent` | VRAM utilization in percent |
| `xe_gpu_memory_bandwidth_util_percent` | Memory bandwidth utilization in percent |
| `xe_gpu_memory_read_kbytes_per_second` | Memory read throughput in kB/s |
| `xe_gpu_memory_write_kbytes_per_second` | Memory write throughput in kB/s |

### Fabric

| Metric | Description |
|---|---|
| `xe_gpu_xelink_throughput_kbytes_per_second` | Xe-Link combined throughput in kB/s |

### RAS Error Counters (monotonically increasing)

| Metric | Description |
|---|---|
| `xe_gpu_errors_reset_total` | GPU resets |
| `xe_gpu_errors_programming_total` | Programming errors |
| `xe_gpu_errors_driver_total` | Driver errors |
| `xe_gpu_errors_cache_correctable_total` | Correctable cache errors |
| `xe_gpu_errors_cache_uncorrectable_total` | Uncorrectable cache errors |
| `xe_gpu_errors_memory_correctable_total` | Correctable memory errors |
| `xe_gpu_errors_memory_uncorrectable_total` | Uncorrectable memory errors |

## Requirements

- **Linux Kernel 6.8+** with the `xe` driver loaded.
- **Intel Arc GPU** (Battlemage B-series or Alchemist A-series).
- **Go 1.26+** (build from source only).

## Configuration

| Flag | Default | Description |
|---|---|---|
| `-prom-port` | `9101` | Prometheus `/metrics` port |
| `-enable-prom` | `true` | Enable Prometheus endpoint |
| `-otlp-endpoint` | _(none)_ | OTLP gRPC endpoint (e.g. `otel-collector:4317`) |
| `-debug` | `false` | Verbose collection logging |

## Prometheus Configuration

```yaml
scrape_configs:
  - job_name: xe-exporter
    static_configs:
      - targets: ['localhost:9101']
    scrape_interval: 2s
```

## OpenTelemetry (OTLP push)

```bash
docker run -d --name xe-exporter \
  --device /dev/dri \
  --cap-add SYS_ADMIN \
  -v /sys/kernel/debug:/sys/kernel/debug:ro \
  ghcr.io/nsaintot/xe-exporter:latest \
  -enable-prom=false -otlp-endpoint otel-collector:4317
```

## Build from Source

```bash
git clone https://github.com/nsaintot/xe-exporter
cd xe-exporter
docker build -t xe-exporter .
```

## License

MIT
