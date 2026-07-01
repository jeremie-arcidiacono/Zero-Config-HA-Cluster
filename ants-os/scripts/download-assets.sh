#!/usr/bin/env bash
# =============================================================================
# Run once to download all required binaries before the Packer build.
# =============================================================================
set -euo pipefail

K3S_VERSION="${K3S_VERSION:-v1.36.1+k3s1}"
K3S_BASE_URL="https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}"

ASSETS_DIR="$(dirname "$0")/../assets"

mkdir -p "$ASSETS_DIR"
cd "$ASSETS_DIR"

echo "=====> Downloading k3s ARM64 binary (version $K3S_VERSION)..."
curl -fsSL -o k3s-arm64 "${K3S_BASE_URL}/k3s-arm64"

echo "=====> Downloading k3s air-gap images ARM64..."
curl -fsSL -o k3s-airgap-images-arm64.tar.zst "${K3S_BASE_URL}/k3s-airgap-images-arm64.tar.zst"

echo "=====> Downloading the k3s install script..."
curl -fsSL -o install-k3s.sh https://get.k3s.io

echo "=====> Verifying official k3s checksums..."
curl -fsSL -o k3s-arm64.sha256 "${K3S_BASE_URL}/sha256sum-arm64.txt"
# Verify only the k3s binary
grep "k3s-arm64$" k3s-arm64.sha256 | sha256sum --check
rm k3s-arm64.sha256

echo ""
echo "=====> Assets ready in $ASSETS_DIR :"
ls -lh "$ASSETS_DIR"
