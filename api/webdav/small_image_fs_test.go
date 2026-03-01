package webdav

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"
)

// makeTestJPEG 生成指定尺寸的测试 JPEG 数据
func makeTestJPEG(t *testing.T, width, height, quality int) []byte {
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
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}))
	return buf.Bytes()
}

// setupTestDir 创建临时目录并写入测试文件
func setupTestDir(t *testing.T) (string, *SmallImageFS) {
	t.Helper()
	dir := t.TempDir()

	// 创建大 JPEG（> 512KB）
	bigJPEG := makeTestJPEG(t, 2000, 1500, 95)
	require.True(t, len(bigJPEG) > 512*1024, "big JPEG should be > 512KB, got %d", len(bigJPEG))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "photo.jpg"), bigJPEG, 0644))

	// 创建小 JPEG（< 512KB）
	smallJPEG := makeTestJPEG(t, 100, 100, 50)
	require.True(t, len(smallJPEG) < 512*1024)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tiny.jpg"), smallJPEG, 0644))

	// 创建文本文件
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644))

	// 创建一个真实的 small_ 文件（真实文件优先场景）
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small_real.jpg"), smallJPEG, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.jpg"), bigJPEG, 0644))

	cache := NewImageCache(defaultMaxCacheSize)
	fs := NewSmallImageFS(webdav.Dir(dir), cache)
	return dir, fs
}

func TestIsSmallImagePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOrig string
		wantOK   bool
	}{
		{"virtual jpg", "/photos/small_test.jpg", "/photos/test.jpg", true},
		{"virtual jpeg", "/small_photo.jpeg", "/photo.jpeg", true},
		{"virtual png", "/dir/small_img.png", "/dir/img.png", true},
		{"not virtual - no prefix", "/test.jpg", "", false},
		{"not virtual - wrong ext", "/small_test.gif", "", false},
		{"not virtual - dir name", "/small_dir/test.jpg", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig, ok := isSmallImagePath(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantOrig, orig)
			}
		})
	}
}

func TestStat_VirtualFile(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	// 大图片的 small_ 虚拟文件应该存在
	info, err := fs.Stat(ctx, "/small_photo.jpg")
	require.NoError(t, err)
	assert.Equal(t, "small_photo.jpg", info.Name())
	assert.False(t, info.IsDir())
}

func TestStat_SmallImageNoVirtual(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	// 小图片不应生成虚拟文件
	_, err := fs.Stat(ctx, "/small_tiny.jpg")
	assert.True(t, os.IsNotExist(err))
}

func TestStat_RealFile(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	// 真实文件正常访问
	info, err := fs.Stat(ctx, "/photo.jpg")
	require.NoError(t, err)
	assert.Equal(t, "photo.jpg", info.Name())
}

func TestStat_RealFileOverridesVirtual(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	// 真实的 small_real.jpg 文件应优先于虚拟路径
	info, err := fs.Stat(ctx, "/small_real.jpg")
	require.NoError(t, err)
	assert.Equal(t, "small_real.jpg", info.Name())
	// 应该返回真实文件的大小，不是虚拟估算大小
}

