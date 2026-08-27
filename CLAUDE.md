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
| `screenshot.go` | `pkg/ffmpeg/transcoder/screenshot.go` |
| `generator.go` | `pkg/scene/generate/generator.go` |
| `preview.go` | `pkg/scene/generate/preview.go` |
| `sprite.go` | `pkg/scene/generate/sprite.go` |
| `screenshot_generate.go` | `pkg/scene/generate/screenshot.go` |
| `marker_preview.go` | `pkg/scene/generate/marker_preview.go` |
| `phash.go` | `pkg/hash/videophash/phash.go` |
| `task_generate_phash.go` | `internal/manager/task_generate_phash.go` |

The Dockerfile verifies patches with `grep -q` checks for key symbols after copying.

## Key Architecture Concepts

**GPU Encoding (preview/marker videos):** `codec_hardware.go` exports `HWCodecMP4Compatible()` which returns the best available hardware codec. `preview.go` and `marker_preview.go` use it to build ffmpeg args with `-hwaccel cuda`, CUDA filter chains (`scale_cuda`, `hwupload_cuda`), and NVENC output (`h264_nvenc`).

**GPU Decoding (sprites/screenshots/phash):** For image-output tasks, patches add `-hwaccel cuda` to ffmpeg input args when `GetTranscodeHardwareAcceleration()` returns true. This is injected via the `FFMpegConfig` interface defined in `generator.go`.

**Config interface:** `FFMpegConfig` (in `generator.go`) is the central interface patches use to check hardware acceleration state. It provides `GetTranscodeHardwareAcceleration() bool`.

**`ExtraInputArgs`:** Added to `screenshot.go` (transcoder) and used by sprite/screenshot generators to pass `-hwaccel cuda` as extra input arguments.

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
