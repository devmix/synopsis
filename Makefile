.PHONY: build build-all clean test lint vet \
  build-linux-amd64 build-linux-arm64 \
  build-windows-amd64 \
  build-macos-amd64 build-macos-arm64 \
  run sync rebuild test-coverage

BINARY_NAME := synopsis
OUTPUT_DIR := bin
CONFIG := configs/config.default.yaml
DB_PATH := data/knowledge.db

# SQLite feature flags; without -DSQLITE_ENABLE_FTS5 go-sqlite3 silently drops FTS5.
SQLITE_DEFS := -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_VEC

# Native (gcc) builds/tests must link libm explicitly: FTS5 bm25 scoring calls
# log(), which lives in a separate libm.so on glibc where math is not merged
# into libc. Harmless where it is, so set it on every native target.
SQLITE_LDFLAGS := -lm

# Default target builds for the current platform.
build:
	CGO_ENABLED=1 CGO_CFLAGS="$(SQLITE_DEFS) -O2" CGO_LDFLAGS="$(SQLITE_LDFLAGS)" go build -o $(OUTPUT_DIR)/$(BINARY_NAME) ./cmd/app/

# --- Development targets ---

run: build
	./$(OUTPUT_DIR)/$(BINARY_NAME) --config $(CONFIG) serve

sync: build
	./$(OUTPUT_DIR)/$(BINARY_NAME) --config $(CONFIG) sync

rebuild: build
	./$(OUTPUT_DIR)/$(BINARY_NAME) --config $(CONFIG) sync --rebuild

# --- Testing targets ---

test:
	CGO_CFLAGS="$(SQLITE_DEFS)" CGO_LDFLAGS="$(SQLITE_LDFLAGS)" go test -race ./...

test-coverage:
	CGO_CFLAGS="$(SQLITE_DEFS)" CGO_LDFLAGS="$(SQLITE_LDFLAGS)" go test -coverprofile=coverage.out -race ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

vet:
	go vet ./...

# --- Cross-compilation targets using Zig as C toolchain ---

# Resolve go-sqlite3 directory and create sqlite3.h symlink for CGO includes.
SQLITE_INCLUDE := $(shell TMPDIR=$$(mktemp -d) && \
	GO_SQLITE3_DIR=$$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3) && \
	ln -sf "$$GO_SQLITE3_DIR/sqlite3-binding.h" "$$TMPDIR/sqlite3.h" && \
	echo "$$TMPDIR")

build-linux-amd64:
	CGO_ENABLED=1 \
	CC="zig cc -target x86_64-linux-gnu" \
	CXX="zig c++ -target x86_64-linux-gnu" \
	CGO_CFLAGS="$(SQLITE_DEFS) -I$(SQLITE_INCLUDE) -fno-sanitize=undefined" \
	GOOS=linux GOARCH=amd64 \
	go build -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/app/

build-linux-arm64:
	CGO_ENABLED=1 \
	CC="zig cc -target aarch64-linux-gnu" \
	CXX="zig c++ -target aarch64-linux-gnu" \
	CGO_CFLAGS="$(SQLITE_DEFS) -I$(SQLITE_INCLUDE) -fno-sanitize=undefined" \
	GOOS=linux GOARCH=arm64 \
	go build -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/app/

build-windows-amd64:
	CGO_ENABLED=1 \
	CC="zig cc -target x86_64-windows" \
	CXX="zig c++ -target x86_64-windows" \
	CGO_CFLAGS="$(SQLITE_DEFS) -I$(SQLITE_INCLUDE) -fno-sanitize=undefined" \
	GOOS=windows GOARCH=amd64 \
	go build -o $(OUTPUT_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/app/

# macOS has no separate libresolv/dl/pthread (they're in libSystem). Point Zig
# at our parseable !tapi-tbd stubs (scripts/darwin-stubs) first, then its
# bundled SDK for libSystem.tbd; dynamic_lookup defers unresolved symbols.
# -ldflags=-w: without DWARF, Go skips its dsymutil/strip step (host GNU strip
# cannot process Mach-O).
ZIG_LIB_DIR := $(shell zig env | sed -n 's/.*\.lib_dir = "\([^"]*\)".*/\1/p')
CGO_LDFLAGS_DARWIN := CGO_LDFLAGS="-Wl,-undefined,dynamic_lookup -L$(CURDIR)/scripts/darwin-stubs -F$(CURDIR)/scripts/darwin-stubs -L$(ZIG_LIB_DIR)/libc/darwin -F$(ZIG_LIB_DIR)/libc/darwin"
LDFLAGS_DARWIN := -ldflags=-w

build-macos-amd64:
	CGO_ENABLED=1 \
	CC="zig cc -target x86_64-macos" \
	CXX="zig c++ -target x86_64-macos" \
	CGO_CFLAGS="$(SQLITE_DEFS) -I$(SQLITE_INCLUDE) -fno-sanitize=undefined" \
	$(CGO_LDFLAGS_DARWIN) \
	GOOS=darwin GOARCH=amd64 \
	go build $(LDFLAGS_DARWIN) -o $(OUTPUT_DIR)/$(BINARY_NAME)-macos-amd64 ./cmd/app/

build-macos-arm64:
	CGO_ENABLED=1 \
	CC="zig cc -target aarch64-macos" \
	CXX="zig c++ -target aarch64-macos" \
	CGO_CFLAGS="$(SQLITE_DEFS) -I$(SQLITE_INCLUDE) -fno-sanitize=undefined" \
	$(CGO_LDFLAGS_DARWIN) \
	GOOS=darwin GOARCH=arm64 \
	go build $(LDFLAGS_DARWIN) -o $(OUTPUT_DIR)/$(BINARY_NAME)-macos-arm64 ./cmd/app/

# Build all platforms (sqlite-vec compiled into binary via CGO).
build-all: build-linux-amd64 build-linux-arm64 build-windows-amd64 build-macos-amd64 build-macos-arm64
	@echo "All platforms built into $(OUTPUT_DIR)/"

clean:
	rm -rf $(OUTPUT_DIR) coverage.out coverage.html
