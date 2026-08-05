// Package store 提供服务器节点配置的持久化存储。
// 当前使用 JSON 文件实现，后续可按设计文档替换为 SQLite/BoltDB 加密存储。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"ding-ssh/internal/models"
)

// Store 服务器节点存储接口。
type Store interface {
	List() ([]models.ServerNode, error)
	Save(node models.ServerNode) error
	Delete(id string) error
}

// JSONStore 基于 JSON 文件的存储实现。
type JSONStore struct {
	mu   sync.Mutex
	path string
}

// NewJSONStore 创建 JSON 存储，并确保配置文件目录存在。
func NewJSONStore(path string) (*JSONStore, error) {
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户配置目录失败: %w", err)
		}
		path = filepath.Join(dir, "ding-ssh", "servers.json")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	return &JSONStore{path: path}, nil
}

// DefaultPath 返回默认配置文件路径。
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "servers.json"
	}
	return filepath.Join(dir, "ding-ssh", "servers.json")
}

func (s *JSONStore) load() ([]models.ServerNode, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []models.ServerNode{}, nil
	}
	if err != nil {
		return nil, err
	}
	var nodes []models.ServerNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []models.ServerNode{}
	}
	return nodes, nil
}

func (s *JSONStore) save(nodes []models.ServerNode) error {
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List 返回全部服务器节点。
func (s *JSONStore) List() ([]models.ServerNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Save 新增或更新服务器节点（按 ID 匹配，ID 为空时忽略）。
func (s *JSONStore) Save(node models.ServerNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes, err := s.load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range nodes {
		if nodes[i].ID == node.ID {
			nodes[i] = node
			replaced = true
			break
		}
	}
	if !replaced {
		nodes = append(nodes, node)
	}
	return s.save(nodes)
}

// Delete 按 ID 删除服务器节点。
func (s *JSONStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes, err := s.load()
	if err != nil {
		return err
	}
	out := nodes[:0]
	for _, n := range nodes {
		if n.ID != id {
			out = append(out, n)
		}
	}
	return s.save(out)
}
