package store

import (
	"ding-ssh/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
)

// GroupStore 分组存储接口。
type GroupStore interface {
	List() ([]string, error)
	Add(name string) error
	Rename(oldName, newName string) error
	Remove(name string) error
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

// List 返回全部分组（已排序）。
func (s *JSONGroupStore) List() ([]string, error) {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 读取分组列表: path=%q", s.path)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 读取分组列表失败: path=%q err=%v", s.path, err)
		return nil, err
	}
	sort.Strings(list)
	logx.Debugf("[store] 分组列表读取完成: count=%d elapsed=%s", len(list), time.Since(start))
	return list, nil
}

// Add 新增分组（已存在则忽略）。
func (s *JSONGroupStore) Add(name string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 新增分组(JSON): name=%q", name)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 新增分组(JSON)前读取列表失败: name=%q err=%v", name, err)
		return err
	}
	for _, g := range list {
		if g == name {
			logx.Debugf("[store] 分组已存在，忽略新增: name=%q count=%d", name, len(list))
			return nil
		}
	}
	list = append(list, name)
	if err := s.save(list); err != nil {
		logx.Errorf("[store] 新增分组失败(JSON): name=%q err=%v", name, err)
		return err
	}
	logx.Infof("[store] 新增分组成功(JSON): name=%q count=%d elapsed=%s", name, len(list), time.Since(start))
	return nil
}

// Rename 重命名分组。
func (s *JSONGroupStore) Rename(oldName, newName string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 重命名分组(JSON): old=%q new=%q", oldName, newName)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 重命名分组(JSON)前读取列表失败: old=%q err=%v", oldName, err)
		return err
	}
	renamed := false
	for i, g := range list {
		if g == oldName {
			list[i] = newName
			renamed = true
			break
		}
	}
	if !renamed {
		list = append(list, newName)
		logx.Warnf("[store] 重命名分组(JSON)未找到旧分组，已作为新分组追加: old=%q new=%q", oldName, newName)
	}
	if err := s.save(list); err != nil {
		logx.Errorf("[store] 重命名分组失败(JSON): old=%q new=%q err=%v", oldName, newName, err)
		return err
	}
	logx.Infof("[store] 重命名分组成功(JSON): old=%q new=%q renamed=%v count=%d elapsed=%s",
		oldName, newName, renamed, len(list), time.Since(start))
	return nil
}

// Remove 删除分组。
func (s *JSONGroupStore) Remove(name string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 删除分组(JSON): name=%q", name)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 删除分组(JSON)前读取列表失败: name=%q err=%v", name, err)
		return err
	}
	before := len(list)
	out := list[:0]
	for _, g := range list {
		if g != name {
			out = append(out, g)
		}
	}
	if err := s.save(out); err != nil {
		logx.Errorf("[store] 删除分组失败(JSON): name=%q err=%v", name, err)
		return err
	}
	if len(out) == before {
		logx.Warnf("[store] 删除分组(JSON)未命中: name=%q count=%d", name, before)
	} else {
		logx.Infof("[store] 删除分组成功(JSON): name=%q remaining=%d elapsed=%s", name, len(out), time.Since(start))
	}
	return nil
}

var _ GroupStore = (*JSONGroupStore)(nil)
