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
	logx.Debugf("[store] 创建 JSON 凭证存储: path=%q", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logx.Errorf("[store] 创建凭证存储目录失败: dir=%q err=%v", filepath.Dir(path), err)
		return nil, fmt.Errorf(i18n.T("创建配置目录失败: %w"), err)
	}
	logx.Debugf("[store] JSON 凭证存储就绪: path=%q", path)
	return &JSONCredentialStore{path: path}, nil
}

// DefaultCredentialPath 返回默认凭证文件路径。
func DefaultCredentialPath() string {
	return filepath.Join(appdir.ConfigDir(), "credentials.json")
}

func (s *JSONCredentialStore) load() ([]models.Credential, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		logx.Debugf("[store] 凭证文件不存在，按空列表处理: path=%q", s.path)
		return []models.Credential{}, nil
	}
	if err != nil {
		logx.Warnf("[store] 读取凭证文件失败: path=%q err=%v", s.path, err)
		return nil, err
	}
	var list []models.Credential
	if err := json.Unmarshal(data, &list); err != nil {
		logx.Warnf("[store] 解析凭证文件失败: path=%q bytes=%d err=%v", s.path, len(data), err)
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
		logx.Warnf("[store] 序列化凭证列表失败: count=%d err=%v", len(list), err)
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logx.Warnf("[store] 写入凭证临时文件失败: path=%q bytes=%d err=%v", tmp, len(data), err)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		logx.Warnf("[store] 原子替换凭证文件失败: path=%q err=%v", s.path, err)
		return err
	}
	logx.Debugf("[store] 凭证文件已写入: path=%q count=%d bytes=%d", s.path, len(list), len(data))
	return nil
}

// List 返回全部凭证。
func (s *JSONCredentialStore) List() ([]models.Credential, error) {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 读取凭证列表: path=%q", s.path)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 读取凭证列表失败: path=%q err=%v", s.path, err)
		return nil, err
	}
	logx.Debugf("[store] 凭证列表读取完成: count=%d elapsed=%s", len(list), time.Since(start))
	return list, nil
}

// Save 新增或更新凭证（按 ID 匹配）。
func (s *JSONCredentialStore) Save(c models.Credential) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 保存凭证(JSON): id=%s name=%q user=%s authType=%s", c.ID, c.Name, c.User, c.AuthType)
	if c.ID == "" {
		logx.Warnf("[store] 保存凭证(JSON)缺少 ID，按 ID 匹配可能产生重复项: name=%q", c.Name)
	}
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 保存凭证(JSON)前读取列表失败: id=%s err=%v", c.ID, err)
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
	if err := s.save(list); err != nil {
		logx.Errorf("[store] 保存凭证失败(JSON): id=%s name=%q err=%v", c.ID, c.Name, err)
		return err
	}
	logx.Infof("[store] 保存凭证成功(JSON): id=%s name=%q replaced=%v count=%d elapsed=%s",
		c.ID, c.Name, replaced, len(list), time.Since(start))
	return nil
}

// Delete 按 ID 删除凭证。
func (s *JSONCredentialStore) Delete(id string) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 删除凭证(JSON): id=%s", id)
	list, err := s.load()
	if err != nil {
		logx.Errorf("[store] 删除凭证(JSON)前读取列表失败: id=%s err=%v", id, err)
		return err
	}
	before := len(list)
	out := list[:0]
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	if err := s.save(out); err != nil {
		logx.Errorf("[store] 删除凭证失败(JSON): id=%s err=%v", id, err)
		return err
	}
	if len(out) == before {
		logx.Warnf("[store] 删除凭证(JSON)未命中: id=%s count=%d", id, before)
	} else {
		logx.Infof("[store] 删除凭证成功(JSON): id=%s removed=%d remaining=%d elapsed=%s",
			id, before-len(out), len(out), time.Since(start))
	}
	return nil
}
