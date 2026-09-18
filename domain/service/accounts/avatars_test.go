package accounts

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestAvatarNormalizationRejectsInvalidAndOversizedInput(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), bytes.Repeat([]byte("x"), (2<<20)+1)} {
		if _, err := normalizeAvatar(raw); err == nil {
			t.Fatal("non-image or oversized avatar accepted")
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewGray(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAvatar(raw.Bytes()); err == nil {
		t.Fatal("excessive width accepted")
	}
	raw.Reset()
	if err := png.Encode(&raw, image.NewGray(image.Rect(0, 0, 4001, 4000))); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAvatar(raw.Bytes()); err == nil {
		t.Fatal("excessive pixel count accepted")
	}
}

func TestAvatarNormalizationProducesSmallSquareJPEGOnWhite(t *testing.T) {
	input := image.NewNRGBA(image.Rect(0, 0, 800, 400))
	input.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var raw bytes.Buffer
	if err := png.Encode(&raw, input); err != nil {
		t.Fatal(err)
	}
	result, err := normalizeAvatar(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	output, err := jpeg.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	if output.Bounds().Dx() != 256 || output.Bounds().Dy() != 256 || len(result) > 128<<10 {
		t.Fatal("invalid normalized avatar dimensions or size")
	}
	r, g, b, _ := output.At(128, 128).RGBA()
	if r < 65000 || g < 65000 || b < 65000 {
		t.Fatal("transparent input must have a white background")
	}
}

func TestAvatarNormalizationRejectsAnimatedPNG(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	// An APNG animation-control chunk must be rejected before the ordinary PNG decoder ignores it.
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk, 8)
	copy(chunk[4:8], "acTL")
	input := append(append(append([]byte{}, raw.Bytes()[:33]...), chunk...), raw.Bytes()[33:]...)
	if _, err := normalizeAvatar(input); err == nil {
		t.Fatal("animated PNG accepted")
	}
}
