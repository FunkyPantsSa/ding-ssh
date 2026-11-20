package store

import (
	"ding-ssh/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
)

// GroupStore 路径化分组存储接口。路径即身份，'/' 分隔层级，空字符串表示未分组。
//
// Rename 的第二个参数只允许最后一段名称（不含 '/'）；Remove 的 keepChildren
// 决定是递归删除还是把子分组上移一级。SQLite 实现会在事务内级联更新 servers.grp，
// JSON 兜底实现只维护分组列表，服务器由 app 层补更新（见 GroupServerCascadeStore）。
type GroupStore interface {
	List() ([]string, error)
	Add(path string) error
	Rename(path, newLeaf string) error
	Remove(path string, keepChildren bool) error
	// TODO(第二阶段): MoveGroup(path, newParent string) error 调整父级并校验不能移入自己的子孙。
}

// GroupServerCascadeStore 由「写操作已自行级联服务器分组」的存储实现。
// 返回 true 时，app 层无需再逐条保存服务器（SQLite 事务实现）。
type GroupServerCascadeStore interface {
	CascadesServers() bool
}

// JSONGroupStore 基于 JSON 文件的空分组存储。
// 分组实际归属由服务器节点的 Group 字段决定，这里仅保存「手动创建但暂无服务器的分组」。
type JSONGroupStore struct {
	mu   sync.Mutex
	path string
}

// NewJSONGroupStore 创建分组存储，并确保配置目录存在。
func NewJSONGroupStore(path string) (*JSONGroupStore, error) {
	if path == "" {
		path = DefaultGroupPath()
	}
	logx.Debugf("[store] 创建 JSON 分组存储: path=%q", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logx.Errorf("[store] 创建分组存储目录失败: dir=%q err=%v", filepath.Dir(path), err)
		return nil, fmt.Errorf(i18n.T("创建配置目录失败: %w"), err)
	}
	logx.Debugf("[store] JSON 分组存储就绪: path=%q", path)
	return &JSONGroupStore{path: path}, nil
}

// DefaultGroupPath 返回默认分组文件路径。
func DefaultGroupPath() string {
	return filepath.Join(appdir.ConfigDir(), "groups.json")
}

// CascadesServers JSON 兜底实现无法在同一事务内更新服务器，返回 false。
func (s *JSONGroupStore) CascadesServers() bool { return false }

func (s *JSONGroupStore) load() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		logx.Debugf("[store] 分组文件不存在，按空列表处理: path=%q", s.path)
		return []string{}, nil
	}
	if err != nil {
		logx.Warnf("[store] 读取分组文件失败: path=%q err=%v", s.path, err)
		return nil, err
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		logx.Warnf("[store] 解析分组文件失败: path=%q bytes=%d err=%v", s.path, len(data), err)
		return nil, err
	}
	if list == nil {
		list = []string{}
	}
	return list, nil
}

func (s *JSONGroupStore) save(list []string) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		logx.Warnf("[store] 序列化分组列表失败: count=%d err=%v", len(list), err)
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logx.Warnf("[store] 写入分组临时文件失败: path=%q bytes=%d err=%v", tmp, len(data), err)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		logx.Warnf("[store] 原子替换分组文件失败: path=%q err=%v", s.path, err)
		return err
	}
	logx.Debugf("[store] 分组文件已写入: path=%q count=%d bytes=%d", s.path, len(list), len(data))
	return nil
}

// normalizeList 把列表规范化为路径语义（折叠空段、去重、排序），并返回无法修复的条目数。
func normalizeList(list []string) []string {
	out := make([]string, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, raw := range list {
		path, err := NormalizeLegacyGroupPath(raw)
		if err != nil {
			logx.Warnf("[store] JSON 分组路径非法，按原样保留: raw=%q err=%v", raw, err)
			path = strings.TrimSpace(raw)
		}
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			logx.Warnf("[store] JSON 分组路径规范化后重复，已合并: raw=%q path=%q", raw, path)
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// List 返回全部分组路径（规范化、去重、已排序）。
func (s *JSONGroupStore) List() ([]string, error) {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 读取分组列表(JSON): path=%q", s.path)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 读取分组列表失败(JSON): path=%q err=%v", s.path, err)
		return nil, err
	}
	normalized := normalizeList(list)
	logx.Debugf("[store] 分组列表读取完成(JSON): raw=%d count=%d elapsed=%s",
		len(list), len(normalized), time.Since(start))
	return normalized, nil
}

// Add 新增分组路径及所有隐式祖先（已存在则忽略）。
func (s *JSONGroupStore) Add(path string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := NormalizeGroupPath(path)
	if err != nil {
		logx.Warnf("[store] 新增分组被拒（路径非法，JSON）: path=%q err=%v", path, err)
		return err
	}
	logx.Debugf("[store] 新增分组(JSON): path=%q", normalized)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 新增分组(JSON)前读取列表失败: path=%q err=%v", normalized, err)
		return err
	}
	list = normalizeList(list)
	seen := make(map[string]struct{}, len(list))
	for _, g := range list {
		seen[g] = struct{}{}
	}
	created := 0
	for _, p := range append(GroupAncestors(normalized), normalized) {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		list = append(list, p)
		created++
	}
	sort.Strings(list)
	if err := s.save(list); err != nil {
		logx.Errorf("[store] 新增分组失败(JSON): path=%q err=%v", normalized, err)
		return fmt.Errorf(i18n.T("新增分组失败: %w"), err)
	}
	logx.Infof("[store] 新增分组成功(JSON): path=%q created=%d count=%d elapsed=%s",
		normalized, created, len(list), time.Since(start))
	return nil
}

