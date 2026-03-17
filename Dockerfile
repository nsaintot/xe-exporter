# ── Stage 1: build ────────────────────────────────────────────────────────────
# xpumanager headers + libxpum.so are only needed at compile time.
# We extract them from the upstream deb rather than doing a full install
# (the deb hard-depends on Intel GPU driver packages absent in base repos).
ARG XPUM_DEB_URL=https://github.com/intel/xpumanager/releases/download/v1.3.6/xpumanager_1.3.6_20260206.143628.1004f6cb.u24.04_amd64.deb

FROM golang:1.26-bookworm AS builder

ARG XPUM_DEB_URL
WORKDIR /src

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    curl -fsSL -o /tmp/xpumanager.deb "${XPUM_DEB_URL}" && \
    dpkg-deb --extract /tmp/xpumanager.deb /tmp/xpum && \
    cp -P /tmp/xpum/usr/lib/x86_64-linux-gnu/libxpum.so* /usr/lib/x86_64-linux-gnu/ && \
    cp    /tmp/xpum/usr/include/xpum*.h /usr/include/ && \
    rm -rf /tmp/xpum /tmp/xpumanager.deb && \
    ldconfig

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 \
    CGO_CFLAGS="-I/usr/include" \
    CGO_LDFLAGS="-L/usr/lib/x86_64-linux-gnu -lxpum -Wl,-rpath,/usr/lib/x86_64-linux-gnu -Wl,--allow-shlib-undefined" \
    GOOS=linux GOARCH=amd64 \
    go build -o /out/xe-exporter ./cmd/xe-exporter

# ── Stage 2: runtime ──────────────────────────────────────────────────────────
# Kobuk PPA ships xpumanager + igsc 0.9.5+ as a matched stack, avoiding the
# version skew in the upstream Intel GPU repo (which only has igsc 0.8.x).
# https://launchpad.net/~kobuk-team/+archive/ubuntu/intel-graphics
FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends software-properties-common && \
    add-apt-repository -y ppa:kobuk-team/intel-graphics && \
    apt-get update && \
    apt-get install -y --no-install-recommends libxpum1 libze-intel-gpu1 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/xe-exporter /usr/local/bin/xe-exporter

# ZES_ENABLE_SYSMAN is required for Level Zero Sysman to enumerate Intel GPUs.
ENV ZES_ENABLE_SYSMAN=1

EXPOSE 9101

ENTRYPOINT ["/usr/local/bin/xe-exporter"]
