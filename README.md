# Stash NVIDIA GPU Generation Patches

Patches for [Stash](https://github.com/stashapp/stash) that run preview and marker video generation fully on an NVIDIA GPU (decode, scale and encode), while leaving single-frame tasks such as sprites, phash and screenshots on the CPU, where they are faster.

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

Look for, on preview and marker video jobs:
- `-hwaccel cuda -hwaccel_output_format cuda` - GPU decoding, frames stay on the GPU
- `scale_cuda` - GPU scaling
- `-c:v h264_nvenc` - GPU encoding

Sprite, phash and screenshot jobs do not use these flags; that is intentional (see below).

Monitor GPU usage:

```bash
watch -n 1 nvidia-smi
```

## Hardware Acceleration Coverage

| Task | Decode | Scale | Encode |
|------|--------|-------|--------|
| Preview videos | NVDEC | `scale_cuda` | NVENC (h264_nvenc) |
| Marker videos | NVDEC | `scale_cuda` | NVENC (h264_nvenc) |
| Sprites | CPU, keyframes only | CPU | CPU |
| Phash, cover and marker screenshots | CPU | CPU | CPU (stock Stash) |
| WebP previews | CPU | CPU | CPU (libwebp) |

If the GPU cannot decode a source (NVDEC does not support 10-bit or 4:2:2 h264, for example), that video's previews automatically fall back to CPU decoding with NVENC encoding. 10-bit HEVC stays fully on the GPU.

### Why single-frame tasks stay on the CPU

Sprites, phash and screenshots start one ffmpeg process per frame (81 for a sprite, 25 for a phash), and every process that uses CUDA pays to set up a GPU context before decoding anything. Measured on an RTX A2000 with 4K h264:

| Single-frame grab | CPU | GPU, persistence mode off | GPU, persistence mode on |
|---|---|---|---|
| Keyframe every 0.5 s | ~150 ms | ~2,200 ms | ~450 ms |
| Keyframe every 10 s | ~1,180 ms | ~3,000 ms | ~960 ms |

Sprites additionally grab the nearest keyframe instead of the exact timestamp (`-skip_frame nokey -noaccurate_seek`), so ffmpeg decodes one frame instead of everything between the previous keyframe and the target. Each thumbnail can be up to one keyframe interval (typically 2-4 s) early, which does not matter for a scrub bar. With four workers this made a sprite grab 3-9x faster on 4K sources (for example 531 ms to 61 ms with 4 s keyframe intervals). Phash keeps exact timestamps so its values do not change.

For typical sources the CPU wins even with a warm GPU. Only sources with long keyframe intervals come out ahead on the GPU, and then only by about 20% and only with persistence mode on, so these tasks use stock Stash. CPU and CUDA decoding produce byte-identical frames, so phash values are the same either way.

Video work is the opposite: with the GPU warm, a 0.75 s preview segment took about 470 ms with the full GPU pipeline versus about 700 ms when the CPU decoded the 4K source, and used about 70% less CPU time.

### GPU keep-warm and persistence mode

Without persistence mode, the NVIDIA driver shuts the GPU down whenever no process is using it, and the next process has to reload the GPU firmware. On the RTX A2000 that added about 1.7-2 s to every ffmpeg run: a preview segment took about 2.5 s instead of about 0.47 s. Stash starts a separate ffmpeg process per preview segment, so this would dominate preview generation.

The image handles this itself: while preview or marker videos are generating, Stash keeps one idle ffmpeg process holding the GPU open (visible as `ffmpeg ... -init_hw_device cuda ... nullsrc`), and stops it 60 seconds after the last job. It uses effectively no CPU or GPU, and it is killed automatically if Stash exits.

Persistence mode on the host is still worthwhile, because it also removes the start-up delay for live transcoding when you press play. As root:

```bash
nvidia-smi -pm 1
```

This resets on reboot. To make it permanent, enable the `nvidia-persistenced` service; if your driver was installed with NVIDIA's `.run` installer and has no unit file, run the installer in `/usr/share/doc/NVIDIA_GLX-1.0/samples/nvidia-persistenced-init.tar.bz2`. Check with `nvidia-smi --query-gpu=persistence_mode --format=csv`.

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

### Preview and marker videos

1. Detect the hardware codec via `HWCodecMP4Compatible()`
2. With NVENC, try the full GPU pipeline first:
   `-hwaccel_device 0 -hwaccel cuda -hwaccel_output_format cuda`, filter
   `scale_cuda=w=WIDTH:h=-2:format=yuv420p`, encode `-c:v h264_nvenc -rc vbr -cq 21`
3. If ffmpeg fails, retry with CPU decoding and GPU encoding
   (`scale=WIDTH:-2,format=nv12,hwupload_cuda`) for the rest of that video

### Patched Files

- `pkg/ffmpeg/codec_hardware.go` - Exported HWDeviceInit, HWFilterInit methods
- `pkg/ffmpeg/stream_transcode.go` - Updated method calls
- `pkg/ffmpeg/stream_segmented.go` - Updated method calls
- `pkg/scene/generate/generator.go` - Added GetTranscodeHardwareAcceleration()
- `pkg/scene/generate/preview.go` - Full GPU previews with CPU-decode fallback
- `pkg/scene/generate/marker_preview.go` - Full GPU marker videos with CPU-decode fallback
- `pkg/scene/generate/sprite.go` - Keyframe-only sprite thumbnails
- `pkg/ffmpeg/hw_keepwarm*.go` - Keep the CUDA context open while generating (new files)

## Limitations

- NVIDIA GPUs only (no Intel QSV or AMD AMF support)
- The full GPU preview pipeline is NVENC-only; other hardware encoders keep CPU decoding
- WebP previews, sprites, phash and screenshots run on the CPU by design (see above)

## Credits

- [Stash](https://github.com/stashapp/stash) - Original project
- [feederbox826/stash-s6](https://github.com/feederbox826/docker-stash-s6) - Base Docker image with hardware acceleration support

## License

Same license as Stash (AGPL-3.0)
