package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	return &JSONGroupStore{path: path}, nil
}

// DefaultGroupPath 返回默认分组文件路径。
func DefaultGroupPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "groups.json"
	}
	return filepath.Join(dir, "ding-ssh", "groups.json")
}

func (s *JSONGroupStore) load() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
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
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List 返回全部分组（已排序）。
func (s *JSONGroupStore) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	sort.Strings(list)
	return list, nil
}

// Add 新增分组（已存在则忽略）。
func (s *JSONGroupStore) Add(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	for _, g := range list {
		if g == name {
			return nil
		}
	}
	list = append(list, name)
	return s.save(list)
}

// Rename 重命名分组。
func (s *JSONGroupStore) Rename(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
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
	}
	return s.save(list)
}

// Remove 删除分组。
func (s *JSONGroupStore) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, g := range list {
		if g != name {
			out = append(out, g)
		}
	}
	return s.save(out)
}

var _ GroupStore = (*JSONGroupStore)(nil)
