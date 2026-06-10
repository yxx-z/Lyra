package transcode

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Cache 管理磁盘转码缓存：路径、per-key 锁、LRU 容量回收。
type Cache struct {
	dir      string
	maxBytes int64 // 0 = 不限
	mu       sync.Mutex
	// inflight：cache key → 锁。规模受 (trackID,codec,bitrate) 组合数约束（≈ 库规模），
	// 不随请求数增长，条目不回收，在此规模可接受。
	inflight map[string]*sync.Mutex
}

// NewCache 创建根于 dir 的缓存；maxSizeMB ≤0 表示不限容量。
func NewCache(dir string, maxSizeMB int) *Cache {
	return &Cache{
		dir:      dir,
		maxBytes: int64(maxSizeMB) * 1024 * 1024,
		inflight: make(map[string]*sync.Mutex),
	}
}

// Path 返回缓存文件路径；trackID 取 base 以防越出缓存目录。
func (c *Cache) Path(trackID, codec string, bitrate int) string {
	name := fmt.Sprintf("%s_%s_%dk.%s", filepath.Base(trackID), codec, bitrate, codecFor(codec).Ext)
	return filepath.Join(c.dir, name)
}

func (c *Cache) key(trackID, codec string, bitrate int) string {
	return fmt.Sprintf("%s_%s_%dk", filepath.Base(trackID), codec, bitrate)
}

// lockFor 返回某 key 的锁，惰性创建。
func (c *Cache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.inflight[key]; ok {
		return m
	}
	m := &sync.Mutex{}
	c.inflight[key] = m
	return m
}

// touch 把文件 mtime 刷成当前，作为 LRU 的"最近访问"近似。
func (c *Cache) touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

// evict 若缓存目录总大小超上限，按 mtime 最旧优先删除，删到 ≤ 上限。
// keep 为本次刚写入、需跳过的文件路径（传 "" 表示无）。
func (c *Cache) evict(keep string) {
	if c.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type fileInfo struct {
		path string
		size int64
		mod  time.Time
	}
	var files []fileInfo
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{filepath.Join(c.dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= c.maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if f.path == keep {
			continue
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}
