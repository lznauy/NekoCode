package media

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

const (
	fastImageMaxDimension       = 1280
	fastImageMaxSourceDimension = 32768
	fastImageMaxSourcePixels    = 25_000_000
)

// prepareImageForUnderstanding reduces visual-token and transfer cost in fast
// mode. Accurate mode deliberately preserves the exact source bytes.
func prepareImageForUnderstanding(data []byte, mediaType string, mode imageUnderstandMode) ([]byte, string, error) {
	if mode != imageUnderstandModeFast {
		return data, mediaType, nil
	}
	// Keep animated/extended formats intact. The standard decoders expose only
	// one frame and re-encoding would silently discard useful image content.
	if mediaType == "image/gif" || mediaType == "image/webp" {
		return data, mediaType, nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// readImageFile already preserves the historical header-based validation.
		// Leave unusually encoded but supported images for the remote model.
		return data, mediaType, nil
	}
	if err := validateFastImageDimensions(config.Width, config.Height); err != nil {
		return nil, "", err
	}
	if config.Width <= fastImageMaxDimension && config.Height <= fastImageMaxDimension {
		return data, mediaType, nil
	}

	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image for fast mode: %w", err)
	}
	width, height := scaledImageDimensions(config.Width, config.Height, fastImageMaxDimension)
	destination := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(destination, destination.Bounds(), source, source.Bounds(), draw.Over, nil)

	var output bytes.Buffer
	switch mediaType {
	case "image/jpeg":
		if err := jpeg.Encode(&output, destination, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", fmt.Errorf("encode resized JPEG: %w", err)
		}
		return output.Bytes(), "image/jpeg", nil
	default:
		encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := encoder.Encode(&output, destination); err != nil {
			return nil, "", fmt.Errorf("encode resized PNG: %w", err)
		}
		return output.Bytes(), "image/png", nil
	}
}

func validateFastImageDimensions(width, height int) error {
	if width <= 0 || height <= 0 || width > fastImageMaxSourceDimension || height > fastImageMaxSourceDimension ||
		int64(width)*int64(height) > fastImageMaxSourcePixels {
		return fmt.Errorf("image dimensions %dx%d are too large for fast mode; use accurate mode to send the original image", width, height)
	}
	return nil
}

func scaledImageDimensions(width, height, limit int) (int, int) {
	if width <= limit && height <= limit {
		return width, height
	}
	if width >= height {
		return limit, max(1, height*limit/width)
	}
	return max(1, width*limit/height), limit
}
