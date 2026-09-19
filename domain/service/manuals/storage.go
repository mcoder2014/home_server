package manuals

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	apperrors "github.com/mcoder2014/home_server/errors"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type StoredFile struct {
	Kind          model.ManualItemKind
	Text          string
	StorageKey    string
	ThumbnailKey  string
	ContentType   string
	OriginalName  string
	SizeBytes     int64
	SHA256        string
	PreviewStatus model.ManualPreviewStatus
}

var previewState = struct {
	sync.RWMutex
	gate chan struct{}
}{gate: make(chan struct{}, 1)}

func configurePreviewGate(limit int) {
	previewState.Lock()
	previewState.gate = make(chan struct{}, limit)
	previewState.Unlock()
}

// StoreFile streams an original into a same-filesystem staging directory,
// validates actual bytes, creates a stripped thumbnail, then atomically moves
// the complete item directory into its service-generated final location.
func StoreFile(conf *config.ManualsConfig, ownerID, manualID, itemID int64, originalName string, source io.Reader) (*StoredFile, error) {
	if conf == nil || !conf.Enabled || ownerID <= 0 || manualID <= 0 || itemID <= 0 {
		return nil, apperrors.ErrDependency
	}
	name, err := validateOriginalName(originalName)
	if err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(filepath.Join(conf.StorageRoot, ".staging"), "item-")
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	defer os.RemoveAll(staging)
	originalPath := filepath.Join(staging, "original")
	size, digest, err := writeOriginal(originalPath, source, conf.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	stored, err := inspectStoredFile(conf, originalPath, filepath.Join(staging, "thumbnail.jpg"), name)
	if err != nil {
		return nil, err
	}
	stored.OriginalName, stored.SizeBytes, stored.SHA256 = name, size, digest
	if stored.Kind == model.ManualItemText {
		return stored, nil
	}
	itemKey := path.Join(strconv.FormatInt(ownerID, 10), strconv.FormatInt(manualID, 10), strconv.FormatInt(itemID, 10))
	finalParent, err := ensureManualDirectory(conf.StorageRoot, strconv.FormatInt(ownerID, 10), strconv.FormatInt(manualID, 10))
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	finalDirectory := filepath.Join(finalParent, strconv.FormatInt(itemID, 10))
	if _, err := os.Lstat(finalDirectory); err == nil {
		return nil, apperrors.ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, apperrors.ErrDependency
	}
	if err := os.Rename(staging, finalDirectory); err != nil {
		return nil, apperrors.ErrDependency
	}
	if err := syncDirectory(finalDirectory); err != nil {
		_ = os.RemoveAll(finalDirectory)
		return nil, apperrors.ErrDependency
	}
	if err := syncDirectory(finalParent); err != nil {
		_ = os.RemoveAll(finalDirectory)
		return nil, apperrors.ErrDependency
	}
	stored.StorageKey = path.Join(itemKey, "original")
	if stored.PreviewStatus == model.ManualPreviewReady {
		stored.ThumbnailKey = path.Join(itemKey, "thumbnail.jpg")
	}
	return stored, nil
}

func validateOriginalName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !validRunes(name, MaxOriginalNameRunes) || strings.ContainsAny(name, "\x00\r\n") {
		return "", apperrors.ErrInvalid
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return "", apperrors.ErrInvalid
	}
	return name, nil
}

