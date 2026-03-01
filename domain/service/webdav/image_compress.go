package webdav

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strings"

	"golang.org/x/image/draw"
)

const (
	// SmallImageMaxSize 缩略图目标最大字节数 512KB
	SmallImageMaxSize = 512 * 1024
	// MaxOriginalSize 原文件超过此大小则跳过压缩（防 OOM）
	MaxOriginalSize = 50 * 1024 * 1024
	// maxDimension 任一维度超过此像素数则跳过压缩
	maxDimension = 20000
)

// CompressImage 将 src 中的图片数据压缩到 SmallImageMaxSize 以内。
// ext 为小写扩展名（含点号），如 ".jpg", ".jpeg", ".png"。
// originalSize 为原文件字节数，用于计算缩放比例。
func CompressImage(src io.Reader, originalSize int64, ext string) ([]byte, error) {
	if originalSize > MaxOriginalSize {
		return nil, fmt.Errorf("original file too large: %d bytes", originalSize)
	}

	ext = strings.ToLower(ext)
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		return nil, fmt.Errorf("unsupported extension: %s", ext)
	}

	data, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if cfg.Width > maxDimension || cfg.Height > maxDimension {
		return nil, fmt.Errorf("image dimensions too large: %dx%d", cfg.Width, cfg.Height)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// 综合文件大小和像素数量计算缩放比例
	// 文件大小比例
	fileRatio := math.Sqrt(float64(SmallImageMaxSize)/float64(originalSize)) * 0.85
	// 像素数量比例（以 JPEG quality 50 约 1.5 bytes/pixel 估算）
	pixelCount := float64(cfg.Width * cfg.Height)
	targetPixels := float64(SmallImageMaxSize) / 1.5
	pixelRatio := math.Sqrt(targetPixels / pixelCount)
	// 取更激进的比例
	ratio := math.Min(fileRatio, pixelRatio)
	if ratio > 1.0 {
		ratio = 1.0
	}

	bounds := img.Bounds()

	// 迭代缩放：如果首次编码仍超限，继续缩小
	for attempt := 0; attempt < 4; attempt++ {
		scaled := scaleImage(img, bounds, ratio)

		var result []byte
		switch ext {
		case ".jpg", ".jpeg":
			result, err = encodeJPEG(scaled)
		case ".png":
			result, err = encodePNG(scaled)
		}
		if err != nil {
			return nil, err
		}
		if len(result) <= SmallImageMaxSize {
			return result, nil
		}
		// 缩小 30% 再试
		ratio *= 0.7
	}

	// 最终尝试：强制 JPEG quality 30
	scaled := scaleImage(img, bounds, ratio)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 30}); err != nil {
		return nil, fmt.Errorf("jpeg encode final fallback: %w", err)
	}
	return buf.Bytes(), nil
}

func scaleImage(img image.Image, bounds image.Rectangle, ratio float64) image.Image {
	newWidth := int(float64(bounds.Dx()) * ratio)
	newHeight := int(float64(bounds.Dy()) * ratio)
	if newWidth < 1 {
		newWidth = 1
	}
	if newHeight < 1 {
		newHeight = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

// encodeJPEG 使用二分搜索 quality 使输出 <= SmallImageMaxSize
func encodeJPEG(img image.Image) ([]byte, error) {
	lo, hi := 30, 85
	var bestBuf []byte

	for lo <= hi {
		mid := (lo + hi) / 2
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: mid}); err != nil {
			return nil, fmt.Errorf("jpeg encode: %w", err)
		}
		if buf.Len() <= SmallImageMaxSize {
			bestBuf = buf.Bytes()
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	if bestBuf != nil {
		return bestBuf, nil
	}

	// quality=30 仍超大，交给调用方决定是否进一步缩放
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 30}); err != nil {
		return nil, fmt.Errorf("jpeg encode fallback: %w", err)
	}
	return buf.Bytes(), nil
}

// encodePNG 编码为 PNG；若仍超 SmallImageMaxSize 则转为 JPEG
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("png encode: %w", err)
	}
	if buf.Len() <= SmallImageMaxSize {
		return buf.Bytes(), nil
	}
	return encodeJPEG(img)
}
