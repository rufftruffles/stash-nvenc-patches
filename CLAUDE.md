# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repo maintains **patched Go source files** for [Stash](https://github.com/stashapp/stash) that add NVIDIA GPU hardware acceleration (CUDA decoding + NVENC encoding) to all media generation tasks. The patches are applied via full-file replacement during a Docker build — there are no `.patch` diff files, just complete Go source files in `patches/`.

## Repository Structure

```
patches/               # Patched Go files (copied into Stash source tree during Docker build)
Dockerfile             # Multi-stage build: clone Stash → copy patches → build UI → build Go binary
build.sh               # Local build helper (creates temp build-context, runs docker build)
docker-compose.yml     # Example deployment with NVIDIA runtime + GPU config
.github/workflows/     # CI: builds and pushes to ghcr.io on push/weekly schedule
```

## Build Commands

**Docker build (primary):**
```bash
docker build -t stash-hwaccel-gen:latest .
```

**Clean build (no cache):**
```bash
docker builder prune -af && docker build --no-cache -t stash-hwaccel-gen:latest .
```

**Local build script:**
```bash
./build.sh
```

There are no local Go builds, tests, or linting — all compilation happens inside the Docker multi-stage build against upstream Stash source.

## How Patches Work

The Dockerfile clones Stash `develop` branch, then **replaces** files at these paths:

| Patch file | Replaces in Stash |
|---|---|
| `codec_hardware.go` | `pkg/ffmpeg/codec_hardware.go` |
| `stream_transcode.go` | `pkg/ffmpeg/stream_transcode.go` |
| `stream_segmented.go` | `pkg/ffmpeg/stream_segmented.go` |
| `hw_keepwarm.go` | `pkg/ffmpeg/hw_keepwarm.go` (new file, no upstream counterpart) |
| `hw_keepwarm_linux.go` | `pkg/ffmpeg/hw_keepwarm_linux.go` (new file) |
| `hw_keepwarm_other.go` | `pkg/ffmpeg/hw_keepwarm_other.go` (new file) |
| `generator.go` | `pkg/scene/generate/generator.go` |
| `preview.go` | `pkg/scene/generate/preview.go` |
| `marker_preview.go` | `pkg/scene/generate/marker_preview.go` |
| `sprite.go` | `pkg/scene/generate/sprite.go` |

The Dockerfile verifies patches with `grep -q` checks for key symbols after copying.

## Key Architecture Concepts

**Full GPU preview/marker videos:** `codec_hardware.go` exports `HWCodecMP4Compatible()`, which returns the best available hardware codec. With NVENC, `preview.go` and `marker_preview.go` first try a full GPU pipeline (`-hwaccel cuda -hwaccel_output_format cuda`, `scale_cuda=...:format=yuv420p`, `h264_nvenc`). If ffmpeg exits with an error, they retry with CPU decoding plus `hwupload_cuda` and NVENC, and skip the GPU attempt for the rest of that video (`previewVideoChunkHW`). NVDEC rejects 10-bit and 4:2:2 h264, which is what the fallback is for; do not remove it, because the image is public.

**CUDA keep-warm:** without GPU persistence mode on the host, the NVIDIA driver reloads GPU firmware whenever no process holds a context, adding ~1.7-2 s to every ffmpeg run (a preview segment goes from ~0.47 s to ~2.5 s). `FFMpeg.HoldHWDevice` (`hw_keepwarm.go`) starts one idle `ffmpeg -init_hw_device cuda -re -f lavfi -i nullsrc=s=16x16:r=1 -f null -` while preview or marker videos are generating, and kills it 60 s after the last job. It is NVENC-only, disables itself if the holder exits within 2 s (no usable CUDA), and uses `Pdeathsig` on Linux so it dies with Stash. Keep the three `hw_keepwarm*.go` files together: the `_linux`/`_other` pair provides `setParentDeathSignal` per platform.

