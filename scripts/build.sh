#!/bin/bash
set -eufo pipefail

RED='\E[1;31m'
YELLOW='\E[1;33m'
RESET='\033[0m'

PROJECT_DIR="$(cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$PROJECT_DIR"

trap cleanup EXIT
cleanup() {
	if [ "$?" -ne 0 ]; then
		echo -e "${RED}BUILDING FAILED${RESET}"
	fi
}

# GOOS/GOARCH pairs for the common platforms lsgo targets. Kept as a flat
# array (rather than nested loops over every GOOS x GOARCH) since not every
# combination is meaningful (e.g. no windows/mips64) and this keeps that
# curation explicit.
#
# NB: 32-bit Linux (linux/386, linux/arm) is deliberately excluded
PLATFORMS=(
	"linux/amd64"
	"linux/arm64"
	"darwin/amd64"
	"darwin/arm64"
	"windows/amd64"
	"windows/arm64"
	"freebsd/amd64"
)

rm -rf dist
mkdir -p dist

for platform in "${PLATFORMS[@]}"; do
	GOOS="${platform%/*}"
	GOARCH="${platform#*/}"

	ext=""
	if [ "$GOOS" = "windows" ]; then
		ext=".exe"
	fi

	out="dist/polyschema_${GOOS}_${GOARCH}${ext}"

	echo "Building ${out}..."
	CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -o "$out" ./cmd/polyschema/main.go
done

echo -e "${YELLOW}Build completed for ${#PLATFORMS[@]} platforms in dist/${RESET}"
