package attachment

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// renderPNG encodes a solid-color image as PNG for the given dimensions.
func renderPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCompressForStorageResizesOversizedImages(t *testing.T) {
	// A 4000x3000 opaque PNG far exceeds the 1568 long-edge cap.
	data := renderPNG(t, 4000, 3000)
	out, ext := compressForStorage(data, ".png")
	if ext != ".jpg" {
		t.Fatalf("opaque image should re-encode as JPEG, got ext %q", ext)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width > storageMaxDimension || cfg.Height > storageMaxDimension {
		t.Fatalf("resized dimensions = %dx%d, want long edge <= %d", cfg.Width, cfg.Height, storageMaxDimension)
	}
	if len(out) >= len(data) {
		t.Fatalf("compressed size %d not smaller than source %d", len(out), len(data))
	}
}

func TestCompressForStorageKeepsSmallImagesWhenLarger(t *testing.T) {
	// A tiny image whose JPEG re-encode could be larger than the original
	// bytes must pass through unchanged (compression never grows files).
	data := renderPNG(t, 8, 8)
	out, ext := compressForStorage(data, ".png")
	if ext != ".png" || !bytes.Equal(out, data) {
		t.Fatalf("small image was re-encoded: ext=%q len=%d (source %d)", ext, len(out), len(data))
	}
}

func TestCompressForStorageIsDeterministic(t *testing.T) {
	data := renderPNG(t, 2000, 1500)
	first, _ := compressForStorage(data, ".png")
	second, _ := compressForStorage(data, ".png")
	if !bytes.Equal(first, second) {
		t.Fatal("compression is not byte-stable; prompt-cache prefixes would break")
	}
}

func TestCompressForStorageKeepsGifBytes(t *testing.T) {
	// Minimal GIF header; animated entries must pass through untouched.
	gif := append([]byte("GIF89a"), make([]byte, 16)...)
	out, ext := compressForStorage(gif, ".gif")
	if ext != ".gif" || !bytes.Equal(out, gif) {
		t.Fatal("GIF was modified by storage compression")
	}
}

func TestCompressForStoragePreservesTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2000, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 2000; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0, B: 0, A: 0x80})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, ext := compressForStorage(buf.Bytes(), ".png")
	if ext != ".png" {
		t.Fatalf("transparent image must stay PNG, got ext %q", ext)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if imageIsOpaque(decoded) {
		t.Fatal("transparency was lost during compression")
	}
}

func TestSaveImageCompressesBeforeWriting(t *testing.T) {
	// SaveImage end-to-end: a huge paste is stored already-compressed, with
	// inspectable dimensions matching the stored bytes.
	data := renderPNG(t, 4000, 3000)
	path, err := SaveImage("sess_compress", data)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(path))

	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) >= len(data) {
		t.Fatalf("stored image was not compressed: %d bytes (source %d)", len(stored), len(data))
	}
	mime, width, height, err := InspectImage(path)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("stored mime = %q, want image/jpeg", mime)
	}
	if width > storageMaxDimension || height > storageMaxDimension {
		t.Fatalf("stored dimensions %dx%d exceed the %d cap", width, height, storageMaxDimension)
	}
	// The stored bytes must be decodable by the provider path.
	if _, err := jpeg.Decode(bytes.NewReader(stored)); err != nil {
		t.Fatalf("stored image is not decodable: %v", err)
	}
}

func TestInspectImageDecodesGIFHeaders(t *testing.T) {
	// GIF89a + logical screen descriptor: 64x32 little-endian. A GIF that
	// passes through SaveImage must still be inspectable, otherwise every
	// attachment degrades to a headerless entry (max-fallback token
	// estimate plus an error log per image).
	header := []byte{
		'G', 'I', 'F', '8', '9', 'a',
		0x40, 0x00, // width 64
		0x20, 0x00, // height 32
		0x00, 0x00, 0x00, // packed, background, aspect
	}
	path := filepath.Join(t.TempDir(), "img.gif")
	if err := os.WriteFile(path, header, 0o600); err != nil {
		t.Fatal(err)
	}
	mime, width, height, err := InspectImage(path)
	if err != nil {
		t.Fatalf("InspectImage failed on GIF (decoder not registered?): %v", err)
	}
	if mime != "image/gif" || width != 64 || height != 32 {
		t.Fatalf("InspectImage = %q %dx%d, want image/gif 64x32", mime, width, height)
	}
}
