package webdav

import (
	"bytes"
	"context"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	webdavService "github.com/mcoder2014/home_server/domain/service/webdav"
	"golang.org/x/net/webdav"
)

const smallPrefix = "small_"
const defaultMaxCacheSize = 256 * 1024 * 1024 // 256MB

// imageExtensions 支持的图片扩展名（小写）
var imageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

// ImageCache 缩略图内存缓存（LRU 淘汰）
type ImageCache struct {
	mu        sync.Mutex
	entries   map[string]*CacheEntry
	order     []string // 访问顺序，最近访问的在尾部
	totalSize int64    // 当前缓存总字节数
	maxSize   int64    // 最大容量（字节）
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Data    []byte
	ModTime time.Time
}

// NewImageCache 创建缓存实例，maxSize 为最大缓存字节数
func NewImageCache(maxSize int64) *ImageCache {
	if maxSize <= 0 {
		maxSize = defaultMaxCacheSize
	}
	return &ImageCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
	}
}

// Get 获取缓存数据，modTime 不匹配时视为失效
func (c *ImageCache) Get(path string, modTime time.Time) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[path]
	if !ok || !entry.ModTime.Equal(modTime) {
		return nil, false
	}
	// LRU：将 key 移到 order 尾部
	c.moveToBack(path)
	return entry.Data, true
}

// Put 存入缓存，超过 maxSize 时从头部淘汰最久未访问的条目
func (c *ImageCache) Put(path string, modTime time.Time, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果 key 已存在，先移除旧条目
	if old, ok := c.entries[path]; ok {
		c.totalSize -= int64(len(old.Data))
		c.removeFromOrder(path)
	}

	// 存入新条目
	c.entries[path] = &CacheEntry{Data: data, ModTime: modTime}
	c.totalSize += int64(len(data))
	c.order = append(c.order, path)

	// 淘汰最久未访问的条目直到满足容量限制
	for c.totalSize > c.maxSize && len(c.order) > 0 {
		evictKey := c.order[0]
		c.order = c.order[1:]
		if entry, ok := c.entries[evictKey]; ok {
			c.totalSize -= int64(len(entry.Data))
			delete(c.entries, evictKey)
		}
	}
}

// moveToBack 将 key 移到 order 尾部
func (c *ImageCache) moveToBack(key string) {
	c.removeFromOrder(key)
	c.order = append(c.order, key)
}

// removeFromOrder 从 order 中移除指定 key
func (c *ImageCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// SmallImageFS 包装 webdav.FileSystem，为大图片注入 small_ 缩略图虚拟文件
type SmallImageFS struct {
	underlying webdav.FileSystem
	cache      *ImageCache
}

// NewSmallImageFS 创建 SmallImageFS
func NewSmallImageFS(underlying webdav.FileSystem, cache *ImageCache) *SmallImageFS {
	return &SmallImageFS{
		underlying: underlying,
		cache:      cache,
	}
}

// isSmallImagePath 判断路径是否为 small_ 虚拟图片路径。
// 返回 true 时同时返回对应的原始文件路径。
func isSmallImagePath(name string) (originalPath string, ok bool) {
	dir, file := path.Split(name)
	if !strings.HasPrefix(file, smallPrefix) {
		return "", false
	}
	ext := strings.ToLower(path.Ext(file))
	if !imageExtensions[ext] {
		return "", false
	}
	originalFile := strings.TrimPrefix(file, smallPrefix)
	return path.Join(dir, originalFile), true
}

// isImageFile 检查文件名是否为支持的图片格式
func isImageFile(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return imageExtensions[ext]
}

// Mkdir 创建目录，虚拟路径拒绝操作
func (fs *SmallImageFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if _, ok := isSmallImagePath(name); ok {
		// 检查是否存在同名真实文件
		if _, err := fs.underlying.Stat(ctx, name); err == nil {
			return fs.underlying.Mkdir(ctx, name, perm)
		}
		return os.ErrPermission
	}
	return fs.underlying.Mkdir(ctx, name, perm)
}

// RemoveAll 删除，虚拟路径拒绝操作
func (fs *SmallImageFS) RemoveAll(ctx context.Context, name string) error {
	if _, ok := isSmallImagePath(name); ok {
		if _, err := fs.underlying.Stat(ctx, name); err == nil {
			return fs.underlying.RemoveAll(ctx, name)
		}
		return os.ErrPermission
	}
	return fs.underlying.RemoveAll(ctx, name)
}

// Rename 重命名，虚拟路径拒绝操作
func (fs *SmallImageFS) Rename(ctx context.Context, oldName, newName string) error {
	oldIsVirtual := false
	if _, ok := isSmallImagePath(oldName); ok {
		if _, err := fs.underlying.Stat(ctx, oldName); err != nil {
			oldIsVirtual = true
		}
	}
	newIsVirtual := false
	if _, ok := isSmallImagePath(newName); ok {
		if _, err := fs.underlying.Stat(ctx, newName); err != nil {
			newIsVirtual = true
		}
	}
	if oldIsVirtual || newIsVirtual {
		return os.ErrPermission
	}
	return fs.underlying.Rename(ctx, oldName, newName)
}

// Stat 获取文件信息
func (fs *SmallImageFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	originalPath, isVirtual := isSmallImagePath(name)
	if isVirtual {
		// 真实文件优先
		if info, err := fs.underlying.Stat(ctx, name); err == nil {
			return info, nil
		}
		// 查原始文件
		info, err := fs.underlying.Stat(ctx, originalPath)
		if err != nil {
			return nil, err
		}
		if info.IsDir() || info.Size() < webdavService.SmallImageMaxSize || info.Size() > webdavService.MaxOriginalSize {
			return nil, os.ErrNotExist
		}
		return newVirtualFileInfo(name, info), nil
	}
	return fs.underlying.Stat(ctx, name)
}

// OpenFile 打开文件
func (fs *SmallImageFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	originalPath, isVirtual := isSmallImagePath(name)
	if isVirtual {
		// 真实文件优先
		if f, err := fs.underlying.OpenFile(ctx, name, flag, perm); err == nil {
			return f, nil
		}
		// 拒绝写操作
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
			return nil, os.ErrPermission
		}
		// 获取原文件信息
		origInfo, err := fs.underlying.Stat(ctx, originalPath)
		if err != nil {
			return nil, err
		}
		if origInfo.IsDir() || origInfo.Size() < webdavService.SmallImageMaxSize || origInfo.Size() > webdavService.MaxOriginalSize {
			return nil, os.ErrNotExist
		}
		// 查缓存
		if data, ok := fs.cache.Get(originalPath, origInfo.ModTime()); ok {
			vInfo := newVirtualFileInfo(name, origInfo)
			return newSmallImageVirtualFile(data, vInfo), nil
		}
		// 压缩原图
		origFile, err := fs.underlying.OpenFile(ctx, originalPath, os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
		defer origFile.Close()

		ext := strings.ToLower(path.Ext(originalPath))
		compressed, err := webdavService.CompressImage(origFile, origInfo.Size(), ext)
		if err != nil {
			return nil, err
		}
		fs.cache.Put(originalPath, origInfo.ModTime(), compressed)

		vInfo := newVirtualFileInfo(name, origInfo)
		return newSmallImageVirtualFile(compressed, vInfo), nil
	}

	// 非虚拟路径
	f, err := fs.underlying.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	// 如果是目录，包装为 SmallImageDirFile 以注入虚拟条目
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		return &SmallImageDirFile{File: f, fs: fs, ctx: ctx}, nil
	}
	return f, nil
}

