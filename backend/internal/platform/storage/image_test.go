package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestStripImageMetadata(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		input       []byte
	}{
		{name: "jpeg GPS EXIF", contentType: "image/jpeg", input: jpegWithGPS(t)},
		{name: "PNG metadata", contentType: "image/png", input: pngWithText(t)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := StripImageMetadata(test.contentType, bytes.NewReader(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(got, []byte("GPS")) || bytes.Contains(got, []byte("Latitude")) || bytes.Contains(got, []byte("private note")) {
				t.Fatalf("sanitized image retained metadata")
			}
			if _, _, err := image.Decode(bytes.NewReader(got)); err != nil {
				t.Fatalf("sanitized image is not decodable: %v", err)
			}
		})
	}
}

func TestStripImageMetadataRejectsUnsupportedType(t *testing.T) {
	if _, err := StripImageMetadata("image/webp", bytes.NewReader([]byte("not an image"))); err != ErrUnsupportedImageType {
		t.Fatalf("error = %v, want %v", err, ErrUnsupportedImageType)
	}
}

func TestStripImageMetadataAppliesEXIFOrientation(t *testing.T) {
	input := jpegWithOrientation(t, 6)
	got, err := StripImageMetadata("image/jpeg", bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1 || config.Height != 2 {
		t.Fatalf("dimensions=%dx%d, want 1x2", config.Width, config.Height)
	}
}

func TestStripImageMetadataRejectsMoreThanFortyMegapixels(t *testing.T) {
	if _, err := StripImageMetadata("image/png", bytes.NewReader(oversizedPNG(t))); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("error=%v, want %v", err, ErrImageTooLarge)
	}
}

func jpegWithGPS(t *testing.T) []byte {
	t.Helper()
	var base bytes.Buffer
	if err := jpeg.Encode(&base, fixtureImage(color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff}), nil); err != nil {
		t.Fatal(err)
	}
	// This APP1 payload is a real EXIF/TIFF IFD containing GPSInfo (tag 0x8825)
	// and an ASCII GPS marker. It is deliberately added to a valid JPEG fixture.
	exif := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x01\x00%\x88\x04\x00\x01\x00\x00\x00\x1a\x00\x00\x00\x00\x00\x00\x00\x01\x00\x02\x00\x02\x00\x00\x00\x02\x00\x00\x00(\x00\x00\x00\x00\x00\x00\x00NGPSLatitude\x00")
	segment := append([]byte{0xff, 0xe1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	return append(append([]byte{}, base.Bytes()[:2]...), append(segment, base.Bytes()[2:]...)...)
}

func jpegWithOrientation(t *testing.T, orientation uint16) []byte {
	t.Helper()
	var base bytes.Buffer
	image := image.NewRGBA(image.Rect(0, 0, 2, 1))
	image.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	image.Set(1, 0, color.RGBA{B: 0xff, A: 0xff})
	if err := jpeg.Encode(&base, image, nil); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(exif[24:26], orientation)
	segment := append([]byte{0xff, 0xe1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	return append(append([]byte{}, base.Bytes()[:2]...), append(segment, base.Bytes()[2:]...)...)
}

func oversizedPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, fixtureImage(color.Black)); err != nil {
		t.Fatal(err)
	}
	result := append([]byte(nil), out.Bytes()...)
	binary.BigEndian.PutUint32(result[16:20], 8_000)
	binary.BigEndian.PutUint32(result[20:24], 5_001)
	binary.BigEndian.PutUint32(result[29:33], crc32.ChecksumIEEE(result[12:29]))
	return result
}

func pngWithText(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := png.Encoder{}
	if err := encoder.Encode(&out, fixtureImage(color.RGBA{R: 1, A: 0xff})); err != nil {
		t.Fatal(err)
	}
	// The PNG encoder emits no textual chunks; use a PNG byte sequence with a
	// metadata marker to exercise the same re-encode path without relying on a
	// metadata parsing dependency.
	return append(out.Bytes(), []byte("private note")...)
}

func fixtureImage(c color.Color) image.Image {
	result := image.NewRGBA(image.Rect(0, 0, 2, 2))
	draw.Draw(result, result.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return result
}
