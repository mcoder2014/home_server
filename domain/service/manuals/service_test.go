package manuals

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateManualFieldsUsesDocumentedCharacterLimits(t *testing.T) {
	name, description, access, err := ValidateManualFields(strings.Repeat("说", 120), strings.Repeat("明", 2000), "")
	require.NoError(t, err)
	require.Equal(t, model.ManualAccessOwner, access)
	require.Len(t, []rune(name), 120)
	require.Len(t, []rune(description), 2000)

	_, _, _, err = ValidateManualFields(strings.Repeat("说", 121), "", "owner")
	require.ErrorIs(t, err, apperrors.ErrInvalid)
	_, _, _, err = ValidateManualFields("name", "", "unknown")
	require.ErrorIs(t, err, apperrors.ErrInvalid)
}

func TestValidateCategoriesTrimsDeduplicatesAndPreservesCase(t *testing.T) {
	categories, err := ValidateCategories([]string{" 厨房 ", "厨房", "kitchen", "Kitchen"})
	require.NoError(t, err)
	require.Equal(t, []string{"Kitchen", "kitchen", "厨房"}, categories)

	_, err = ValidateCategories([]string{""})
	require.ErrorIs(t, err, apperrors.ErrInvalid)
	_, err = ValidateCategories([]string{strings.Repeat("类", MaxCategoryRunes+1)})
	require.ErrorIs(t, err, apperrors.ErrInvalid)
	_, err = ValidateCategories(make([]string, MaxCategories+1))
	require.ErrorIs(t, err, apperrors.ErrInvalid)
}

func TestValidateInlineItemRejectsUnsafeURLAndAcceptsLiteralText(t *testing.T) {
	_, err := ValidateInlineItem(InlineItemInput{Kind: "url", URL: "https://user:secret@example.com/manual", ClientRequestID: "unsafe-userinfo"})
	require.ErrorIs(t, err, apperrors.ErrInvalid)
	_, err = ValidateInlineItem(InlineItemInput{Kind: "url", URL: "javascript:alert(1)", ClientRequestID: "unsafe-scheme"})
	require.ErrorIs(t, err, apperrors.ErrInvalid)
	validated, err := ValidateInlineItem(InlineItemInput{Kind: "url", Title: "官网", URL: "https://example.com/manual?q=1", ClientRequestID: "valid-url"})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/manual?q=1", validated.URL)

	validated, err = ValidateInlineItem(InlineItemInput{Kind: "text", Text: "第一行\n第二行", ClientRequestID: "valid-text"})
	require.NoError(t, err)
	require.Equal(t, "第一行\n第二行", validated.Text)
}

func TestCanReadManualCoversOwnerAuthenticatedAndPublicModes(t *testing.T) {
	tests := []struct {
		name     string
		mode     model.ManualAccess
		status   model.ManualStatus
		ownerID  int64
		viewerID int64
		want     bool
	}{
		{name: "owner draft", mode: model.ManualAccessPublic, status: model.ManualStatusDraft, ownerID: 1, viewerID: 1, want: true},
		{name: "other draft", mode: model.ManualAccessPublic, status: model.ManualStatusDraft, ownerID: 1, viewerID: 2},
		{name: "anonymous public active", mode: model.ManualAccessPublic, status: model.ManualStatusActive, ownerID: 1, want: true},
		{name: "anonymous authenticated active", mode: model.ManualAccessAuthenticated, status: model.ManualStatusActive, ownerID: 1},
		{name: "other authenticated active", mode: model.ManualAccessAuthenticated, status: model.ManualStatusActive, ownerID: 1, viewerID: 2, want: true},
		{name: "other owner-only active", mode: model.ManualAccessOwner, status: model.ManualStatusActive, ownerID: 1, viewerID: 2},
		{name: "deleted owner", mode: model.ManualAccessOwner, status: model.ManualStatusDeleted, ownerID: 1, viewerID: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, CanReadManual(tt.mode, tt.status, tt.ownerID, tt.viewerID))
		})
	}
}

