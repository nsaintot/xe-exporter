## Running xe-exporter in a container

Build the image:

```bash
docker build -t xe-exporter .
```

Run with access to the GPUs (adjust `--device` as needed for your host):

```bash
docker run --rm \
  --device /dev/dri \
  -p 9101:9101 \
  xe-exporter \
  -prom-port 9101
```

To also send OTLP metrics:

```bash
docker run --rm \
  --device /dev/dri \
  -p 9101:9101 \
  xe-exporter \
  -prom-port 9101 \
  -otlp-endpoint my-otel-collector:4317
```

