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

// cacheErrLog 缓存后台校验失败采样器：连接异常时每次列目录都会失败，限流避免刷屏。
var cacheErrLog = logx.NewSampler(0, 30*time.Second)

// DirCacheNode 目录缓存节点，存储单个路径下的文件列表和更新时间。
type DirCacheNode struct {
	Path      string             `json:"path"`
	Entries   []models.SFTPEntry `json:"entries"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// entryDiff 计算两个目录列表之间的差异。
type entryDiff struct {
	Added   []models.SFTPEntry `json:"added"`
	Removed []models.SFTPEntry `json:"removed"`
	Changed []models.SFTPEntry `json:"changed"`
}

type sftpCacheKey struct {
	sessionID            string
	path                 string
	sessionGeneration    uint64
	pathGeneration       uint64
	globalPathGeneration uint64
	allGeneration        uint64
}

type sftpCachePath struct {
	sessionID string
	path      string
}

// SFTPCacheManager SWR (Stale-While-Revalidate) 目录缓存管理器。
// 使用 sync.Map 存储内存缓存，提供即时读取 + 后台异步校验。
type SFTPCacheManager struct {
	tree       sync.Map // map[string]*DirCacheNode
	mu         sync.RWMutex
	gen        map[sftpCachePath]uint64
	sessionGen map[string]uint64
	pathGen    map[string]uint64
	allGen     uint64
	ttl        time.Duration
	enabled    bool
}

// NewSFTPCacheManager 创建 SWR 缓存管理器，默认启用，TTL 30 秒。
func NewSFTPCacheManager() *SFTPCacheManager {
	logx.Debugf("[sshx] 初始化 SFTP 目录缓存: ttl=%s enabled=true", 30*time.Second)
	return &SFTPCacheManager{
		ttl:        30 * time.Second,
		enabled:    true,
		gen:        make(map[sftpCachePath]uint64),
		sessionGen: make(map[string]uint64),
		pathGen:    make(map[string]uint64),
	}
}

// Get 从缓存中获取目录列表，返回缓存数据和是否命中。
func (c *SFTPCacheManager) Get(path string) (*DirCacheNode, bool) {
	return c.GetForSession("", path)
}

// GetForSession scopes a remote path to the SSH session that owns it.
func (c *SFTPCacheManager) GetForSession(sessionID, path string) (*DirCacheNode, bool) {
	key := c.keyForSession(sessionID, path)
	return c.getByKey(key)
}

func (c *SFTPCacheManager) keyForSession(sessionID, path string) sftpCacheKey {
	c.mu.RLock()
	keyPath := normalizePath(path)
	scope := sftpCachePath{sessionID: sessionID, path: keyPath}
	sessionGeneration := c.sessionGen[sessionID]
	generation := c.gen[scope]
	globalPathGeneration := c.pathGen[keyPath]
	allGeneration := c.allGen
	c.mu.RUnlock()
	return sftpCacheKey{
		sessionID:            sessionID,
		path:                 keyPath,
		sessionGeneration:    sessionGeneration,
		pathGeneration:       generation,
		globalPathGeneration: globalPathGeneration,
		allGeneration:        allGeneration,
	}
}

func (c *SFTPCacheManager) getByKey(key sftpCacheKey) (*DirCacheNode, bool) {
	if !c.isEnabled() {
		return nil, false
	}
	val, ok := c.tree.Load(key)
	if !ok {
		logx.Debugf("[sshx] SFTP 缓存未命中: session=%s path=%s", key.sessionID, key.path)
		return nil, false
	}
	node := val.(*DirCacheNode)
	logx.Debugf("[sshx] SFTP 缓存命中: session=%s path=%s entries=%d age=%s", key.sessionID, key.path, len(node.Entries), time.Since(node.UpdatedAt).Round(time.Millisecond))
	return node, true
}

func (c *SFTPCacheManager) isEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// Set 将目录列表写入缓存。
func (c *SFTPCacheManager) Set(path string, entries []models.SFTPEntry) {
	c.SetForSession("", path, entries)
}

// SetForSession stores a directory listing under its owning SSH session.
func (c *SFTPCacheManager) SetForSession(sessionID, path string, entries []models.SFTPEntry) {
	if !c.isEnabled() {
		return
	}
	path = normalizePath(path)
	key := c.keyForSession(sessionID, path)
	c.storeByKey(key, &DirCacheNode{
		Path:      path,
		Entries:   sortedEntries(entries),
		UpdatedAt: time.Now(),
	})
}

func (c *SFTPCacheManager) storeByKey(key sftpCacheKey, node *DirCacheNode) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.keyIsCurrentLocked(key) {
		return false
	}
	if !c.enabled {
		return true
	}
	c.tree.Store(key, node)
	return true
}

func (c *SFTPCacheManager) keyIsCurrentLocked(key sftpCacheKey) bool {
	return c.sessionGen[key.sessionID] == key.sessionGeneration &&
		c.gen[sftpCachePath{sessionID: key.sessionID, path: key.path}] == key.pathGeneration &&
		c.pathGen[key.path] == key.globalPathGeneration &&
		c.allGen == key.allGeneration
}

// Invalidate 使指定路径的缓存失效。
func (c *SFTPCacheManager) Invalidate(path string) {
	keyPath := normalizePath(path)
	c.mu.Lock()
	c.pathGen[keyPath]++
	c.tree.Range(func(key, _ any) bool {
		cacheKey, ok := key.(sftpCacheKey)
		if ok && cacheKey.path == keyPath {
			c.tree.Delete(cacheKey)
		}
		return true
	})
	c.mu.Unlock()
}

// InvalidateForSession invalidates a path only on the session that owns it.
func (c *SFTPCacheManager) InvalidateForSession(sessionID, path string) {
	keyPath := normalizePath(path)
	scope := sftpCachePath{sessionID: sessionID, path: keyPath}
	c.mu.Lock()
	c.gen[scope]++
	c.deletePathForSessionLocked(sessionID, keyPath)
	c.mu.Unlock()
}

func (c *SFTPCacheManager) deletePathForSessionLocked(sessionID, path string) {
	c.tree.Range(func(key, _ any) bool {
		cacheKey, ok := key.(sftpCacheKey)
		if ok && cacheKey.sessionID == sessionID && cacheKey.path == path {
			c.tree.Delete(cacheKey)
		}
		return true
	})
}

func (c *SFTPCacheManager) InvalidateSession(sessionID string) {
	c.mu.Lock()
	c.sessionGen[sessionID]++
	c.tree.Range(func(key, _ any) bool {
		cacheKey, ok := key.(sftpCacheKey)
		if ok && cacheKey.sessionID == sessionID {
			c.tree.Delete(cacheKey)
		}
		return true
	})
	c.mu.Unlock()
}

// Clear 清空全部缓存。
func (c *SFTPCacheManager) Clear() {
	count := 0
	c.mu.Lock()
	c.allGen++
	c.tree.Range(func(key, _ interface{}) bool {
		c.tree.Delete(key)
		count++
		return true
	})
	c.mu.Unlock()
	logx.Infof("[sshx] SFTP 目录缓存已清空: entries=%d", count)
}

// Revalidate 后台异步校验缓存：执行 ReadDir 并与缓存比对，返回差异。
// 如果缓存为空或不存在，直接填充并返回 nil diff。
func (c *SFTPCacheManager) Revalidate(path string, readDir func(string) ([]models.SFTPEntry, error)) (*entryDiff, error) {
	return c.RevalidateForSession("", path, readDir)
}

func (c *SFTPCacheManager) RevalidateForSession(sessionID, path string, readDir func(string) ([]models.SFTPEntry, error)) (*entryDiff, error) {
	return c.revalidateKey(c.keyForSession(sessionID, path), path, readDir)
}

func (c *SFTPCacheManager) revalidateKey(key sftpCacheKey, path string, readDir func(string) ([]models.SFTPEntry, error)) (*entryDiff, error) {
	path = normalizePath(path)
	start := time.Now()
	entries, err := readDir(path)
	if err != nil {
		logx.Debugf("[sshx] SFTP 缓存校验读取失败: path=%s elapsed=%s err=%v", path, time.Since(start), err)
		return nil, err
	}

	sorted := sortedEntries(entries)

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.keyIsCurrentLocked(key) {
		return nil, nil
	}
	oldVal, existed := c.tree.Load(key)
	c.storeByKeyLocked(key, &DirCacheNode{Path: path, Entries: sorted, UpdatedAt: time.Now()})

	if !existed {
		logx.Debugf("[sshx] SFTP 缓存首次填充: path=%s entries=%d elapsed=%s", path, len(sorted), time.Since(start))
		return nil, nil // 首次缓存，无需 diff
	}

	oldNode := oldVal.(*DirCacheNode)
	diff := computeDiff(oldNode.Entries, sorted)
	logx.Debugf("[sshx] SFTP 缓存校验完成: path=%s entries=%d added=%d removed=%d changed=%d elapsed=%s",
		path, len(sorted), len(diff.Added), len(diff.Removed), len(diff.Changed), time.Since(start))
	return &diff, nil
}

func sortedEntries(entries []models.SFTPEntry) []models.SFTPEntry {
	sorted := append([]models.SFTPEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsDir != sorted[j].IsDir {
			return sorted[i].IsDir
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

func (c *SFTPCacheManager) storeByKeyLocked(key sftpCacheKey, node *DirCacheNode) {
	if c.enabled {
		c.tree.Store(key, node)
	}
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
	_, ok := c.tree.Load(c.keyForSession("", path))
	return ok
}

func (c *SFTPCacheManager) HasForSession(sessionID, path string) bool {
	_, ok := c.tree.Load(c.keyForSession(sessionID, path))
	return ok
}

// SetEnabled 启用/禁用缓存。
func (c *SFTPCacheManager) SetEnabled(enabled bool) {
	logx.Infof("[sshx] SFTP 目录缓存开关: enabled=%v", enabled)
	c.mu.Lock()
	c.enabled = enabled
	if !enabled {
		c.allGen++
		c.tree.Range(func(key, _ any) bool {
			c.tree.Delete(key)
			return true
		})
	}
	c.mu.Unlock()
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
	c.SWRReadDirForSession("", ctx, path, readDir, onCacheHit, onDiff)
}

// SWRReadDirForSession returns cached entries only from the selected SSH session.
func (c *SFTPCacheManager) SWRReadDirForSession(
	sessionID string,
	ctx context.Context,
	path string,
	readDir func(string) ([]models.SFTPEntry, error),
	onCacheHit func(entries []models.SFTPEntry),
	onDiff func(diff *entryDiff, entries []models.SFTPEntry),
) {
	path = normalizePath(path)
	key := c.keyForSession(sessionID, path)

	// 先尝试缓存命中
	if node, ok := c.getByKey(key); ok {
		if onCacheHit != nil {
			onCacheHit(node.Entries)
		}
	} else {
		// 缓存未命中，直接读取
		entries, err := readDir(path)
		if err != nil {
			logx.Debugf("[sshx] SWR 首次读取失败: path=%s err=%v", path, err)
			return
		}
		sorted := sortedEntries(entries)
		if !c.storeByKey(key, &DirCacheNode{Path: path, Entries: sorted, UpdatedAt: time.Now()}) {
			return
		}
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
		diff, err := c.revalidateKey(key, path, readDir)
		if err != nil {
			logx.WarnSampled(cacheErrLog, "[sshx] SWR 后台校验失败，继续使用旧缓存: path=%s err=%v", path, err)
			return
		}
		if diff != nil && (len(diff.Added) > 0 || len(diff.Removed) > 0 || len(diff.Changed) > 0) {
			if node, ok := c.getByKey(key); ok {
				if onDiff != nil {
					onDiff(diff, node.Entries)
				}
			}
		}
	}()
}
