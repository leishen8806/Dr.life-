package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

var ErrUnsupportedImageType = errors.New("storage image type is not supported for metadata stripping")
var ErrImageTooLarge = errors.New("storage image exceeds 40 megapixels")

const maxImagePixels int64 = 40_000_000

// StripImageMetadata decodes and re-encodes a supported image. Re-encoding is
// intentional: JPEG EXIF/XMP/IPTC APP segments and PNG ancillary metadata are
// not copied to the returned bytes.
func StripImageMetadata(contentType string, source io.Reader) ([]byte, error) {
	contents, err := io.ReadAll(source)
	if err != nil {
		return nil, err
	}
	if contentType != "image/jpeg" && contentType != "image/png" {
		return nil, ErrUnsupportedImageType
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(contents))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxImagePixels {
		return nil, ErrImageTooLarge
	}

	var output bytes.Buffer
	switch contentType {
	case "image/jpeg":
		decoded, err := jpeg.Decode(bytes.NewReader(contents))
		if err != nil {
			return nil, err
		}
		decoded = applyOrientation(decoded, jpegOrientation(contents))
		if err := jpeg.Encode(&output, decoded, nil); err != nil {
			return nil, err
		}
	case "image/png":
		decoded, err := png.Decode(bytes.NewReader(contents))
		if err != nil {
			return nil, err
		}
		if err := (&png.Encoder{}).Encode(&output, decoded); err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}

func applyOrientation(source image.Image, orientation uint16) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	destination := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			destination.Set(x, y, source.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return destination
}

func jpegOrientation(contents []byte) uint16 {
	if len(contents) < 4 || contents[0] != 0xff || contents[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(contents); {
		if contents[offset] != 0xff {
			return 1
		}
		marker := contents[offset+1]
		offset += 2
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		length := int(binary.BigEndian.Uint16(contents[offset : offset+2]))
		if length < 2 || offset+length > len(contents) {
			return 1
		}
		payload := contents[offset+2 : offset+length]
		if marker == 0xe1 && len(payload) >= 14 && bytes.Equal(payload[:6], []byte("Exif\x00\x00")) {
			if orientation := tiffOrientation(payload[6:]); orientation != 0 {
				return orientation
			}
		}
		offset += length
	}
	return 1
}

func tiffOrientation(tiff []byte) uint16 {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 0 || offset+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 && order.Uint16(tiff[entry+2:entry+4]) == 3 && order.Uint32(tiff[entry+4:entry+8]) == 1 {
			return order.Uint16(tiff[entry+8 : entry+10])
		}
	}
	return 0
}
