package generate

import (
	"context"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
)

const (
	markerPreviewWidth        = 640
	maxMarkerPreviewDuration  = 20
	markerPreviewAudioBitrate = "64k"

	markerImageDuration = 5
	markerWebpFPS       = 12

	markerScreenshotQuality = 2
)

func (g Generator) MarkerPreviewVideo(ctx context.Context, input string, hash string, seconds float64, endSeconds *float64, includeAudio bool) error {
	lockCtx := g.LockManager.ReadLock(ctx, input)
	defer lockCtx.Cancel()

	output := g.MarkerPaths.GetVideoPreviewPath(hash, int(seconds))
	if !g.Overwrite {
		if exists, _ := fsutil.FileExists(output); exists {
			return nil
		}
	}

	duration := float64(maxMarkerPreviewDuration)

	// don't allow preview to exceed max duration
	if endSeconds != nil && *endSeconds-seconds < maxMarkerPreviewDuration {
		duration = float64(*endSeconds) - seconds
	}

	if err := g.generateFile(lockCtx, g.MarkerPaths, mp4Pattern, output, g.markerPreviewVideo(input, sceneMarkerOptions{
		Seconds:  seconds,
		Duration: duration,
		Audio:    includeAudio,
	})); err != nil {
		return err
	}

	logger.Debug("created marker video: ", output)

	return nil
}

type sceneMarkerOptions struct {
	Seconds  float64
	Duration float64
	Audio    bool
}

// getMarkerVideoCodec returns the video codec to use for marker preview generation.
// If hardware acceleration is enabled and a compatible hardware codec is available,
// returns the hardware codec. Otherwise returns libx264.
func (g Generator) getMarkerVideoCodec() ffmpeg.VideoCodec {
	if g.FFMpegConfig.GetTranscodeHardwareAcceleration() {
		if hwcodec := g.Encoder.HWCodecMP4Compatible(); hwcodec != nil {
			logger.Debugf("[generator] Using hardware codec for marker preview: %s", hwcodec.Name)
			return *hwcodec
		}
	}
	return ffmpeg.VideoCodecLibX264
}

func (g Generator) markerPreviewVideo(input string, options sceneMarkerOptions) generateFn {
	return func(lockCtx *fsutil.LockContext, tmpFn string) error {
		codec := g.getMarkerVideoCodec()
		useHardware := codec != ffmpeg.VideoCodecLibX264

		var videoFilter ffmpeg.VideoFilter
		videoFilter = videoFilter.ScaleWidth(markerPreviewWidth)

		var videoArgs ffmpeg.Args

		if useHardware {
			// Hardware encoding: scale on CPU first, then upload to GPU
			hwFilter := g.Encoder.HWFilterInit(codec, false)
			if hwFilter != "" {
				videoFilter = ffmpeg.VideoFilter(string(videoFilter) + "," + string(hwFilter))
			}
			videoArgs = videoArgs.VideoFilter(videoFilter)
			videoArgs = append(videoArgs,
				"-rc", "vbr",
				"-cq", "21",
				"-movflags", "+faststart",
			)
		} else {
			// Software encoding: use original settings
			videoArgs = videoArgs.VideoFilter(videoFilter)
			videoArgs = append(videoArgs,
				"-pix_fmt", "yuv420p",
				"-profile:v", "high",
				"-level", "4.2",
				"-preset", "veryslow",
				"-crf", "24",
				"-movflags", "+faststart",
				"-threads", "4",
				"-sws_flags", "lanczos",
				"-strict", "-2",
			)
		}

		// Build extra input args with hardware device initialization if needed
		extraInputArgs := g.FFMpegConfig.GetTranscodeInputArgs()
		if useHardware {
			var hwArgs ffmpeg.Args
			hwArgs = g.Encoder.HWDeviceInit(hwArgs, codec, false)
			extraInputArgs = append(hwArgs, extraInputArgs...)
		}

		trimOptions := transcoder.TranscodeOptions{
			Duration:        options.Duration,
			StartTime:       options.Seconds,
			OutputPath:      tmpFn,
			VideoCodec:      codec,
			VideoArgs:       videoArgs,
			ExtraInputArgs:  extraInputArgs,
			ExtraOutputArgs: g.FFMpegConfig.GetTranscodeOutputArgs(),
		}

		if options.Audio {
			var audioArgs ffmpeg.Args
			audioArgs = audioArgs.AudioBitrate(markerPreviewAudioBitrate)

			trimOptions.AudioCodec = ffmpeg.AudioCodecAAC
			trimOptions.AudioArgs = audioArgs
		}

		args := transcoder.Transcode(input, trimOptions)

		return g.generate(lockCtx, args)
	}
}

