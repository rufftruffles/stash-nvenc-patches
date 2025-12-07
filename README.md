# Stash NVIDIA GPU Generation Patches

Patches for [Stash](https://github.com/stashapp/stash) that enable NVIDIA GPU hardware acceleration for all generation tasks including previews, sprites, screenshots, phash, and markers.

## Important

**Hardware Acceleration must be enabled in Stash settings for GPU to be used.** After starting the container, go to Settings - System - Transcoding and enable the Hardware Acceleration checkbox. Without this, all tasks will use CPU.

## Overview

These patches modify Stash's ffmpeg calls to utilize NVIDIA CUDA for decoding and NVENC for encoding during generation tasks. This significantly reduces generation time, especially for 4K content.

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

## Requirements

- NVIDIA GPU with NVENC support (GTX 600+ / Quadro K series or newer)
- Docker with NVIDIA Container Toolkit
- NVIDIA drivers installed on host
- Base image: ghcr.io/feederbox826/stash-s6:hwaccel-develop (or similar with jellyfin-ffmpeg)

## Building

### Quick Build

```bash
docker build -t stash-hwaccel-gen:latest .
```

### Clean Build (recommended)

```bash
docker builder prune -af
docker build --no-cache -t stash-hwaccel-gen:latest .
```

### Build Time

Approximately 15-20 minutes depending on system.

## Docker Compose Configuration

```yaml
services:
  stash:
    image: stash-hwaccel-gen:latest
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

Alternative GPU configuration (if runtime: nvidia is not available):

```yaml
services:
  stash:
    image: stash-hwaccel-gen:latest
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu, compute, video, utility]
```

## Stash Configuration

After starting the container:

1. Navigate to Settings - System - Transcoding
2. Enable **Hardware Acceleration** checkbox (required for GPU to work)
3. Set **FFmpeg path**: `/usr/bin/ffmpeg`
4. Set **FFProbe path**: `/usr/bin/ffprobe`
5. Leave **FFmpeg Transcode Input Args** empty (the patches handle this automatically)
6. Save settings

## Verification

### Check GPU codec detection on startup

```bash
docker logs stash 2>&1 | grep -i "HW codecs"
```

Expected output should show NVENC codecs.

### Monitor ffmpeg commands during generation

```bash
while true; do docker exec stash ps aux 2>/dev/null | grep ffmpeg | grep -v grep | head -5; sleep 1; done
```

Look for:
- `-hwaccel cuda` - GPU decoding enabled
- `-c:v h264_nvenc` - GPU encoding enabled
- `-vf scale=640:-2,format=nv12,hwupload_cuda` - GPU filter chain

### Monitor GPU usage

```bash
watch -n 1 nvidia-smi
```

## Patched Files

The following files are modified from the Stash develop branch:

### FFmpeg Core
- `pkg/ffmpeg/codec_hardware.go` - Exported HWDeviceInit, HWFilterInit methods
- `pkg/ffmpeg/stream_transcode.go` - Updated method calls to use exported names
- `pkg/ffmpeg/stream_segmented.go` - Updated method calls to use exported names

### Transcoder
- `pkg/ffmpeg/transcoder/screenshot.go` - Added ExtraInputArgs field for hwaccel

### Scene Generation
- `pkg/scene/generate/generator.go` - Added GetTranscodeHardwareAcceleration() interface
- `pkg/scene/generate/preview.go` - GPU encoding for preview videos
- `pkg/scene/generate/sprite.go` - GPU decoding for sprite screenshots
- `pkg/scene/generate/screenshot.go` - GPU decoding for cover screenshots
- `pkg/scene/generate/marker_preview.go` - GPU encoding/decoding for markers

### Phash Generation
- `pkg/hash/videophash/phash.go` - GPU decoding for phash frame extraction
- `internal/manager/task_generate_phash.go` - Pass config for hardware acceleration

## Technical Details

### Hardware Encoding (NVENC)

For preview and marker videos, the patches:
1. Detect available hardware codec via HWCodecMP4Compatible()
2. Initialize CUDA device: `-hwaccel_device 0`
3. Build filter chain: `scale=WIDTH:-2,format=nv12,hwupload_cuda`
4. Encode with NVENC: `-c:v h264_nvenc -rc vbr -cq 21`

### Hardware Decoding (CUDA)

For sprites, screenshots, and phash, the patches:
1. Check if Hardware Acceleration is enabled in settings
2. Prepend `-hwaccel cuda` to ffmpeg input arguments
3. Decode frames on GPU before CPU processing

## Troubleshooting

### No GPU codecs detected

Check that ffmpeg path is set to `/usr/bin/ffmpeg` (jellyfin-ffmpeg with NVENC), not the bundled stash ffmpeg.

### Generation errors with exit status

Check docker logs for specific error:
```bash
docker logs stash 2>&1 | grep -i "error\|failed" | tail -20
```

### GPU not showing usage in nvidia-smi

Generation tasks complete quickly. Use continuous monitoring:
```bash
nvidia-smi dmon -s u -d 1
```

### Container not seeing GPU

Verify NVIDIA runtime is configured:
```bash
docker run --rm --gpus all nvidia/cuda:11.0-base nvidia-smi
```

### Generation still using CPU

1. Verify Hardware Acceleration is enabled in Settings - System - Transcoding
2. Check FFmpeg path is `/usr/bin/ffmpeg` not the bundled version
3. Restart container after changing settings

## Performance Notes

- Preview generation: 3-5x faster on 4K content
- Sprite generation: 2-3x faster on 4K content  
- Phash generation: 2-3x faster on 4K content
- Lower CPU usage during generation tasks
- GPU utilization depends on parallel task count and source resolution

## Limitations

- NVIDIA GPUs only (no Intel QSV or AMD AMF support)
- WebP preview encoding remains CPU-bound (no hardware encoder exists)
- Requires specific base image with NVENC-enabled ffmpeg

## Files Included

```
stash-build/
├── Dockerfile           # Multi-stage build file
├── docker-compose.yml   # Example compose file
├── build.sh             # Build script
├── README.md            # This file
└── patches/             # Modified Go source files
    ├── codec_hardware.go
    ├── stream_transcode.go
    ├── stream_segmented.go
    ├── screenshot.go
    ├── generator.go
    ├── preview.go
    ├── sprite.go
    ├── screenshot_generate.go
    ├── marker_preview.go
    ├── phash.go
    └── task_generate_phash.go
```

## Credits

- [Stash](https://github.com/stashapp/stash) - Original project
- [feederbox826/stash-s6](https://github.com/feederbox826/docker-stash-s6) - Base Docker image with hardware acceleration support

## License

Same license as Stash (AGPL-3.0)
