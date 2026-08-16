#!/usr/bin/env bash
# build.sh — Cross-compilation script using Zig as C toolchain.
#
# Usage:
#   ./scripts/build.sh [platform]
#
# Platforms:
#   linux-amd64, linux-arm64, windows-amd64, macos-amd64, macos-arm64
#   all (builds every platform)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY_NAME="synopsis"
OUTPUT_DIR="$PROJECT_ROOT/dist"

# Zig's bundled minimal macOS SDK. libSystem.tbd parses cleanly, but the
# libresolv/framework stubs use the legacy !libstubs format that Zig 0.16
# rejects (NotLibStub) — so parseable replacements live in darwin-stubs/.
ZIG_LIB_DIR="$(zig env | sed -n 's/.*\.lib_dir = "\([^"]*\)".*/\1/p')"
DARWIN_SDK_DIR="$ZIG_LIB_DIR/libc/darwin"
DARWIN_STUBS_DIR="$SCRIPT_DIR/darwin-stubs"

# Zig target triples for cross-compilation.
declare -A ZIG_TARGETS=(
  [linux-amd64]="x86_64-linux-gnu"
  [linux-arm64]="aarch64-linux-gnu"
  [windows-amd64]="x86_64-windows"
  [macos-amd64]="x86_64-macos"
  [macos-arm64]="aarch64-macos"
)

# Go environment variables per platform.
declare -A GOOS_MAP=(
  [linux-amd64]="linux"
  [linux-arm64]="linux"
  [windows-amd64]="windows"
  [macos-amd64]="darwin"
  [macos-arm64]="darwin"
)

declare -A GOARCH_MAP=(
  [linux-amd64]="amd64"
  [linux-arm64]="arm64"
  [windows-amd64]="amd64"
  [macos-amd64]="amd64"
  [macos-arm64]="arm64"
)

# Resolve the go-sqlite3 module directory and create a sqlite3.h symlink
# so that sqlite-vec-go-bindings can find its #include "sqlite3.h".
setup_sqlite_include() {
  local SQLITE_INCLUDE_DIR=$(mktemp -d)
  local GO_SQLITE3_DIR
  GO_SQLITE3_DIR="$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)"
  ln -sf "$GO_SQLITE3_DIR/sqlite3-binding.h" "$SQLITE_INCLUDE_DIR/sqlite3.h"
  echo "$SQLITE_INCLUDE_DIR"
}

build_platform() {
  local platform="$1"
  local zig_target="${ZIG_TARGETS[$platform]}"
  local goos="${GOOS_MAP[$platform]}"
  local goarch="${GOARCH_MAP[$platform]}"
  local ext=""
  if [ "$goos" = "windows" ]; then
    ext=".exe"
  fi

  local out_name="${BINARY_NAME}-${platform}${ext}"
  mkdir -p "$OUTPUT_DIR"

  local sqlite_include
  sqlite_include="$(setup_sqlite_include)"
  trap "rm -rf '$sqlite_include'" RETURN

  # macOS has no separate libresolv/dl/pthread (they're in libSystem). Point
  # Zig at our parseable !tapi-tbd stubs first, then its bundled SDK for
  # libSystem.tbd; dynamic_lookup defers any remaining unresolved symbols.
  # -ldflags=-w: without DWARF, Go skips its dsymutil/strip step (which would
  # invoke the host GNU strip and choke on Mach-O).
  local cgo_ldflags=""
  local ldflags=""
  if [ "$goos" = "darwin" ]; then
    cgo_ldflags="-Wl,-undefined,dynamic_lookup -L${DARWIN_STUBS_DIR} -F${DARWIN_STUBS_DIR} -L${DARWIN_SDK_DIR} -F${DARWIN_SDK_DIR}"
    ldflags="-w"
  fi

  echo "==> Building ${platform} (zig target: ${zig_target})"

  CGO_ENABLED=1 \
    CC="zig cc -target ${zig_target}" \
    CXX="zig c++ -target ${zig_target}" \
    CGO_CFLAGS="-I${sqlite_include} -fno-sanitize=undefined" \
    CGO_LDFLAGS="$cgo_ldflags" \
    GOOS="$goos" \
    GOARCH="$goarch" \
    go build -ldflags="$ldflags" -o "$OUTPUT_DIR/$out_name" ./cmd/app/

  echo "    -> $OUTPUT_DIR/$out_name"
}

build_all() {
  for platform in linux-amd64 linux-arm64 windows-amd64 macos-amd64 macos-arm64; do
    build_platform "$platform"
  done
}

# --- main ---
PLATFORM="${1:-}"

if [ -z "$PLATFORM" ]; then
  echo "Usage: $0 <platform>"
  echo ""
  echo "Platforms:"
  for p in "${!ZIG_TARGETS[@]}"; do
    echo "  $p"
  done
  echo "  all"
  exit 1
fi

if [ "$PLATFORM" = "all" ]; then
  build_all
else
  if [ -z "${ZIG_TARGETS[$PLATFORM]+x}" ]; then
    echo "Unknown platform: $PLATFORM" >&2
    exit 1
  fi
  build_platform "$PLATFORM"
fi
