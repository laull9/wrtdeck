// Package certs 负责面板 HTTPS 需要的证书：加载既有证书，或在缺省时生成自签证书。
//
// 面板的口令是明文提交的，一旦直接暴露在公网就必须用 TLS 兜住，
// 否则同一条链路上的任何一跳都能读走口令，后面的散列与限速都白做。
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 自签证书的有效期。设备上的自签证书没有续期渠道，给足十年，
// 反正浏览器本来就会提示不受信任，公网部署应当换正式证书。
const self_signed_years = 10

// 证书文件在数据目录下的默认文件名
const (
	self_signed_cert = "tls.crt"
	self_signed_key  = "tls.key"
)

// Settings 描述 TLS 的准备参数
type Settings struct {
	Enabled        bool
	CertFile       string
	KeyFile        string
	DataDir        string
	AutoSelfSigned bool
}

// Result 是准备结果，Enabled 为假时其余字段无意义
type Result struct {
	Enabled     bool
	CertFile    string
	KeyFile     string
	SelfSigned  bool
	Fingerprint string
	NotAfter    time.Time
	Subjects    []string
}

// Prepare 按配置准备好证书。三种情况：
//   - 未启用：直接返回 Enabled=false；
//   - 给了证书与私钥：加载并报告指纹，路径不合法时返回错误；
//   - 没给证书：AutoSelfSigned 为真时生成自签证书并持久化到数据目录，
//     否则报错，避免运维以为开了 HTTPS 实际还在明文跑。
func Prepare(settings Settings) (Result, error) {
	if !settings.Enabled {
		return Result{}, nil
	}
	cert_file := strings.TrimSpace(settings.CertFile)
	key_file := strings.TrimSpace(settings.KeyFile)
	if cert_file == "" || key_file == "" {
		if !settings.AutoSelfSigned {
			return Result{}, fmt.Errorf("已启用 TLS 但没有配置 tls.cert_file / tls.key_file，" +
				"并且把 tls.auto_self_signed 设成了 false；请补齐证书或打开自签开关")
		}
		cert_file = filepath.Join(settings.DataDir, self_signed_cert)
		key_file = filepath.Join(settings.DataDir, self_signed_key)
		if err := ensure_self_signed(cert_file, key_file, settings.DataDir); err != nil {
			return Result{}, err
		}
	}
	return describe(cert_file, key_file)
}

// describe 读取证书，回报指纹与是否为自签
func describe(cert_file, key_file string) (Result, error) {
	// 先真正加载一次：路径写错、公私钥不匹配都在启动阶段暴露，不要拖到第一个请求
	if _, err := tls.LoadX509KeyPair(cert_file, key_file); err != nil {
		return Result{}, fmt.Errorf("加载证书失败（%s / %s）: %w", cert_file, key_file, err)
	}
	raw, err := os.ReadFile(cert_file)
	if err != nil {
		return Result{}, err
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return Result{}, fmt.Errorf("证书文件不是 PEM 格式: %s", cert_file)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Result{}, fmt.Errorf("解析证书失败: %w", err)
	}
	sum := sha256.Sum256(leaf.Raw)
	return Result{
		Enabled:     true,
		CertFile:    cert_file,
		KeyFile:     key_file,
		SelfSigned:  leaf.Issuer.String() == leaf.Subject.String(),
		Fingerprint: format_fingerprint(sum),
		NotAfter:    leaf.NotAfter,
		Subjects:    leaf.DNSNames,
	}, nil
}

// ensure_self_signed 在证书缺失时生成一对自签证书，已存在则直接复用。
// 复用是刻意的：每次启动都换证书会让浏览器里已信任的例外失效。
func ensure_self_signed(cert_file, key_file, data_dir string) error {
	if file_exists(cert_file) && file_exists(key_file) {
		return nil
	}
	if err := os.MkdirAll(data_dir, 0o755); err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("生成私钥失败: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("生成证书序列号失败: %w", err)
	}
	hostname, _ := os.Hostname()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: common_name(hostname)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(self_signed_years, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dns_names(hostname),
		IPAddresses:           ip_addresses(),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("签发自签证书失败: %w", err)
	}
	if err = write_pem(cert_file, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	key_der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("编码私钥失败: %w", err)
	}
	return write_pem(key_file, "EC PRIVATE KEY", key_der, 0o600)
}

// common_name 取一个人类可读的证书主体名
func common_name(hostname string) string {
	if hostname == "" {
		return "wrtdeck"
	}
	return hostname
}

// dns_names 列出证书覆盖的主机名：本机名、mDNS 名与 localhost
func dns_names(hostname string) []string {
	names := []string{"localhost"}
	if hostname != "" {
		names = append(names, hostname)
		if !strings.Contains(hostname, ".") {
			names = append(names, hostname+".local")
		}
	}
	return names
}

// ip_addresses 收集本机所有单播地址，让浏览器用 IP 访问时证书也说得通
func ip_addresses() []net.IP {
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ips = append(ips, ipnet.IP)
	}
	return ips
}

// write_pem 以指定权限写出一段 PEM
func write_pem(path, block_type string, der []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	return pem.Encode(file, &pem.Block{Type: block_type, Bytes: der})
}

// file_exists 判断文件存在且不是目录
func file_exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// format_fingerprint 把指纹写成冒号分隔的大写十六进制，方便与浏览器里显示的值比对
func format_fingerprint(sum [32]byte) string {
	text := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(text)/2)
	for i := 0; i+2 <= len(text); i += 2 {
		parts = append(parts, text[i:i+2])
	}
	return strings.Join(parts, ":")
}