func TestImageThumbnailAppliesJPEGEXIFOrientation(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 60, 90))
	draw.Draw(source, image.Rect(0, 0, 30, 45), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(30, 0, 60, 45), image.NewUniform(color.RGBA{G: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(0, 45, 30, 90), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(30, 45, 60, 90), image.NewUniform(color.RGBA{R: 255, G: 255, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100}))
	oriented := insertEXIFOrientation(t, encoded.Bytes(), 6)

	destination := filepath.Join(t.TempDir(), "thumbnail.jpg")
	contentType, err := CreateImageThumbnail(bytes.NewReader(oriented), destination)
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", contentType)
	file, err := os.Open(destination)
	require.NoError(t, err)
	defer file.Close()
	thumbnail, _, err := image.Decode(file)
	require.NoError(t, err)
	require.Equal(t, 90, thumbnail.Bounds().Dx())
	require.Equal(t, 60, thumbnail.Bounds().Dy())
	// Orientation 6 rotates the original clockwise: its blue bottom-left pixel
	// block moves to the thumbnail's top-left. Sample well inside the block so
	// JPEG chroma subsampling at color boundaries cannot decide the assertion.
	r, g, b, _ := thumbnail.At(15, 15).RGBA()
	require.Greater(t, b, r)
	require.Greater(t, b, g)
}

func TestImageThumbnailRejectsOversizedDimensionsBeforeDecode(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "thumbnail.jpg")
	_, err := CreateImageThumbnail(bytes.NewReader(pngHeader(10000, 5000)), destination)
	require.ErrorIs(t, err, apperrors.ErrTooLarge)
	require.NoFileExists(t, destination)
}

func TestInitRejectsManualStorageOverlapAndSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	require.NoError(t, os.MkdirAll(shared, 0700))
	conf := config.ManualsConfig{Enabled: true, StorageRoot: filepath.Join(shared, "manuals")}
	require.Error(t, Init(&conf, shared, ""))

	private := filepath.Join(root, "private")
	require.NoError(t, os.MkdirAll(private, 0700))
	alias := filepath.Join(root, "manual-link")
	require.NoError(t, os.Symlink(private, alias))
	conf.StorageRoot = alias
	require.Error(t, Init(&conf, "", private))
}

func insertEXIFOrientation(t *testing.T, jpegData []byte, orientation uint16) []byte {
	t.Helper()
	require.GreaterOrEqual(t, len(jpegData), 2)
	payload := make([]byte, 32)
	copy(payload, []byte("Exif\x00\x00II"))
	binary.LittleEndian.PutUint16(payload[8:10], 42)
	binary.LittleEndian.PutUint32(payload[10:14], 8)
	binary.LittleEndian.PutUint16(payload[14:16], 1)
	binary.LittleEndian.PutUint16(payload[16:18], 0x0112)
	binary.LittleEndian.PutUint16(payload[18:20], 3)
	binary.LittleEndian.PutUint32(payload[20:24], 1)
	binary.LittleEndian.PutUint16(payload[24:26], orientation)
	segment := []byte{0xff, 0xe1, 0, byte(len(payload) + 2)}
	segment = append(segment, payload...)
	result := append([]byte{}, jpegData[:2]...)
	result = append(result, segment...)
	return append(result, jpegData[2:]...)
}

func pngHeader(width, height uint32) []byte {
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8], data[9] = 8, 2
	chunk := append([]byte("IHDR"), data...)
	result := append([]byte{}, []byte("\x89PNG\r\n\x1a\n")...)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	result = append(result, length...)
	result = append(result, chunk...)
	checksum := make([]byte, 4)
	binary.BigEndian.PutUint32(checksum, crc32.ChecksumIEEE(chunk))
	return append(result, checksum...)
}
