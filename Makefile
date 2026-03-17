BIN_NAME = xe-exporter

# ── xpumanager library paths ──────────────────────────────────────────────────
# Default install location from the xpumanager deb package (Ubuntu/Debian).
# The deb installs headers to /usr/include and the library to the multiarch
# path /usr/lib/x86_64-linux-gnu/.  Override if your layout differs:
#   make XPUM_INCLUDE=/opt/xpumanager/include XPUM_LIB=/opt/xpumanager/lib
XPUM_INCLUDE ?= /usr/include
XPUM_LIB     ?= /usr/lib/x86_64-linux-gnu

# CGo environment – always enabled; inject include/lib paths.
export CGO_ENABLED  = 1
export CGO_CFLAGS  ?= -I$(XPUM_INCLUDE)
export CGO_LDFLAGS ?= -L$(XPUM_LIB) -lxpum -Wl,-rpath,$(XPUM_LIB) -Wl,--allow-shlib-undefined

# ── Targets ───────────────────────────────────────────────────────────────────
.PHONY: all build clean

all: build

build:
	go build -o $(BIN_NAME) ./cmd/xe-exporter

# Verify the code compiles (fast, no linking)
vet:
	go vet ./...

clean:
	rm -f $(BIN_NAME)