// --- SmallImageDirFile ---

// SmallImageDirFile 包装目录文件，在 Readdir 中注入虚拟缩略图条目
type SmallImageDirFile struct {
	webdav.File
	fs  *SmallImageFS
	ctx context.Context
}

// Readdir 读取目录条目并注入虚拟缩略图
func (d *SmallImageDirFile) Readdir(count int) ([]os.FileInfo, error) {
	entries, err := d.File.Readdir(count)
	if err != nil {
		return entries, err
	}

	// 构建已有文件名集合，用于检测 small_ 同名冲突
	nameSet := make(map[string]bool, len(entries))
	for _, e := range entries {
		nameSet[e.Name()] = true
	}

	var virtual []os.FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !isImageFile(e.Name()) {
			continue
		}
		if e.Size() < webdavService.SmallImageMaxSize || e.Size() > webdavService.MaxOriginalSize {
			continue
		}
		smallName := smallPrefix + e.Name()
		if nameSet[smallName] {
			continue
		}
		virtual = append(virtual, &virtualFileInfo{
			name:    smallName,
			size:    e.Size() / 4, // 估算大小
			modTime: e.ModTime(),
		})
	}

	return append(entries, virtual...), nil
}

// --- SmallImageVirtualFile ---

// SmallImageVirtualFile 内存中的只读虚拟文件
type SmallImageVirtualFile struct {
	*bytes.Reader
	info os.FileInfo
}

func newSmallImageVirtualFile(data []byte, info os.FileInfo) *SmallImageVirtualFile {
	return &SmallImageVirtualFile{
		Reader: bytes.NewReader(data),
		info:   info,
	}
}

func (f *SmallImageVirtualFile) Close() error {
	return nil
}

func (f *SmallImageVirtualFile) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *SmallImageVirtualFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

func (f *SmallImageVirtualFile) Stat() (os.FileInfo, error) {
	return f.info, nil
}

// --- virtualFileInfo ---

type virtualFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func newVirtualFileInfo(fullPath string, origInfo os.FileInfo) *virtualFileInfo {
	_, file := path.Split(fullPath)
	return &virtualFileInfo{
		name:    file,
		size:    origInfo.Size() / 4,
		modTime: origInfo.ModTime(),
	}
}

func (i *virtualFileInfo) Name() string      { return i.name }
func (i *virtualFileInfo) Size() int64        { return i.size }
func (i *virtualFileInfo) Mode() os.FileMode  { return 0444 }
func (i *virtualFileInfo) ModTime() time.Time { return i.modTime }
func (i *virtualFileInfo) IsDir() bool        { return false }
func (i *virtualFileInfo) Sys() interface{}   { return nil }

