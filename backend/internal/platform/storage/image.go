package storage

import (
	"bytes"
	"errors"
	"image/jpeg"
	"image/png"
	"io"
)

var ErrUnsupportedImageType = errors.New("storage image type is not supported for metadata stripping")

// StripImageMetadata decodes and re-encodes a supported image. Re-encoding is
// intentional: JPEG EXIF/XMP/IPTC APP segments and PNG ancillary metadata are
// not copied to the returned bytes.
func StripImageMetadata(contentType string, source io.Reader) ([]byte, error) {
	var output bytes.Buffer
	switch contentType {
	case "image/jpeg":
		decoded, err := jpeg.Decode(source)
		if err != nil {
			return nil, err
		}
		if err := jpeg.Encode(&output, decoded, nil); err != nil {
			return nil, err
		}
	case "image/png":
		decoded, err := png.Decode(source)
		if err != nil {
			return nil, err
		}
		if err := (&png.Encoder{}).Encode(&output, decoded); err != nil {
			return nil, err
		}
	default:
		return nil, ErrUnsupportedImageType
	}
	return output.Bytes(), nil
}
