package attachment

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"

	_ "image/gif" // register the GIF decoder for DecodeConfig (InspectImage)

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register the webp decoder for DecodeConfig/Decode
)

// storageMaxDimension caps the long edge of saved images. It matches the
// Anthropic server-side downscale limit (1568), keeps OpenAI/Gemini tile
// counts small, and bounds the visual-token cost of native image input.
// Compression happens exactly once, at save time: the stored bytes are final,
// so every later re-encode for provider requests is byte-stable and the
// prompt-cache prefix never breaks.
const storageMaxDimension = 1568

// Storage compression guards against decompression surprises: a 20 MiB
// payload of flat color can decode to hundreds of MB of pixel buffers, so
// images beyond these bounds pass through untouched (mirroring the
// image_understand fast-mode limits).
const (
	storageMaxSourceDimension = 32768
	storageMaxSourcePixels    = 25_000_000
)

// compressForStorage shrinks an image before it is written to disk. Animated
// GIFs, undecodable payloads, and already-compact JPEG/WebP within the
// dimension budget pass through unchanged; everything else is resized to the
// long-edge cap and re-encoded (opaque pixels as JPEG q85, transparency as
// PNG). When re-encoding would grow the file, the original bytes win.
func compressForStorage(data []byte, ext string) ([]byte, string) {
	if ext == ".gif" {
		return data, ext
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return data, ext
	}
	if config.Width > storageMaxSourceDimension || config.Height > storageMaxSourceDimension ||
		int64(config.Width)*int64(config.Height) > storageMaxSourcePixels {
		return data, ext
	}
	needsResize := config.Width > storageMaxDimension || config.Height > storageMaxDimension
	switch ext {
	case ".jpg":
		if !needsResize {
			return data, ext
		}
	case ".webp":
		// WebP within the budget is already compact; only oversized
		// entries pay for a decode/re-encode round trip.
		if !needsResize {
			return data, ext
		}
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, ext
	}
	if needsResize {
		width, height := scaledImageDimensions(config.Width, config.Height, storageMaxDimension)
		destination := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.ApproxBiLinear.Scale(destination, destination.Bounds(), source, source.Bounds(), draw.Over, nil)
		source = destination
	}
	if imageIsOpaque(source) {
		var output bytes.Buffer
		if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: 85}); err == nil {
			if needsResize || output.Len() < len(data) {
				return output.Bytes(), ".jpg"
			}
		}
		return data, ext
	}
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := encoder.Encode(&output, source); err == nil {
		if needsResize || output.Len() < len(data) {
			return output.Bytes(), ".png"
		}
	}
	return data, ext
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

// imageIsOpaque reports whether every pixel is fully opaque. Common decoded
// types are scanned directly over their pixel buffers; anything else falls
// back to per-pixel At calls.
func imageIsOpaque(img image.Image) bool {
	switch v := img.(type) {
	case *image.Gray, *image.YCbCr, *image.CMYK:
		return true
	case *image.RGBA:
		for i := 3; i < len(v.Pix); i += 4 {
			if v.Pix[i] != 0xff {
				return false
			}
		}
		return true
	case *image.NRGBA:
		for i := 3; i < len(v.Pix); i += 4 {
			if v.Pix[i] != 0xff {
				return false
			}
		}
		return true
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
				return false
			}
		}
	}
	return true
}

// InspectImage returns the storage MIME type and pixel dimensions of a saved
// image file. It reads only the header, so it is safe to call when building
// message metadata.
func InspectImage(path string) (mime string, width, height int, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, 0, fmt.Errorf("open image for inspection: %w", err)
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil {
		return "", 0, 0, fmt.Errorf("inspect image %s: %w", path, err)
	}
	mime = formatToMime(format)
	if mime == "" {
		return "", 0, 0, fmt.Errorf("inspect image %s: unsupported format %q", path, format)
	}
	return mime, config.Width, config.Height, nil
}

func formatToMime(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	}
	return ""
}
