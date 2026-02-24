package generate

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/ffmpeg/transcoder"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/utils"
)

func (g Generator) SpriteScreenshot(ctx context.Context, input string, seconds float64, size int, isPortrait bool) (image.Image, error) {
	lockCtx := g.LockManager.ReadLock(ctx, input)
	defer lockCtx.Cancel()

	// Build input args - add hwaccel cuda if hardware acceleration is enabled
	var extraInputArgs []string
	if g.FFMpegConfig.GetTranscodeHardwareAcceleration() {
		extraInputArgs = append(extraInputArgs, "-hwaccel", "cuda")
	}

	ssOptions := transcoder.ScreenshotOptions{
		OutputPath:     "-",
		OutputType:     transcoder.ScreenshotOutputTypeBMP,
		ExtraInputArgs: extraInputArgs,
	}

	if !isPortrait {
		ssOptions.Width = size
	} else {
		ssOptions.Height = size
	}

	args := transcoder.ScreenshotTime(input, seconds, ssOptions)

	return g.generateImage(lockCtx, args)
}

func (g Generator) SpriteScreenshotSlow(ctx context.Context, input string, frame int, width int) (image.Image, error) {
	lockCtx := g.LockManager.ReadLock(ctx, input)
	defer lockCtx.Cancel()

	// Build input args - add hwaccel cuda if hardware acceleration is enabled
	var extraInputArgs []string
	if g.FFMpegConfig.GetTranscodeHardwareAcceleration() {
		extraInputArgs = append(extraInputArgs, "-hwaccel", "cuda")
	}

	ssOptions := transcoder.ScreenshotOptions{
		OutputPath:     "-",
		OutputType:     transcoder.ScreenshotOutputTypeBMP,
		Width:          width,
		ExtraInputArgs: extraInputArgs,
	}

	args := transcoder.ScreenshotFrame(input, frame, ssOptions)

	return g.generateImage(lockCtx, args)
}

func (g Generator) generateImage(lockCtx *fsutil.LockContext, args ffmpeg.Args) (image.Image, error) {
	out, err := g.generateOutput(lockCtx, args)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("decoding image from ffmpeg: %w", err)
	}

	return img, nil
}

func (g Generator) CombineSpriteImages(images []image.Image) image.Image {
	width := images[0].Bounds().Size().X
	height := images[0].Bounds().Size().Y
	gridSize := GetSpriteGridSize(len(images))
	canvasWidth := width * gridSize
	canvasHeight := height * gridSize
	montage := imaging.New(canvasWidth, canvasHeight, color.NRGBA{})
	for index := 0; index < len(images); index++ {
		x := width * (index % gridSize)
		y := height * int(math.Floor(float64(index)/float64(gridSize)))
		img := images[index]
		montage = imaging.Paste(montage, img, image.Pt(x, y))
	}

	return montage
}

func GetSpriteGridSize(imageCount int) int {
	return int(math.Ceil(math.Sqrt(float64(imageCount))))
}

func (g Generator) SpriteVTT(ctx context.Context, output string, spritePath string, stepSize float64, spriteChunks int) error {
	lockCtx := g.LockManager.ReadLock(ctx, spritePath)
	defer lockCtx.Cancel()
	return g.generateFile(lockCtx, g.ScenePaths, vttPattern, output, g.spriteVTT(spritePath, stepSize, spriteChunks))
}

func (g Generator) spriteVTT(spritePath string, stepSize float64, spriteChunks int) generateFn {
	return func(lockCtx *fsutil.LockContext, tmpFn string) error {
		spriteImage, err := os.Open(spritePath)
		if err != nil {
			return err
		}
		defer spriteImage.Close()
		spriteImageName := filepath.Base(spritePath)
		image, _, err := image.DecodeConfig(spriteImage)
		if err != nil {
			return err
		}

		gridSize := GetSpriteGridSize(spriteChunks)
		width := image.Width / gridSize
		height := image.Height / gridSize

		vttLines := []string{"WEBVTT", ""}
		for index := 0; index < spriteChunks; index++ {
			x := width * (index % gridSize)
			y := height * int(math.Floor(float64(index)/float64(gridSize)))
			startTime := utils.GetVTTTime(float64(index) * stepSize)
			endTime := utils.GetVTTTime(float64(index+1) * stepSize)

			vttLines = append(vttLines, startTime+" --> "+endTime)
			vttLines = append(vttLines, fmt.Sprintf("%s#xywh=%d,%d,%d,%d", spriteImageName, x, y, width, height))
			vttLines = append(vttLines, "")
		}
		vtt := strings.Join(vttLines, "\n")

		return os.WriteFile(tmpFn, []byte(vtt), 0644)
	}
}
