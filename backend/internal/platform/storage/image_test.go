package storage

import (
	"bytes"
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
