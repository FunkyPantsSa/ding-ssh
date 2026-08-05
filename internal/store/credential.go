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

// CredentialStore 凭证存储接口。
type CredentialStore interface {
	List() ([]models.Credential, error)
	Save(c models.Credential) error
	Delete(id string) error
}

// JSONCredentialStore 基于 JSON 文件的凭证存储。
type JSONCredentialStore struct {
	mu   sync.Mutex
	path string
}

// NewJSONCredentialStore 创建凭证存储，并确保配置目录存在。
func NewJSONCredentialStore(path string) (*JSONCredentialStore, error) {
	if path == "" {
		path = DefaultCredentialPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	return &JSONCredentialStore{path: path}, nil
}

// DefaultCredentialPath 返回默认凭证文件路径。
func DefaultCredentialPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "credentials.json"
	}
	return filepath.Join(dir, "ding-ssh", "credentials.json")
}

func (s *JSONCredentialStore) load() ([]models.Credential, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []models.Credential{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []models.Credential
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []models.Credential{}
	}
	return list, nil
}

func (s *JSONCredentialStore) save(list []models.Credential) error {
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

// List 返回全部凭证。
func (s *JSONCredentialStore) List() ([]models.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Save 新增或更新凭证（按 ID 匹配）。
func (s *JSONCredentialStore) Save(c models.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range list {
		if list[i].ID == c.ID {
			list[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, c)
	}
	return s.save(list)
}

// Delete 按 ID 删除凭证。
func (s *JSONCredentialStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return s.save(out)
}