func writeOriginal(target string, source io.Reader, limit int64) (int64, string, error) {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, "", apperrors.ErrDependency
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, limit+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return 0, "", apperrors.ErrDependency
	}
	if written == 0 {
		return 0, "", apperrors.ErrUnprocessable
	}
	if written > limit {
		return 0, "", apperrors.ErrTooLarge
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectStoredFile(conf *config.ManualsConfig, originalPath, thumbnailPath, originalName string) (*StoredFile, error) {
	header, err := filePrefix(originalPath, 512)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	if bytes.HasPrefix(header, []byte("%PDF-")) {
		status := model.ManualPreviewUnavailable
		if renderPDFPreview(conf, originalPath, thumbnailPath) == nil {
			status = model.ManualPreviewReady
		}
		return &StoredFile{Kind: model.ManualItemPDF, ContentType: "application/pdf", PreviewStatus: status}, nil
	}
	file, err := os.Open(originalPath)
	if err != nil {
		return nil, apperrors.ErrDependency
	}
	_, format, imageErr := image.DecodeConfig(file)
	_ = file.Close()
	if imageErr == nil && imageContentType(format) != "" {
		contentType, thumbErr := createImageThumbnailFile(originalPath, thumbnailPath)
		if thumbErr != nil {
			return nil, thumbErr
		}
		return &StoredFile{Kind: model.ManualItemImage, ContentType: contentType, PreviewStatus: model.ManualPreviewReady}, nil
	}
	if strings.HasPrefix(http.DetectContentType(header), "image/") {
		return nil, apperrors.ErrUnprocessable
	}
	if strings.ToLower(filepath.Ext(originalName)) != ".txt" {
		return nil, apperrors.ErrUnsupported
	}
	text, textErr := readPlainText(originalPath)
	if textErr == nil {
		return &StoredFile{Kind: model.ManualItemText, Text: text, ContentType: "text/plain; charset=utf-8", PreviewStatus: model.ManualPreviewNone}, nil
	}
	return nil, textErr
}

func filePrefix(name string, limit int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}

func readPlainText(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", apperrors.ErrDependency
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxTextRunes*4+1))
	if err != nil {
		return "", apperrors.ErrDependency
	}
	if len(data) > MaxTextRunes*4 || !utf8.Valid(data) || utf8.RuneCount(data) > MaxTextRunes || bytes.IndexByte(data, 0) >= 0 {
		return "", apperrors.ErrUnsupported
	}
	return string(data), nil
}

func CreateImageThumbnail(source io.Reader, destination string) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), "image-source-")
	if err != nil {
		return "", apperrors.ErrDependency
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err = io.Copy(temporary, source); err != nil || temporary.Close() != nil {
		return "", apperrors.ErrDependency
	}
	return createImageThumbnailFile(name, destination)
}

func createImageThumbnailFile(source, destination string) (string, error) {
	previewState.RLock()
	gate := previewState.gate
	previewState.RUnlock()
	gate <- struct{}{}
	defer func() {
		<-gate
	}()
	return writeImageThumbnailFile(source, destination)
}

// writeImageThumbnailFile decodes, bounds and re-encodes an image while its
// caller holds the shared preview gate.
func writeImageThumbnailFile(source, destination string) (string, error) {
	file, err := os.Open(source)
	if err != nil {
		return "", apperrors.ErrDependency
	}
	reader := bufio.NewReader(file)
	orientation := jpegOrientation(reader)
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return "", apperrors.ErrDependency
	}
	imageConfig, format, err := image.DecodeConfig(file)
	contentType := imageContentType(format)
	if err != nil || contentType == "" {
		_ = file.Close()
		return "", apperrors.ErrUnprocessable
	}
	width, height := int64(imageConfig.Width), int64(imageConfig.Height)
	if width <= 0 || height <= 0 || width > MaxImagePixels/height {
		_ = file.Close()
		return "", apperrors.WithMessage(apperrors.ErrTooLarge, "image pixel count exceeds limit")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return "", apperrors.ErrDependency
	}
	decoded, decodedFormat, err := image.Decode(file)
	_ = file.Close()
	if err != nil || decodedFormat != format {
		return "", apperrors.ErrUnprocessable
	}
	bounds := decoded.Bounds()
	width, height = int64(bounds.Dx()), int64(bounds.Dy())
	if width <= 0 || height <= 0 || width > MaxImagePixels/height {
		return "", apperrors.WithMessage(apperrors.ErrTooLarge, "image pixel count exceeds limit")
	}
	oriented := orientImage(decoded, orientation)
	targetWidth, targetHeight := thumbnailSize(oriented.Bounds().Dx(), oriented.Bounds().Dy())
	thumbnail := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), oriented, oriented.Bounds(), draw.Over, nil)
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", apperrors.ErrDependency
	}
	encodeErr := jpeg.Encode(output, thumbnail, &jpeg.Options{Quality: 88})
	syncErr := output.Sync()
	closeErr := output.Close()
	if encodeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return "", apperrors.ErrDependency
	}
	return contentType, nil
}

