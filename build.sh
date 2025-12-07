#!/bin/bash
# Build script for patched Stash with hardware acceleration for generation tasks

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="stash-hwaccel-gen"
IMAGE_TAG="latest"

echo "=========================================="
echo "Stash Hardware Accelerated Generation Build"
echo "=========================================="

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed"
    exit 1
fi

# Create build context
BUILD_DIR="${SCRIPT_DIR}/build-context"
mkdir -p "${BUILD_DIR}/patches"

echo "[1/4] Preparing patch files..."

# Copy patch files to build context
cp "${SCRIPT_DIR}/patches/"*.go "${BUILD_DIR}/patches/" 2>/dev/null || {
    echo "Error: Patch files not found in ${SCRIPT_DIR}/patches/"
    echo "Expected files: codec_hardware.go, stream_transcode.go, stream_segmented.go, generator.go, preview.go"
    exit 1
}

# Copy Dockerfile
cp "${SCRIPT_DIR}/Dockerfile" "${BUILD_DIR}/Dockerfile"

echo "[2/4] Building Docker image..."
echo "This may take several minutes on first build..."

cd "${BUILD_DIR}"
docker build \
    --build-arg STASH_VERSION=develop \
    -t "${IMAGE_NAME}:${IMAGE_TAG}" \
    -f Dockerfile \
    .

echo "[3/4] Verifying build..."
# Quick verification that the image works
docker run --rm "${IMAGE_NAME}:${IMAGE_TAG}" /app/stash --help > /dev/null 2>&1 && \
    echo "✓ Binary verification passed" || \
    echo "⚠ Warning: Could not verify binary"

echo "[4/4] Cleanup..."
rm -rf "${BUILD_DIR}"

echo ""
echo "=========================================="
echo "Build complete!"
echo "=========================================="
echo ""
echo "Image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo ""
echo "To use, update your docker-compose.yml:"
echo ""
echo "  services:"
echo "    stash:"
echo "      image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo "      # ... rest of your config"
echo ""
echo "Then run: docker-compose up -d"
