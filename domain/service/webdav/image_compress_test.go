package webdav

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeJPEG 生成指定尺寸的测试 JPEG 图片
func makeJPEG(t *testing.T, width, height, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// 填充随机色彩以产生更真实的压缩大小
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 7 + y * 13) % 256),
				G: uint8((x * 11 + y * 3) % 256),
				B: uint8((x * 5 + y * 17) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}))
	return buf.Bytes()
}

// makePNG 生成指定尺寸的测试 PNG 图片
func makePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 7 + y * 13) % 256),
				G: uint8((x * 11 + y * 3) % 256),
				B: uint8((x * 5 + y * 17) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestCompressImage_JPEG(t *testing.T) {
	// 生成一个大 JPEG（约 1MB+）
	data := makeJPEG(t, 2000, 1500, 95)
	require.True(t, len(data) > SmallImageMaxSize, "test image should be > 512KB, got %d", len(data))

	result, err := CompressImage(bytes.NewReader(data), int64(len(data)), ".jpg")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), SmallImageMaxSize)
	assert.Greater(t, len(result), 0)

	// 验证输出仍然是合法的 JPEG
	_, err = jpeg.Decode(bytes.NewReader(result))
	assert.NoError(t, err)
}

func TestCompressImage_JPEG_Extension(t *testing.T) {
	data := makeJPEG(t, 2000, 1500, 95)
	require.True(t, len(data) > SmallImageMaxSize)

	result, err := CompressImage(bytes.NewReader(data), int64(len(data)), ".jpeg")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), SmallImageMaxSize)
}

func TestCompressImage_PNG(t *testing.T) {
	data := makePNG(t, 4000, 3000)
	require.True(t, len(data) > SmallImageMaxSize, "test PNG should be > 512KB, got %d", len(data))

	result, err := CompressImage(bytes.NewReader(data), int64(len(data)), ".png")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), SmallImageMaxSize)
	assert.Greater(t, len(result), 0)
}

func TestCompressImage_SmallImage(t *testing.T) {
	// 小图片（< 512KB）也能正常压缩
	data := makeJPEG(t, 100, 100, 50)
	require.True(t, len(data) < SmallImageMaxSize)

	result, err := CompressImage(bytes.NewReader(data), int64(len(data)), ".jpg")
	require.NoError(t, err)
	assert.Greater(t, len(result), 0)
}

func TestCompressImage_UnsupportedExtension(t *testing.T) {
	data := makeJPEG(t, 100, 100, 50)
	_, err := CompressImage(bytes.NewReader(data), int64(len(data)), ".gif")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported extension")
}

func TestCompressImage_TooLargeOriginal(t *testing.T) {
	_, err := CompressImage(bytes.NewReader(nil), MaxOriginalSize+1, ".jpg")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}
