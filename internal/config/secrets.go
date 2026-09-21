package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Secrets 保存 API Token 与模板可引用的敏感值，永不通过 API 返回原值
type Secrets struct {
	APIToken string            `json:"api_token"`
	Values   map[string]string `json:"values,omitempty"`

	path string
	mu   sync.RWMutex
}

// LoadSecrets 读取密钥文件，不存在时生成随机 API Token 并以 0600 落盘
func LoadSecrets(path string) (*Secrets, error) {
	s := &Secrets{Values: map[string]string{}, path: path}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("解析密钥文件失败: %w", err)
		}
		s.path = path
		if s.Values == nil {
			s.Values = map[string]string{}
		}
		if s.APIToken != "" {
			return s, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	token, err := random_token()
	if err != nil {
		return nil, err
	}
	s.APIToken = token
	if err = s.Save(); err != nil {
		return nil, err
	}
	return s, nil
}

// Token 返回当前 API Token
func (s *Secrets) Token() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.APIToken
}

// Rotate 重新生成 API Token 并落盘
func (s *Secrets) Rotate() (string, error) {
	token, err := random_token()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.APIToken = token
	s.mu.Unlock()
	return token, s.Save()
}

// ValuesCopy 返回敏感值表的副本，供模板作用域使用
func (s *Secrets) ValuesCopy() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.Values))
	for k, v := range s.Values {
		out[k] = v
	}
	return out
}

// Save 原子写回密钥文件，权限固定 0600
func (s *Secrets) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err = os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// random_token 生成 32 字节随机 Token，用 URL 安全编码表示
func random_token() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机 Token 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
