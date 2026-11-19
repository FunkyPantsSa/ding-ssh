// Package store 提供服务器节点配置的持久化存储。
// 当前使用 JSON 文件实现，后续可按设计文档替换为 SQLite/BoltDB 加密存储。
package store

import (
	"ding-ssh/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
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
		path = DefaultPath()
	}
	logx.Debugf("[store] 创建 JSON 服务器存储: path=%q", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logx.Errorf("[store] 创建服务器存储目录失败: dir=%q err=%v", filepath.Dir(path), err)
		return nil, fmt.Errorf(i18n.T("创建配置目录失败: %w"), err)
	}
	logx.Debugf("[store] JSON 服务器存储就绪: path=%q", path)
	return &JSONStore{path: path}, nil
}

// DefaultPath 返回默认配置文件路径。
func DefaultPath() string {
	return filepath.Join(appdir.ConfigDir(), "servers.json")
}

func (s *JSONStore) load() ([]models.ServerNode, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		logx.Debugf("[store] 服务器文件不存在，按空列表处理: path=%q", s.path)
		return []models.ServerNode{}, nil
	}
	if err != nil {
		logx.Warnf("[store] 读取服务器文件失败: path=%q err=%v", s.path, err)
		return nil, err
	}
	var nodes []models.ServerNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		logx.Warnf("[store] 解析服务器文件失败: path=%q bytes=%d err=%v", s.path, len(data), err)
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
		logx.Warnf("[store] 序列化服务器列表失败: count=%d err=%v", len(nodes), err)
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logx.Warnf("[store] 写入服务器临时文件失败: path=%q bytes=%d err=%v", tmp, len(data), err)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		logx.Warnf("[store] 原子替换服务器文件失败: path=%q err=%v", s.path, err)
		return err
	}
	logx.Debugf("[store] 服务器文件已写入: path=%q count=%d bytes=%d", s.path, len(nodes), len(data))
	return nil
}

// List 返回全部服务器节点。
func (s *JSONStore) List() ([]models.ServerNode, error) {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 读取服务器列表: path=%q", s.path)
	nodes, err := s.load()
	if err != nil {
		logx.Errorf("[store] 读取服务器列表失败: path=%q err=%v", s.path, err)
		return nil, err
	}
	logx.Debugf("[store] 服务器列表读取完成: count=%d elapsed=%s", len(nodes), time.Since(start))
	return nodes, nil
}

// Save 新增或更新服务器节点（按 ID 匹配，ID 为空时忽略）。
func (s *JSONStore) Save(node models.ServerNode) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 保存服务器(JSON): id=%s name=%q host=%s:%d user=%s", node.ID, node.Name, node.Host, node.Port, node.User)
	if node.ID == "" {
		logx.Warnf("[store] 保存服务器(JSON)缺少 ID，按 ID 匹配可能产生重复项: name=%q", node.Name)
	}
	nodes, err := s.load()
	if err != nil {
		logx.Errorf("[store] 保存服务器(JSON)前读取列表失败: id=%s err=%v", node.ID, err)
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
	if err := s.save(nodes); err != nil {
		logx.Errorf("[store] 保存服务器失败(JSON): id=%s name=%q err=%v", node.ID, node.Name, err)
		return err
	}
	logx.Infof("[store] 保存服务器成功(JSON): id=%s name=%q replaced=%v count=%d elapsed=%s",
		node.ID, node.Name, replaced, len(nodes), time.Since(start))
	return nil
}

// Delete 按 ID 删除服务器节点。
func (s *JSONStore) Delete(id string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 删除服务器(JSON): id=%s", id)
	nodes, err := s.load()
	if err != nil {
		logx.Errorf("[store] 删除服务器(JSON)前读取列表失败: id=%s err=%v", id, err)
		return err
	}
	before := len(nodes)
	out := nodes[:0]
	for _, n := range nodes {
		if n.ID != id {
			out = append(out, n)
		}
	}
	if err := s.save(out); err != nil {
		logx.Errorf("[store] 删除服务器失败(JSON): id=%s err=%v", id, err)
		return err
	}
	if len(out) == before {
		logx.Warnf("[store] 删除服务器(JSON)未命中: id=%s count=%d", id, before)
	} else {
		logx.Infof("[store] 删除服务器成功(JSON): id=%s removed=%d remaining=%d elapsed=%s",
			id, before-len(out), len(out), time.Since(start))
	}
	return nil
}

// SaveBatch atomically merges a validated import snapshot into the JSON store.
func (s *JSONStore) SaveBatch(incoming []models.ServerNode) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 批量保存服务器(JSON): count=%d", len(incoming))
	nodes, err := s.load()
	if err != nil {
		logx.Errorf("[store] 批量保存服务器(JSON)前读取列表失败: count=%d err=%v", len(incoming), err)
		return err
	}
	replaced := 0
	for _, node := range incoming {
		found := false
		for i := range nodes {
			if nodes[i].ID == node.ID {
				nodes[i] = node
				found = true
				replaced++
				break
			}
		}
		if !found {
			nodes = append(nodes, node)
		}
	}
	if err := s.save(nodes); err != nil {
		logx.Errorf("[store] 批量保存服务器失败(JSON): count=%d err=%v", len(incoming), err)
		return err
	}
	logx.Infof("[store] 批量保存服务器完成(JSON): total=%d replaced=%d added=%d count=%d elapsed=%s",
		len(incoming), replaced, len(incoming)-replaced, len(nodes), time.Since(start))
	return nil
}