func TestOpenFile_VirtualRead(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	f, err := fs.OpenFile(ctx, "/small_photo.jpg", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Greater(t, len(data), 0)
	assert.LessOrEqual(t, len(data), 512*1024)

	// 验证输出是合法 JPEG
	_, err = jpeg.Decode(bytes.NewReader(data))
	assert.NoError(t, err)
}

func TestOpenFile_VirtualWriteRejected(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	_, err := fs.OpenFile(ctx, "/small_photo.jpg", os.O_WRONLY, 0644)
	assert.ErrorIs(t, err, os.ErrPermission)
}

func TestOpenFile_Directory(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	f, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	entries, err := f.Readdir(-1)
	require.NoError(t, err)

	nameSet := make(map[string]bool)
	for _, e := range entries {
		nameSet[e.Name()] = true
	}

	// 应包含大图片的虚拟缩略图
	assert.True(t, nameSet["small_photo.jpg"], "should have virtual small_photo.jpg")
	// 小图片不应有虚拟缩略图
	assert.False(t, nameSet["small_tiny.jpg"], "should not have virtual small_tiny.jpg")
	// 文本文件不应有虚拟缩略图
	assert.False(t, nameSet["small_readme.txt"], "should not have virtual small_readme.txt")
	// 真实 small_real.jpg 应存在（真实文件）
	assert.True(t, nameSet["small_real.jpg"], "should have real small_real.jpg")
	// 由于 real.jpg 是大图片且已存在真实 small_real.jpg，不应重复注入
	// （注意：此处 small_real.jpg 的原始文件是 real.jpg，但真实文件已存在同名）
}

func TestRemoveAll_VirtualRejected(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	err := fs.RemoveAll(ctx, "/small_photo.jpg")
	assert.ErrorIs(t, err, os.ErrPermission)
}

func TestRename_VirtualRejected(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	err := fs.Rename(ctx, "/small_photo.jpg", "/other.jpg")
	assert.ErrorIs(t, err, os.ErrPermission)
}

func TestImageCache(t *testing.T) {
	cache := NewImageCache(defaultMaxCacheSize)
	now := time.Now()

	// 缓存未命中
	_, ok := cache.Get("/test.jpg", now)
	assert.False(t, ok)

	// 存入
	cache.Put("/test.jpg", now, []byte("data"))
	data, ok := cache.Get("/test.jpg", now)
	assert.True(t, ok)
	assert.Equal(t, []byte("data"), data)

	// ModTime 不匹配则失效
	_, ok = cache.Get("/test.jpg", now.Add(time.Second))
	assert.False(t, ok)
}

func TestOpenFile_CacheHit(t *testing.T) {
	_, fs := setupTestDir(t)
	ctx := context.Background()

	// 第一次访问，触发压缩
	f1, err := fs.OpenFile(ctx, "/small_photo.jpg", os.O_RDONLY, 0)
	require.NoError(t, err)
	data1, err := io.ReadAll(f1)
	require.NoError(t, err)
	f1.Close()

	// 第二次访问，应命中缓存
	f2, err := fs.OpenFile(ctx, "/small_photo.jpg", os.O_RDONLY, 0)
	require.NoError(t, err)
	data2, err := io.ReadAll(f2)
	require.NoError(t, err)
	f2.Close()

	assert.Equal(t, data1, data2)
}

func TestImageCache_Eviction(t *testing.T) {
	// maxSize = 100 字节
	cache := NewImageCache(100)
	now := time.Now()

	// 存入 60 字节
	cache.Put("/a.jpg", now, make([]byte, 60))
	_, ok := cache.Get("/a.jpg", now)
	assert.True(t, ok)

	// 再存入 60 字节，总共 120 > 100，应淘汰 /a.jpg
	cache.Put("/b.jpg", now, make([]byte, 60))
	_, ok = cache.Get("/a.jpg", now)
	assert.False(t, ok, "/a.jpg should be evicted")
	_, ok = cache.Get("/b.jpg", now)
	assert.True(t, ok, "/b.jpg should still exist")
}

func TestImageCache_LRUOrder(t *testing.T) {
	// maxSize = 150 字节
	cache := NewImageCache(150)
	now := time.Now()

	// 存入 a(50), b(50), c(50)，总共 150，刚好不淘汰
	cache.Put("/a.jpg", now, make([]byte, 50))
	cache.Put("/b.jpg", now, make([]byte, 50))
	cache.Put("/c.jpg", now, make([]byte, 50))

	// 访问 /a.jpg，使其变为最近访问
	_, ok := cache.Get("/a.jpg", now)
	assert.True(t, ok)

	// 存入 d(50)，总共 200 > 150，应淘汰最久未访问的 /b.jpg
	cache.Put("/d.jpg", now, make([]byte, 50))

	_, ok = cache.Get("/a.jpg", now)
	assert.True(t, ok, "/a.jpg should survive (recently accessed)")
	_, ok = cache.Get("/b.jpg", now)
	assert.False(t, ok, "/b.jpg should be evicted (least recently used)")
	_, ok = cache.Get("/c.jpg", now)
	assert.True(t, ok, "/c.jpg should survive")
	_, ok = cache.Get("/d.jpg", now)
	assert.True(t, ok, "/d.jpg should exist")
}

func TestImageCache_UpdateExisting(t *testing.T) {
	cache := NewImageCache(100)
	now := time.Now()

	// 存入 80 字节
	cache.Put("/a.jpg", now, make([]byte, 80))
	assert.Equal(t, int64(80), cache.totalSize)

	// 更新为 50 字节
	cache.Put("/a.jpg", now, make([]byte, 50))
	assert.Equal(t, int64(50), cache.totalSize)

	_, ok := cache.Get("/a.jpg", now)
	assert.True(t, ok)
}