// Rename 只修改最后一段，按路径前缀同步分组列表（非事务）。
func (s *JSONGroupStore) Rename(path, newLeaf string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	oldPath, targetPath, err := ResolveGroupRename(path, newLeaf)
	if err != nil {
		logx.Warnf("[store] 重命名分组被拒（参数非法，JSON）: path=%q newLeaf=%q err=%v", path, newLeaf, err)
		return err
	}
	if oldPath == targetPath {
		logx.Debugf("[store] 重命名分组跳过（路径未变，JSON）: path=%q", oldPath)
		return nil
	}
	logx.Debugf("[store] 重命名分组(JSON): old=%q new=%q", oldPath, targetPath)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 重命名分组(JSON)前读取列表失败: old=%q err=%v", oldPath, err)
		return err
	}
	list = normalizeList(list)
	for _, g := range list {
		if g == targetPath || IsGroupDescendant(g, targetPath) {
			logx.Warnf("[store] 重命名分组被拒（目标已存在，JSON）: old=%q new=%q conflict=%q", oldPath, targetPath, g)
			return errors.New(i18n.T("目标分组已存在"))
		}
	}
	matched := 0
	for i, g := range list {
		switch {
		case g == oldPath:
			list[i] = targetPath
			matched++
		case IsGroupDescendant(g, oldPath):
			list[i] = targetPath + g[len(oldPath):]
			matched++
		}
	}
	if matched == 0 {
		logx.Warnf("[store] 重命名分组(JSON)未在分组列表命中（可能只有服务器在用该分组）: old=%q", oldPath)
	}
	if err := s.save(normalizeList(list)); err != nil {
		logx.Errorf("[store] 重命名分组失败(JSON): old=%q new=%q err=%v", oldPath, targetPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	logx.Infof("[store] 重命名分组成功(JSON、非事务): old=%q new=%q groups=%d elapsed=%s",
		oldPath, targetPath, matched, time.Since(start))
	return nil
}

// Remove 删除分组（非事务）。
//   - keepChildren=false：删除该组及所有子孙分组；
//   - keepChildren=true：删除该组，子孙分组整体上移一级；与已有分组冲突时拒绝。
func (s *JSONGroupStore) Remove(path string, keepChildren bool) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	canonical, err := CanonicalGroupPath(path)
	if err != nil {
		logx.Warnf("[store] 删除分组被拒（路径非法，JSON）: path=%q err=%v", path, err)
		return err
	}
	logx.Debugf("[store] 删除分组(JSON): path=%q keepChildren=%v", canonical, keepChildren)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 删除分组(JSON)前读取列表失败: path=%q err=%v", canonical, err)
		return err
	}
	list = normalizeList(list)
	matched := false
	for _, g := range list {
		if g == canonical || IsGroupDescendant(g, canonical) {
			matched = true
			break
		}
	}
	if !matched {
		logx.Warnf("[store] 删除分组(JSON)未在分组列表命中（可能只有服务器在用该分组）: path=%q", canonical)
		return nil
	}
	if keepChildren {
		if err := CheckGroupLiftConflict(list, nil, canonical); err != nil {
			logx.Warnf("[store] 删除分组被拒（子分组上移冲突，JSON）: path=%q err=%v", canonical, err)
			return err
		}
	}
	parent := GroupParent(canonical)
	out := make([]string, 0, len(list))
	removed := 0
	for _, g := range list {
		switch {
		case g == canonical:
			removed++
			continue
		case IsGroupDescendant(g, canonical):
			removed++
			if keepChildren {
				relative := strings.TrimPrefix(g, canonical+"/")
				out = append(out, JoinGroupPath(parent, relative))
			}
			continue
		default:
			out = append(out, g)
		}
	}
	if err := s.save(normalizeList(out)); err != nil {
		logx.Errorf("[store] 删除分组失败(JSON): path=%q err=%v", canonical, err)
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	logx.Infof("[store] 删除分组成功(JSON、非事务): path=%q keepChildren=%v groups=%d remaining=%d elapsed=%s",
		canonical, keepChildren, removed, len(out), time.Since(start))
	return nil
}

var (
	_ GroupStore              = (*JSONGroupStore)(nil)
	_ GroupServerCascadeStore = (*JSONGroupStore)(nil)
)
