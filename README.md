# Stash NVIDIA GPU Generation Patches

Patches for [Stash](https://github.com/stashapp/stash) that enable NVIDIA GPU hardware acceleration for all generation tasks including previews, sprites, screenshots, phash, and markers. Reduces generation time by 3-5x on 4K content.

## Prerequisites

Before using this image, ensure you have:

- NVIDIA GPU with NVENC support (GTX 600+ / Quadro K series or newer)
- NVIDIA drivers installed on host
- NVIDIA Container Toolkit installed
- Docker configured with NVIDIA runtime

Verify your setup:
```bash
# Check NVIDIA drivers
nvidia-smi

# Check Docker can access GPU
docker run --rm --gpus all nvidia/cuda:11.0-base nvidia-smi
```

If the above commands fail, install NVIDIA Container Toolkit first:
```bash
# Ubuntu/Debian
sudo apt install nvidia-container-toolkit
sudo systemctl restart docker
```

## Quick Start

Pull the pre-built image:

```bash
docker pull ghcr.io/rufftruffles/stash-nvenc-patches:latest
```

Docker Compose:

```yaml
services:
  stash:
    image: ghcr.io/rufftruffles/stash-nvenc-patches:latest
    container_name: stash
    runtime: nvidia
    environment:
      - NVIDIA_VISIBLE_DEVICES=all
      - NVIDIA_DRIVER_CAPABILITIES=compute,video,utility
    volumes:
      - ./config:/root/.stash
      - ./generated:/generated
      - ./data:/data
    ports:
      - "9999:9999"
```

Start the container:

```bash
docker-compose up -d
```

## Stash Configuration

**This step is required for GPU acceleration to work.**

After starting the container, go to Settings - System - Transcoding and configure:

1. Enable **Hardware Acceleration** checkbox
2. Set **FFmpeg path** to `/usr/bin/ffmpeg` (not the default /root/.stash/ffmpeg)
3. Set **FFprobe path** to `/usr/bin/ffprobe`
4. Leave **FFmpeg Transcode Input Args** empty
5. Save settings

Without these settings, all tasks will use CPU.

## Verification

Check GPU codec detection on startup:

```bash
docker logs stash 2>&1 | grep -i "HW codecs"
```

Monitor ffmpeg commands during generation:

```bash
while true; do docker exec stash ps aux 2>/dev/null | grep ffmpeg | grep -v grep | head -5; sleep 1; done
```

Look for:
- `-hwaccel cuda` - GPU decoding enabled
- `-c:v h264_nvenc` - GPU encoding enabled

Monitor GPU usage:

```bash
watch -n 1 nvidia-smi
```

## Hardware Acceleration Coverage

| Task | Decode | Encode | Notes |
|------|--------|--------|-------|
| Preview videos | CUDA | NVENC (h264_nvenc) | Full GPU acceleration |
| Marker videos | CUDA | NVENC (h264_nvenc) | Full GPU acceleration |
| Sprites (81 thumbnails) | CUDA | N/A (BMP output) | GPU decode only |
| Cover screenshots | CUDA | N/A (JPEG output) | GPU decode only |
| Phash generation | CUDA | N/A (BMP output) | GPU decode only |
| Marker screenshots | CUDA | N/A (JPEG output) | GPU decode only |
| WebP previews | CUDA | CPU (libwebp) | No hardware WebP encoder exists |

## Building From Source

If you prefer to build the image yourself:

```bash
git clone https://github.com/rufftruffles/stash-nvenc-patches.git
cd stash-nvenc-patches
docker build -t stash-hwaccel-gen:latest .
```

Clean build (recommended if you encounter issues):

```bash
docker builder prune -af
docker build --no-cache -t stash-hwaccel-gen:latest .
```

Build takes approximately 15-20 minutes.

## Troubleshooting

### No GPU codecs detected

Ensure FFmpeg path is set to `/usr/bin/ffmpeg`, not the default bundled version.

### Generation errors with exit status

Check docker logs:
```bash
docker logs stash 2>&1 | grep -i "error\|failed" | tail -20
```

### GPU not showing usage in nvidia-smi

Generation tasks complete quickly. Use continuous monitoring:
```bash
nvidia-smi dmon -s u -d 1
```

### Container not seeing GPU

1. Verify NVIDIA Container Toolkit is installed
2. Check Docker is configured with NVIDIA runtime
3. Restart Docker: `sudo systemctl restart docker`

### Generation still using CPU

1. Verify Hardware Acceleration is enabled in Settings
2. Check FFmpeg path is `/usr/bin/ffmpeg`
3. Restart container after changing settings

## Technical Details

### Hardware Encoding (NVENC)

For preview and marker videos:
1. Detect available hardware codec via HWCodecMP4Compatible()
2. Initialize CUDA device: `-hwaccel_device 0`
3. Build filter chain: `scale=WIDTH:-2,format=nv12,hwupload_cuda`
4. Encode with NVENC: `-c:v h264_nvenc -rc vbr -cq 21`

### Hardware Decoding (CUDA)

For sprites, screenshots, and phash:
1. Check if Hardware Acceleration is enabled in settings
2. Prepend `-hwaccel cuda` to ffmpeg input arguments
3. Decode frames on GPU before CPU processing

### Patched Files

- `pkg/ffmpeg/codec_hardware.go` - Exported HWDeviceInit, HWFilterInit methods
- `pkg/ffmpeg/stream_transcode.go` - Updated method calls
- `pkg/ffmpeg/stream_segmented.go` - Updated method calls
- `pkg/ffmpeg/transcoder/screenshot.go` - Added ExtraInputArgs field
- `pkg/scene/generate/generator.go` - Added GetTranscodeHardwareAcceleration()
- `pkg/scene/generate/preview.go` - GPU encoding for previews
- `pkg/scene/generate/sprite.go` - GPU decoding for sprites
- `pkg/scene/generate/screenshot.go` - GPU decoding for screenshots
- `pkg/scene/generate/marker_preview.go` - GPU encoding/decoding for markers
- `pkg/hash/videophash/phash.go` - GPU decoding for phash
- `internal/manager/task_generate_phash.go` - Pass config for hardware acceleration

## Limitations

- NVIDIA GPUs only (no Intel QSV or AMD AMF support)
- WebP preview encoding remains CPU-bound (no hardware encoder exists)

## Credits

- [Stash](https://github.com/stashapp/stash) - Original project
- [feederbox826/stash-s6](https://github.com/feederbox826/docker-stash-s6) - Base Docker image with hardware acceleration support

## License

Same license as Stash (AGPL-3.0)
