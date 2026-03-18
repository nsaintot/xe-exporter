package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"xe-exporter/internal/gpu/xpumanager"
	"xe-exporter/internal/metrics"
)

func main() {
	// ── Flags ──────────────────────────────────────────────────────────────────

	// Internal flag — used by the subprocess spawned on each collect-interval tick.
	// Do not set this manually; use -daemon-collector-mode to control collection behaviour.
	collectOnce := flag.Bool("collect-once", false,
		"(Internal) Run one collection cycle, write JSON to stdout, and exit.")

	promPort     := flag.String("prom-port", "9101", "Prometheus metrics port")
	enableProm   := flag.Bool("enable-prom", true, "Enable Prometheus /metrics endpoint")
	otlpEndpoint := flag.String("otlp-endpoint", "", "OTLP gRPC endpoint (e.g. localhost:4317)")
	debug        := flag.Bool("debug", false, "Enable verbose collection logging")
	collectInterval := flag.Duration("collect-interval", 10*time.Second,
		"How often to collect a fresh GPU snapshot.\n"+
			"Keep this ≥ your Prometheus scrape_interval.\n"+
			"In the default mode the GPU idles completely between cycles.")
	daemonCollectorMode := flag.Bool("daemon-collector-mode", false,
		"Run hardware-counter collection in-process instead of spawning a subprocess.\n"+
			"By default xe-exporter spawns a short-lived subprocess on each collect-interval\n"+
			"tick so that all Level Zero handles are released by the OS on exit, allowing\n"+
			"the GPU to power-gate between cycles.\n"+
			"Enable this flag to revert to the single-process model (legacy behaviour).\n"+
			"Warning: in daemon mode L0 handles remain open and the GPU thermal floor\n"+
			"rises by ~12 °C regardless of collect-interval.")
	flag.Parse()

	// ── Subprocess entry point ─────────────────────────────────────────────────
	// When -collect-once is set this is a short-lived worker spawned by the
	// parent server.  It runs one full init→baseline→warmup→collect→shutdown
	// cycle, writes the result as JSON to stdout, and exits.
	if *collectOnce {
		if err := xpumanager.RunCollectOnce(*debug); err != nil {
			log.Fatalf("xe-exporter: collect-once failed: %v", err)
		}
		os.Exit(0)
	}

	// ── Long-lived server mode ─────────────────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── OTel resource ─────────────────────────────────────────────────────────
	res, _ := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String("xe-exporter")),
	)

	// ── Metric readers ────────────────────────────────────────────────────────
	var options []sdkmetric.Option
	options = append(options, sdkmetric.WithResource(res))

	if *enableProm {
		promExporter, err := prometheus.New()
		if err != nil {
			log.Fatalf("failed to create Prometheus exporter: %v", err)
		}
		options = append(options, sdkmetric.WithReader(promExporter))
	}

	if *otlpEndpoint != "" {
		otlpExporter, err := otlpmetricgrpc.New(
			ctx,
			otlpmetricgrpc.WithEndpoint(*otlpEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			log.Fatalf("failed to create OTLP exporter: %v", err)
		}
		options = append(options, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(otlpExporter,
				sdkmetric.WithInterval(10*time.Second)),
		))
	}

	// ── Meter provider ────────────────────────────────────────────────────────
	meterProvider := sdkmetric.NewMeterProvider(options...)
	defer func() {
		shutCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
		defer sc()
		if err := meterProvider.Shutdown(shutCtx); err != nil {
			log.Printf("meter provider shutdown: %v", err)
		}
	}()
	otel.SetMeterProvider(meterProvider)

	var meter metric.Meter = meterProvider.Meter("xe-exporter")

	// ── GPU provider ──────────────────────────────────────────────────────────
	gpuProvider, err := xpumanager.NewProvider(ctx, *debug, *collectInterval, *daemonCollectorMode)
	if err != nil {
		log.Fatalf("failed to initialise xpumanager: %v", err)
	}
	defer gpuProvider.Close()

	// ── Metrics collector ─────────────────────────────────────────────────────
	collector, err := metrics.NewCollector(meter, gpuProvider, *debug)
	if err != nil {
		log.Fatalf("failed to create metrics collector: %v", err)
	}
	if err := collector.Register(ctx); err != nil {
		log.Fatalf("failed to register metrics collector: %v", err)
	}

	// ── HTTP server (Prometheus) ──────────────────────────────────────────────
	if *enableProm {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := &http.Server{
			Addr:    ":" + *promPort,
			Handler: mux,
		}
		go func() {
			log.Printf("Prometheus metrics at http://0.0.0.0:%s/metrics", *promPort)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}()
		defer func() {
			shutCtx, sc := context.WithTimeout(context.Background(), 3*time.Second)
			defer sc()
			_ = srv.Shutdown(shutCtx)
		}()
	}

	if *daemonCollectorMode {
		log.Printf("xe-exporter: collect-interval=%s mode=daemon (L0 handles held in-process)", *collectInterval)
	} else {
		log.Printf("xe-exporter: collect-interval=%s mode=subprocess (GPU idles between cycles)", *collectInterval)
	}
	<-ctx.Done()
	log.Println("xe-exporter: shutting down")
}
