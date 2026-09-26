# Build patched Stash with hardware acceleration for ALL generation tasks
FROM golang:1.25-trixie AS builder
ENV GOTOOLCHAIN=auto
RUN apt-get update && apt-get install -y git make nodejs npm curl && corepack enable
WORKDIR /build
ARG STASH_VERSION=develop
RUN git clone --depth 1 --branch ${STASH_VERSION} https://github.com/stashapp/stash.git .
# Copy all patch files
COPY patches/ /patches/
# ============================================
# Apply patches to pkg/ffmpeg (core ffmpeg)
# ============================================
RUN cp /patches/codec_hardware.go pkg/ffmpeg/ && \
    cp /patches/stream_transcode.go pkg/ffmpeg/ && \
    cp /patches/stream_segmented.go pkg/ffmpeg/ && \
    cp /patches/hw_keepwarm.go /patches/hw_keepwarm_linux.go /patches/hw_keepwarm_other.go pkg/ffmpeg/
# ============================================
# Apply patches to pkg/scene/generate
# ============================================
# Sprites, phash and cover/marker screenshots are deliberately NOT patched: a
# single-frame grab is ~2x faster on the CPU than paying CUDA start-up per ffmpeg
# process, so those use stock Stash. See CLAUDE.md.
RUN cp /patches/generator.go pkg/scene/generate/ && \
    cp /patches/preview.go pkg/scene/generate/ && \
    cp /patches/marker_preview.go pkg/scene/generate/
# ============================================
# DEBUG: Verify patches were applied
# ============================================
RUN echo "=== Verifying patches ===" && \
    grep -q "HWCodecMP4Compatible" pkg/ffmpeg/codec_hardware.go && echo "✓ codec_hardware.go" && \
    grep -q "HoldHWDevice" pkg/ffmpeg/hw_keepwarm.go && echo "✓ hw_keepwarm.go" && \
    grep -q "GetTranscodeHardwareAcceleration" pkg/scene/generate/generator.go && echo "✓ generator.go" && \
    grep -q "previewVideoChunkHW" pkg/scene/generate/preview.go && echo "✓ preview.go" && \
    grep -q "markerPreviewVideoArgs" pkg/scene/generate/marker_preview.go && echo "✓ marker_preview.go" && \
    echo "=== All patches verified ==="
# Build UI
WORKDIR /build/ui/v2.5
RUN pnpm install --frozen-lockfile
WORKDIR /build
RUN make generate
WORKDIR /build/ui/v2.5
RUN npm run build
# Build backend - auto-detect latest release version from GitHub
WORKDIR /build
# NOTE: the version string must stay a bare upstream release tag such as v0.31.1.
# The Android TV client parses it and refuses to connect to anything else
# ("Unsupported server version"), so do not stamp build metadata in here — image
# tags and OCI labels carry that instead.
RUN LATEST_VERSION=$(curl -s https://api.github.com/repos/stashapp/stash/releases/latest | grep '"tag_name"' | cut -d'"' -f4) && \
    echo "Building with version: ${LATEST_VERSION}" && \
    go build -v -tags "sqlite_stat4 sqlite_math_functions" \
    -ldflags "-X 'github.com/stashapp/stash/internal/build.version=${LATEST_VERSION}' \
              -X 'github.com/stashapp/stash/internal/build.buildstamp=$(date +%Y-%m-%d)' \
              -X 'github.com/stashapp/stash/internal/build.githash=$(git rev-parse --short HEAD)'" \
    -o stash ./cmd/stash
# Final image
FROM ghcr.io/feederbox826/stash-s6:hwaccel-develop
COPY --from=builder /build/stash /app/stash
RUN chmod +x /app/stash