**Keyframe-only sprite grabs:** `sprite.go` adds `-skip_frame nokey -noaccurate_seek` before `-ss` (`keyframeSeekArgs`), so each sprite thumbnail is the nearest keyframe at or before its timestamp and only that frame is decoded. Stock ffmpeg decodes forward to the exact timestamp, which on 4K sources with 2-4 s keyframe intervals costs 2.5-4 CPU-seconds per frame. Measured with 4 workers: 4K 4 s GOP 531 → 61 ms per grab, 4K 2 s 193 → 57 ms, 1080p 3.6 s 111 → 31 ms. `-noaccurate_seek` alone does NOT help (ffmpeg still decodes every frame); both flags are needed. If the keyframe grab fails it retries the stock exact grab, then upstream's slow-seek grab. Never apply this to phash: it would change the hash.

**Single-frame tasks otherwise stay on the CPU:** phash, cover and marker screenshots, and marker WebP previews are *not* patched, and sprites do not use CUDA. Each ffmpeg process that uses CUDA pays ~250 ms of context set-up, which is more than decoding one frame on the CPU. Measured on the RTX A2000 with 4 workers at once: ~100 ms per frame on the CPU vs ~210 ms with `-hwaccel cuda`. CPU and CUDA decoding give byte-identical frames, so phash values are unaffected. Do not re-add `-hwaccel cuda` to these paths without re-benchmarking.

**Config interface:** `FFMpegConfig` (in `generator.go`) is the central interface patches use to check hardware acceleration state. It provides `GetTranscodeHardwareAcceleration() bool`.

## When Updating Patches

Patches must stay compatible with upstream Stash `develop` branch. When upstream changes a patched file:

1. Fetch the new upstream version of the changed file
2. Re-apply **only** the hardware acceleration modifications on top of it — do not carry the
   old upstream logic back in. A stale patch that reintroduces removed behaviour (an old
   hardcoded constant, a dropped parameter) silently overrides new upstream features.
3. Verify the compile locally before pushing (see below)
4. Check the patch verification step passes (the `grep -q` checks in Dockerfile)

### Verifying a patch change without Docker

The GitHub Actions build takes ~5 minutes, so do not use it as the first compile check:

```bash
git clone --depth 1 --branch develop https://github.com/stashapp/stash.git && cd stash
# copy the 11 patch files per the table above
mkdir -p ui/v2.5/build && touch ui/v2.5/build/index.html   # satisfies the go:embed in ui/ui.go
go build ./internal/manager/... ./pkg/scene/generate/... ./pkg/ffmpeg/... ./pkg/hash/videophash/...
go vet ./pkg/ffmpeg/... ./pkg/scene/generate/... ./internal/manager/...
```

Building `./internal/api/...` fails without `make generate` (gqlgen). That is expected, not a
regression.

## Version Stamping and Image Tags

The binary's version string must stay a **bare upstream release tag** such as `v0.31.1`. The
Android TV client parses it as plain semver and rejects anything else with "Unsupported
server version", so the Dockerfile looks up the latest upstream release from the GitHub API
rather than stamping build metadata. Do not add a build-arg that puts a richer string in
there — it has already broken the TV once.

Build metadata lives in image tags and OCI labels instead. Each CI build publishes, all at
the same digest:

- `latest`
- `<upstream release>-develop.<date>.<upstream short sha>.<our short sha>` (immutable)
- `develop-<upstream short sha>`

## Go Toolchain Pin

The builder stage is pinned to `golang:1.25-trixie`. Upstream declares `go 1.25.0` and builds
its own images with golang:1.25.9, while `golang:latest` (Go 1.27) fails to compile the
transitive dependency `github.com/enetx/http2`. Do not move it back to `latest`.

## Base Image

The final image is based on `ghcr.io/feederbox826/stash-s6:hwaccel-develop` which provides NVIDIA CUDA runtime and FFmpeg with hardware codec support.
