package config

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 口令散列参数。
//
// 210000 次迭代是 OWASP 对 PBKDF2-HMAC-SHA256 的当前建议下限：
// 在路由器上有几十毫秒的量级，用户感知不到，而暴力破解的代价被放大到几十万倍。
const (
	password_algo       = "pbkdf2-sha256"
	password_iterations = 210000
	password_salt_bytes = 16
	password_key_bytes  = 32

	// DefaultPassword 是首次启动写入的初始口令，登录成功后会被强制更换
	DefaultPassword = "admin"

	password_min_runes = 8
	password_max_runes = 128
)

// weak_passwords 是最常见的弱口令与跟本项目相关的词，直接拒掉。
// 只做小规模字典而不是完整字典：真正拦住暴力破解的是迭代次数与登录限速。
var weak_passwords = map[string]bool{
	"admin": true, "admin123": true, "administrator": true, "password": true,
	"passw0rd": true, "12345678": true, "123456789": true, "1234567890": true,
	"87654321": true, "qwertyui": true, "qwerty123": true, "letmein1": true,
	"iloveyou": true, "wrtdeck": true, "wrtdeck123": true, "openwrt": true,
	"rootroot": true, "changeme": true, "secret12": true, "default1": true,
}

// PasswordHash 是口令的散列存储结构，明文永不落盘也不进日志
type PasswordHash struct {
	Algo       string `json:"algo"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}

// IsZero 表示尚未设置口令
func (h PasswordHash) IsZero() bool {
	return h.Hash == "" || h.Salt == ""
}

// NewPasswordHash 用随机盐计算口令散列
func NewPasswordHash(password string) (PasswordHash, error) {
	salt := make([]byte, password_salt_bytes)
	if _, err := rand.Read(salt); err != nil {
		return PasswordHash{}, fmt.Errorf("生成口令盐失败: %w", err)
	}
	derived, err := derive(password, salt, password_iterations)
	if err != nil {
		return PasswordHash{}, err
	}
	return PasswordHash{
		Algo:       password_algo,
		Iterations: password_iterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Hash:       base64.StdEncoding.EncodeToString(derived),
	}, nil
}

// Verify 校验口令，比较过程定长，不因前缀匹配而提前返回
func (h PasswordHash) Verify(password string) bool {
	if h.IsZero() {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(h.Salt)
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(h.Hash)
	if err != nil {
		return false
	}
	iterations := h.Iterations
	if iterations <= 0 {
		iterations = password_iterations
	}
	derived, err := derive(password, salt, iterations)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(derived, want) == 1
}

// derive 计算 PBKDF2-HMAC-SHA256
func derive(password string, salt []byte, iterations int) ([]byte, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, password_key_bytes)
	if err != nil {
		return nil, fmt.Errorf("计算口令散列失败: %w", err)
	}
	return key, nil
}

// CheckPasswordStrength 按策略检查新口令，不符合时返回可直接展示给用户的中文原因
func CheckPasswordStrength(password string) error {
	if password == "" {
		return fmt.Errorf("口令不能为空")
	}
	if strings.TrimSpace(password) != password {
		return fmt.Errorf("口令首尾不能有空白字符")
	}
	count := utf8.RuneCountInString(password)
	if count < password_min_runes {
		return fmt.Errorf("口令至少 %d 个字符", password_min_runes)
	}
	if count > password_max_runes {
		return fmt.Errorf("口令最多 %d 个字符", password_max_runes)
	}
	if weak_passwords[strings.ToLower(password)] {
		return fmt.Errorf("这个口令太常见，请换一个")
	}
	// 只由单一字符种类组成的口令（纯数字、纯字母）在设备场景下强度不足
	if kinds(password) < 2 {
		return fmt.Errorf("口令需要同时包含字母、数字或符号中的至少两类")
	}
	return nil
}

// kinds 统计口令里出现的字符种类数：小写字母、大写字母、数字、其它符号
func kinds(password string) int {
	var lower, upper, digit, other bool
	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			other = true
		}
	}
	total := 0
	for _, hit := range []bool{lower, upper, digit, other} {
		if hit {
			total++
		}
	}
	return total
}
