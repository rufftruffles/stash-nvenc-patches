# Build patched Stash with hardware acceleration for ALL generation tasks
FROM golang:latest AS builder

ENV GOTOOLCHAIN=auto

RUN apt-get update && apt-get install -y git make nodejs npm && npm install -g yarn

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
    cp /patches/stream_segmented.go pkg/ffmpeg/

# ============================================
# Apply patches to pkg/ffmpeg/transcoder
# ============================================
RUN cp /patches/screenshot.go pkg/ffmpeg/transcoder/

# ============================================
# Apply patches to pkg/scene/generate
# ============================================
RUN cp /patches/generator.go pkg/scene/generate/ && \
    cp /patches/preview.go pkg/scene/generate/ && \
    cp /patches/sprite.go pkg/scene/generate/ && \
    cp /patches/marker_preview.go pkg/scene/generate/ && \
    cp /patches/screenshot_generate.go pkg/scene/generate/screenshot.go

# ============================================
# Apply patches to pkg/hash/videophash (phash)
# ============================================
RUN cp /patches/phash.go pkg/hash/videophash/

# ============================================
# Apply patches to internal/manager (task)
# ============================================
RUN cp /patches/task_generate_phash.go internal/manager/

# ============================================
# DEBUG: Verify patches were applied
# ============================================
RUN echo "=== Verifying patches ===" && \
    grep -q "HWCodecMP4Compatible" pkg/ffmpeg/codec_hardware.go && echo "✓ codec_hardware.go" && \
    grep -q "ExtraInputArgs" pkg/ffmpeg/transcoder/screenshot.go && echo "✓ transcoder/screenshot.go" && \
    grep -q "GetTranscodeHardwareAcceleration" pkg/scene/generate/generator.go && echo "✓ generator.go" && \
    grep -q "getPreviewVideoCodec" pkg/scene/generate/preview.go && echo "✓ preview.go" && \
    grep -q "ExtraInputArgs" pkg/scene/generate/sprite.go && echo "✓ sprite.go" && \
    grep -q "getMarkerVideoCodec" pkg/scene/generate/marker_preview.go && echo "✓ marker_preview.go" && \
    grep -q "GenerateWithConfig" pkg/hash/videophash/phash.go && echo "✓ phash.go" && \
    grep -q "GenerateWithConfig" internal/manager/task_generate_phash.go && echo "✓ task_generate_phash.go" && \
    echo "=== All patches verified ==="

# Build UI
WORKDIR /build/ui/v2.5
RUN yarn install --frozen-lockfile

WORKDIR /build
RUN make generate

WORKDIR /build/ui/v2.5
RUN yarn build

# Build backend
WORKDIR /build
RUN make build

# Final image
FROM ghcr.io/feederbox826/stash-s6:hwaccel-develop
COPY --from=builder /build/stash /app/stash
RUN chmod +x /app/stash