func imageContentType(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func thumbnailSize(width, height int) (int, int) {
	if width <= ThumbnailMaxEdge && height <= ThumbnailMaxEdge {
		return width, height
	}
	if width >= height {
		return ThumbnailMaxEdge, maxInt(1, height*ThumbnailMaxEdge/width)
	}
	return maxInt(1, width*ThumbnailMaxEdge/height), ThumbnailMaxEdge
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

type oriented struct {
	image.Image
	orientation int
	bounds      image.Rectangle
}

func orientImage(source image.Image, orientation int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if orientation < 2 || orientation > 8 {
		return source
	}
	if orientation >= 5 {
		width, height = height, width
	}
	return &oriented{Image: source, orientation: orientation, bounds: image.Rect(0, 0, width, height)}
}

func (value *oriented) Bounds() image.Rectangle {
	return value.bounds
}

func (value *oriented) At(x, y int) color.Color {
	source := value.Image.Bounds()
	w, h := source.Dx(), source.Dy()
	var sourceX, sourceY int
	switch value.orientation {
	case 2:
		sourceX, sourceY = w-1-x, y
	case 3:
		sourceX, sourceY = w-1-x, h-1-y
	case 4:
		sourceX, sourceY = x, h-1-y
	case 5:
		sourceX, sourceY = y, x
	case 6:
		sourceX, sourceY = y, h-1-x
	case 7:
		sourceX, sourceY = w-1-y, h-1-x
	case 8:
		sourceX, sourceY = w-1-y, x
	default:
		sourceX, sourceY = x, y
	}
	return value.Image.At(source.Min.X+sourceX, source.Min.Y+sourceY)
}

func jpegOrientation(reader *bufio.Reader) int {
	const maxMetadataBytes = 1 << 20
	var signature [2]byte
	if _, err := io.ReadFull(reader, signature[:]); err != nil || signature != [2]byte{0xff, 0xd8} {
		return 1
	}
	scanned := len(signature)
	for scanned < maxMetadataBytes {
		prefix, err := reader.ReadByte()
		scanned++
		if err != nil || prefix != 0xff {
			break
		}
		marker, err := reader.ReadByte()
		scanned++
		if err != nil {
			break
		}
		for marker == 0xff && scanned < maxMetadataBytes {
			marker, err = reader.ReadByte()
			scanned++
			if err != nil {
				return 1
			}
		}
		if marker == 0x00 {
			break
		}
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		var lengthBytes [2]byte
		if _, err = io.ReadFull(reader, lengthBytes[:]); err != nil {
			break
		}
		scanned += len(lengthBytes)
		length := int(binary.BigEndian.Uint16(lengthBytes[:]))
		if length < 2 {
			break
		}
		payloadLength := length - 2
		if payloadLength > maxMetadataBytes-scanned {
			break
		}
		if marker == 0xe1 {
			payload := make([]byte, payloadLength)
			if _, err = io.ReadFull(reader, payload); err != nil {
				break
			}
			scanned += payloadLength
			if orientation := parseEXIFOrientation(payload); orientation != 1 {
				return orientation
			}
			continue
		}
		if _, err = io.CopyN(io.Discard, reader, int64(payloadLength)); err != nil {
			break
		}
		scanned += payloadLength
	}
	return 1
}

func parseEXIFOrientation(data []byte) int {
	if len(data) < 14 || !bytes.Equal(data[:6], []byte("Exif\x00\x00")) {
		return 1
	}
	tiff := data[6:]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 8 || offset+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for index := 0; index < count; index++ {
		entry := offset + 2 + index*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 && order.Uint16(tiff[entry+2:entry+4]) == 3 && order.Uint32(tiff[entry+4:entry+8]) == 1 {
			orientation := int(order.Uint16(tiff[entry+8 : entry+10]))
			if orientation >= 1 && orientation <= 8 {
				return orientation
			}
		}
	}
	return 1
}

func renderPDFPreview(conf *config.ManualsConfig, source, destination string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(conf.PDFPreviewTimeoutSeconds)*time.Second)
	defer cancel()
	previewState.RLock()
	gate := previewState.gate
	previewState.RUnlock()
	select {
	case gate <- struct{}{}:
		defer func() {
			<-gate
		}()
	case <-ctx.Done():
		return ctx.Err()
	}
	prefix := strings.TrimSuffix(destination, filepath.Ext(destination))
	args := []string{
		"--as=" + strconv.FormatInt(conf.PDFPreviewMemoryLimitBytes, 10),
		"--cpu=" + strconv.Itoa(conf.PDFPreviewCPUSeconds),
		"--fsize=" + strconv.FormatInt(conf.PDFPreviewOutputLimitBytes, 10),
		"--nofile=" + strconv.Itoa(conf.PDFPreviewOpenFilesLimit), "--", conf.PDFToPPMPath,
		"-f", "1", "-singlefile", "-scale-to", strconv.Itoa(ThumbnailMaxEdge), "-jpeg", "-jpegopt", "quality=88", source, prefix,
	}
	if err := exec.CommandContext(ctx, conf.PRLimitPath, args...).Run(); err != nil {
		_ = os.Remove(destination)
		return err
	}
	info, err := os.Stat(destination)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > conf.PDFPreviewOutputLimitBytes {
		_ = os.Remove(destination)
		return fmt.Errorf("invalid PDF preview output")
	}
	normalized := destination + ".normalized"
	defer os.Remove(normalized)
	contentType, err := writeImageThumbnailFile(destination, normalized)
	if err != nil || contentType != "image/jpeg" {
		_ = os.Remove(destination)
		return fmt.Errorf("invalid PDF preview image")
	}
	info, err = os.Stat(normalized)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > conf.PDFPreviewOutputLimitBytes {
		_ = os.Remove(destination)
		return fmt.Errorf("invalid normalized PDF preview")
	}
	if err = os.Rename(normalized, destination); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func ensureManualDirectory(root string, components ...string) (string, error) {
	current := root
	for _, component := range components {
		current = filepath.Join(current, component)
		mkdirErr := os.Mkdir(current, 0700)
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return "", mkdirErr
		}
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("manual storage ancestry is invalid")
		}
		if mkdirErr == nil {
			if err := syncDirectory(filepath.Dir(current)); err != nil {
				return "", err
			}
		}
	}
	return current, nil
}

