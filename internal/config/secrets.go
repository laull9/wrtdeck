package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Secrets 保存 API Token、登录口令与模板可引用的敏感值，永不通过 API 返回原值
type Secrets struct {
	APIToken string `json:"api_token"`

	// Password 是人登录面板用的口令散列；API Token 仍然保留，供脚本与设备本机调用
	Password          PasswordHash `json:"password,omitempty"`
	PasswordUpdatedAt time.Time    `json:"password_updated_at,omitempty"`
	// MustChangePassword 为真表示当前还是初始口令，只允许登录后立刻修改
	MustChangePassword bool `json:"must_change_password,omitempty"`

	Values map[string]string `json:"values,omitempty"`

	path     string
	from_env bool
	mu       sync.RWMutex
}

// LoadSecrets 读取密钥文件，不存在时生成随机 API Token 与初始口令并以 0600 落盘。
// token_env 非空且对应环境变量有值时优先采用环境变量，此时不再读写文件里的 Token，
// 这样 procd 之类的服务管理器可以在不落盘的前提下注入凭据。
// 配置了 token_env 却没有该环境变量时直接报错：静默退回随机 Token 会让
// 运维以为环境变量已生效，实际鉴权用的是另一个谁也不知道的值。
func LoadSecrets(path, token_env string) (*Secrets, error) {
	s := &Secrets{Values: map[string]string{}, path: path}

	env_name := strings.TrimSpace(token_env)
	env_token := ""
	if env_name != "" {
		env_token = strings.TrimSpace(os.Getenv(env_name))
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("解析密钥文件失败: %w", err)
		}
		s.path = path
		if s.Values == nil {
			s.Values = map[string]string{}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if env_name != "" && env_token == "" {
		return nil, fmt.Errorf("配置要求从环境变量 %s 读取 API Token，但该变量为空；"+
			"请在启动脚本中导出它，或移除 auth.token_env 改用密钥文件", env_name)
	}

	changed := false
	if env_token != "" {
		s.APIToken = env_token
		s.from_env = true
	} else if s.APIToken == "" {
		token, err := random_token()
		if err != nil {
			return nil, err
		}
		s.APIToken = token
		changed = true
	}

	// 口令缺失有两类来源，都按「初始口令 + 强制更换」处理：
	//   1. 全新安装，文件里还没有口令；
	//   2. 从只有 API Token 的旧版本升级上来，此时初始口令同样需要立刻换掉，
	//      否则老设备升上来会留下一个谁都不知道口令的空档。
	if s.Password.IsZero() {
		hash, err := NewPasswordHash(DefaultPassword)
		if err != nil {
			return nil, err
		}
		s.Password = hash
		s.PasswordUpdatedAt = time.Now()
		s.MustChangePassword = true
		changed = true
	}
	if changed {
		if err = s.Save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Token 返回当前 API Token
func (s *Secrets) Token() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.APIToken
}

// FromEnv 表示 Token 来自环境变量，此时文件里的值不参与鉴权
func (s *Secrets) FromEnv() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.from_env
}

// VerifyPassword 校验登录口令，Token 来自环境变量时口令仍走文件里的散列
func (s *Secrets) VerifyPassword(password string) bool {
	s.mu.RLock()
	hash := s.Password
	s.mu.RUnlock()
	return hash.Verify(password)
}

// SetPassword 设置新口令并落盘。must_change 为真表示这仍是初始口令，
// 此时登录后的会话只能用于改口令，其它接口一律拒绝。
func (s *Secrets) SetPassword(password string, must_change bool) error {
	hash, err := NewPasswordHash(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.Password = hash
	s.PasswordUpdatedAt = time.Now()
	s.MustChangePassword = must_change
	s.mu.Unlock()
	return s.Save()
}

// MustChange 表示当前口令仍是初始口令
func (s *Secrets) MustChange() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MustChangePassword
}

// PasswordUpdatedAt 返回口令最后修改时间，零值表示未知
func (s *Secrets) PasswordUpdated() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PasswordUpdatedAt
}

// Rotate 重新生成 API Token 并落盘；Token 由环境变量提供时不允许轮换
func (s *Secrets) Rotate() (string, error) {
	s.mu.Lock()
	if s.from_env {
		s.mu.Unlock()
		return "", fmt.Errorf("Token 由环境变量提供，无法在运行时轮换")
	}
	s.mu.Unlock()

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

// Save 原子写回密钥文件，权限固定 0600；Token 来自环境变量时不把明文写进文件
func (s *Secrets) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	snapshot := Secrets{
		APIToken:           s.APIToken,
		Password:           s.Password,
		PasswordUpdatedAt:  s.PasswordUpdatedAt,
		MustChangePassword: s.MustChangePassword,
		Values:             s.Values,
	}
	if s.from_env {
		snapshot.APIToken = ""
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(&snapshot, "", "  ")
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
