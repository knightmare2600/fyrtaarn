APP     := fyrtaarn
DIST    := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(DATE)

.PHONY: build run tidy clean cross \
        linux-amd64 linux-arm64 linux-ppc64 linux-sparc64 linux-mipsle linux-mips64le \
        windows-amd64 windows-arm64

# Native build (current platform)
build:
	mkdir -p $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP) ./cmd/fyrtaarn

run:
	go run ./cmd/fyrtaarn

tidy:
	go mod tidy

clean:
	rm -rf $(DIST)

# ── Linux targets ─────────────────────────────────────────────────────────────

linux-amd64:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-amd64 ./cmd/fyrtaarn

linux-arm64:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-arm64 ./cmd/fyrtaarn

# PowerPC 64-bit big-endian — covers G3/G4/G5 PowerPC Linux installs
linux-ppc64:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=ppc64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-ppc64 ./cmd/fyrtaarn

# SPARC 64-bit — Sun/Oracle UltraSPARC hardware
linux-sparc64:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=sparc64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-sparc64 ./cmd/fyrtaarn

# MIPS little-endian 32-bit — softfloat for maximum compatibility (no FPU required)
linux-mipsle:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-mipsle ./cmd/fyrtaarn

# MIPS little-endian 64-bit — softfloat for maximum compatibility
linux-mips64le:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=mips64le GOMIPS64=softfloat CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-mips64le ./cmd/fyrtaarn

# ── Windows targets ───────────────────────────────────────────────────────────

windows-amd64:
	mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-windows-amd64.exe ./cmd/fyrtaarn

windows-arm64:
	mkdir -p $(DIST)
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-windows-arm64.exe ./cmd/fyrtaarn

# ── All cross-compilation targets ─────────────────────────────────────────────

cross: linux-amd64 linux-arm64 linux-ppc64 linux-sparc64 linux-mipsle linux-mips64le windows-amd64 windows-arm64
