package sshx

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
)

// DirCacheNode 目录缓存节点，存储单个路径下的文件列表和更新时间。
type DirCacheNode struct {
	Path      string          `json:"path"`
	Entries   []models.SFTPEntry `json:"entries"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// entryDiff 计算两个目录列表之间的差异。
type entryDiff struct {
	Added   []models.SFTPEntry `json:"added"`
	Removed []models.SFTPEntry `json:"removed"`
	Changed []models.SFTPEntry `json:"changed"`
}

// SFTPCacheManager SWR (Stale-While-Revalidate) 目录缓存管理器。
// 使用 sync.Map 存储内存缓存，提供即时读取 + 后台异步校验。
type SFTPCacheManager struct {
	tree    sync.Map // map[string]*DirCacheNode
	ttl     time.Duration
	enabled bool
}

// NewSFTPCacheManager 创建 SWR 缓存管理器，默认启用，TTL 30 秒。
func NewSFTPCacheManager() *SFTPCacheManager {
	return &SFTPCacheManager{
		ttl:     30 * time.Second,
		enabled: true,
	}
}

// Get 从缓存中获取目录列表，返回缓存数据和是否命中。
func (c *SFTPCacheManager) Get(path string) (*DirCacheNode, bool) {
	if !c.enabled {
		return nil, false
	}
	val, ok := c.tree.Load(normalizePath(path))
	if !ok {
		return nil, false
	}
	node := val.(*DirCacheNode)
	return node, true
}

// Set 将目录列表写入缓存。
func (c *SFTPCacheManager) Set(path string, entries []models.SFTPEntry) {
	if !c.enabled {
		return
	}
	path = normalizePath(path)
	// 排序后存储
	sorted := make([]models.SFTPEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsDir != sorted[j].IsDir {
			return sorted[i].IsDir
		}
		return sorted[i].Name < sorted[j].Name
	})
	c.tree.Store(path, &DirCacheNode{
		Path:      path,
		Entries:   sorted,
		UpdatedAt: time.Now(),
	})
}

// Invalidate 使指定路径的缓存失效。
func (c *SFTPCacheManager) Invalidate(path string) {
	if !c.enabled {
		return
	}
	c.tree.Delete(normalizePath(path))
}

// Clear 清空全部缓存。
func (c *SFTPCacheManager) Clear() {
	c.tree.Range(func(key, _ interface{}) bool {
		c.tree.Delete(key)
		return true
	})
}

// Revalidate 后台异步校验缓存：执行 ReadDir 并与缓存比对，返回差异。
// 如果缓存为空或不存在，直接填充并返回 nil diff。
func (c *SFTPCacheManager) Revalidate(path string, readDir func(string) ([]models.SFTPEntry, error)) (*entryDiff, error) {
	path = normalizePath(path)
	entries, err := readDir(path)
	if err != nil {
		return nil, err
	}

	// 排序以便比较
	sorted := make([]models.SFTPEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsDir != sorted[j].IsDir {
			return sorted[i].IsDir
		}
		return sorted[i].Name < sorted[j].Name
	})

	oldVal, existed := c.tree.Load(path)
	c.Set(path, sorted)

	if !existed {
		return nil, nil // 首次缓存，无需 diff
	}

	oldNode := oldVal.(*DirCacheNode)
	diff := computeDiff(oldNode.Entries, sorted)
	return &diff, nil
}

// computeDiff 计算两个已排序列表的差异。
func computeDiff(old, new []models.SFTPEntry) entryDiff {
	var diff entryDiff
	oldMap := make(map[string]models.SFTPEntry, len(old))
	for _, e := range old {
		oldMap[e.Name] = e
	}
	newMap := make(map[string]models.SFTPEntry, len(new))
	for _, e := range new {
		newMap[e.Name] = e
	}

	// 找出新增和变更的条目
	for name, ne := range newMap {
		oe, existed := oldMap[name]
		if !existed {
			diff.Added = append(diff.Added, ne)
		} else if oe.Size != ne.Size || oe.ModTime != ne.ModTime {
			diff.Changed = append(diff.Changed, ne)
		}
	}

	// 找出删除的条目
	for name, oe := range oldMap {
		if _, existed := newMap[name]; !existed {
			diff.Removed = append(diff.Removed, oe)
		}
	}

	return diff
}

// Has 检查缓存中是否存在指定路径。
func (c *SFTPCacheManager) Has(path string) bool {
	_, ok := c.tree.Load(normalizePath(path))
	return ok
}

// SetEnabled 启用/禁用缓存。
func (c *SFTPCacheManager) SetEnabled(enabled bool) {
	c.enabled = enabled
	if !enabled {
		c.Clear()
	}
}

// normalizePath 规范化目录路径，确保以 / 结尾。
func normalizePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

// SWRReadDir 实现 SWR 读取模式：
// 1. 优先从缓存返回（立即）
// 2. 启动后台 Goroutine 执行 Revalidate
// 3. 通过回调通知差异
func (c *SFTPCacheManager) SWRReadDir(
	ctx context.Context,
	path string,
	readDir func(string) ([]models.SFTPEntry, error),
	onCacheHit func(entries []models.SFTPEntry),
	onDiff func(diff *entryDiff, entries []models.SFTPEntry),
) {
	path = normalizePath(path)

	// 先尝试缓存命中
	if node, ok := c.Get(path); ok {
		if onCacheHit != nil {
			onCacheHit(node.Entries)
		}
	} else {
		// 缓存未命中，直接读取
		entries, err := readDir(path)
		if err != nil {
			logx.Debugf("SWR 首次读取失败: path=%s err=%v", path, err)
			return
		}
		c.Set(path, entries)
		if onCacheHit != nil {
			onCacheHit(entries)
		}
		return
	}

	// 后台异步校验
	go func() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		diff, err := c.Revalidate(path, readDir)
		if err != nil {
			logx.Debugf("SWR 后台校验失败: path=%s err=%v", path, err)
			return
		}
		if diff != nil && (len(diff.Added) > 0 || len(diff.Removed) > 0 || len(diff.Changed) > 0) {
			if node, ok := c.Get(path); ok {
				if onDiff != nil {
					onDiff(diff, node.Entries)
				}
			}
		}
	}()
}