func (g Generator) SceneMarkerWebp(ctx context.Context, input string, hash string, seconds float64) error {
	lockCtx := g.LockManager.ReadLock(ctx, input)
	defer lockCtx.Cancel()

	output := g.MarkerPaths.GetWebpPreviewPath(hash, int(seconds))
	if !g.Overwrite {
		if exists, _ := fsutil.FileExists(output); exists {
			return nil
		}
	}

	if err := g.generateFile(lockCtx, g.MarkerPaths, webpPattern, output, g.sceneMarkerWebp(input, sceneMarkerOptions{
		Seconds: seconds,
	})); err != nil {
		return err
	}

	logger.Debug("created marker image: ", output)

	return nil
}

func (g Generator) sceneMarkerWebp(input string, options sceneMarkerOptions) generateFn {
	return func(lockCtx *fsutil.LockContext, tmpFn string) error {
		var videoFilter ffmpeg.VideoFilter
		videoFilter = videoFilter.ScaleWidth(markerPreviewWidth)
		videoFilter = videoFilter.Fps(markerWebpFPS)

		var videoArgs ffmpeg.Args
		videoArgs = videoArgs.VideoFilter(videoFilter)
		videoArgs = append(videoArgs,
			"-lossless", "1",
			"-q:v", "70",
			"-compression_level", "6",
			"-preset", "default",
			"-loop", "0",
			"-threads", "4",
		)

		// Build input args - add hwaccel cuda if hardware acceleration is enabled
		var extraInputArgs []string
		if g.FFMpegConfig.GetTranscodeHardwareAcceleration() {
			extraInputArgs = append(extraInputArgs, "-hwaccel", "cuda")
		}

		trimOptions := transcoder.TranscodeOptions{
			Duration:        markerImageDuration,
			StartTime:       float64(options.Seconds),
			OutputPath:      tmpFn,
			VideoCodec:      ffmpeg.VideoCodecLibWebP,
			VideoArgs:       videoArgs,
			ExtraInputArgs:  extraInputArgs,
			ExtraOutputArgs: g.FFMpegConfig.GetTranscodeOutputArgs(),
		}

		args := transcoder.Transcode(input, trimOptions)

		return g.generate(lockCtx, args)
	}
}

func (g Generator) SceneMarkerScreenshot(ctx context.Context, input string, hash string, seconds float64, width int) error {
	lockCtx := g.LockManager.ReadLock(ctx, input)
	defer lockCtx.Cancel()

	output := g.MarkerPaths.GetScreenshotPath(hash, int(seconds))
	if !g.Overwrite {
		if exists, _ := fsutil.FileExists(output); exists {
			return nil
		}
	}

	if err := g.generateFile(lockCtx, g.MarkerPaths, jpgPattern, output, g.sceneMarkerScreenshot(input, SceneMarkerScreenshotOptions{
		Seconds: seconds,
		Width:   width,
	})); err != nil {
		return err
	}

	logger.Debug("created marker screenshot: ", output)

	return nil
}

type SceneMarkerScreenshotOptions struct {
	Seconds float64
	Width   int
}

func (g Generator) sceneMarkerScreenshot(input string, options SceneMarkerScreenshotOptions) generateFn {
	return func(lockCtx *fsutil.LockContext, tmpFn string) error {
		// Build input args - add hwaccel cuda if hardware acceleration is enabled
		var extraInputArgs []string
		if g.FFMpegConfig.GetTranscodeHardwareAcceleration() {
			extraInputArgs = append(extraInputArgs, "-hwaccel", "cuda")
		}

		ssOptions := transcoder.ScreenshotOptions{
			OutputPath:     tmpFn,
			OutputType:     transcoder.ScreenshotOutputTypeImage2,
			Quality:        markerScreenshotQuality,
			Width:          options.Width,
			ExtraInputArgs: extraInputArgs,
		}

		args := transcoder.ScreenshotTime(input, options.Seconds, ssOptions)

		return g.generate(lockCtx, args)
	}
}