func syncDirectory(directoryPath string) error {
	directory, err := os.Open(directoryPath)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func RemoveStoredFile(conf *config.ManualsConfig, ownerID, manualID, itemID int64) error {
	if conf == nil || ownerID <= 0 || manualID <= 0 || itemID <= 0 {
		return apperrors.ErrInvalid
	}
	target := filepath.Join(conf.StorageRoot, strconv.FormatInt(ownerID, 10), strconv.FormatInt(manualID, 10), strconv.FormatInt(itemID, 10))
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}

func OpenStoredFile(conf *config.ManualsConfig, key string) (*os.File, os.FileInfo, error) {
	if conf == nil || !validStorageKey(key) {
		return nil, nil, apperrors.ErrNotFound
	}
	root, err := filepath.EvalSymlinks(conf.StorageRoot)
	if err != nil {
		return nil, nil, apperrors.ErrDependency
	}
	target := filepath.Join(conf.StorageRoot, filepath.FromSlash(key))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil || (resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator))) {
		return nil, nil, apperrors.ErrNotFound
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, nil, apperrors.ErrNotFound
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, apperrors.ErrNotFound
	}
	return file, info, nil
}

func validStorageKey(key string) bool {
	if key == "" || path.Clean(key) != key || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		return false
	}
	parts := strings.Split(key, "/")
	if len(parts) != 4 || (parts[3] != "original" && parts[3] != "thumbnail.jpg") {
		return false
	}
	for _, value := range parts[:3] {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
			return false
		}
	}
	return true
}

func DiskFreeBytes(root string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 || uint64(stat.Bavail) > math.MaxUint64/uint64(stat.Bsize) {
		return 0, fmt.Errorf("invalid filesystem free-space result")
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
