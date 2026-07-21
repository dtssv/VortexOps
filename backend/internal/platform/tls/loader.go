// Package tls 提供 webhook serving 证书与 CA bundle 的加载/生成。
//
// 加载优先级：
//  1. WEBHOOK_TLS_CERT_FILE / WEBHOOK_TLS_KEY_FILE / WEBHOOK_CA_CERT_FILE 指向的文件（生产推荐，配合 cert-manager）。
//  2. 文件缺失时：从内置自签 CA + serving cert 生成（开发环境默认），每次启动重新生成，
//     并把 CA bundle 通过 registrar 推送到各集群的 MutatingWebhookConfiguration。
//
// 自签证书仅用于开发环境；生产部署必须通过环境变量提供外部签发的证书（如 cert-manager / vault）。
package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// Config TLS 加载配置。
type Config struct {
	// CertFile serving cert 文件路径（PEM）。为空时自签生成。
	CertFile string
	// KeyFile serving key 文件路径（PEM）。为空时自签生成。
	KeyFile string
	// CAFile CA bundle 文件路径（PEM）。为空时自签生成（与 serving cert 同 CA）。
	CAFile string
	// DNSNames serving cert 的 SAN DNS 名（webhook service DNS，如 vortexops-webhook.vortexops.svc）。
	DNSNames []string
	// IPAddresses serving cert 的 SAN IP（开发环境常含 127.0.0.1 / host IP）。
	IPAddresses []net.IP
	// CommonName serving cert CN。
	CommonName string
}

// Bundle 加载后的 TLS 证书与 CA bundle。
type Bundle struct {
	// CertPEM serving cert PEM（含中间链）。
	CertPEM []byte
	// KeyPEM serving key PEM。
	KeyPEM []byte
	// CABundlePEM 信任的 CA bundle PEM（用于推送到 MutatingWebhookConfiguration.clientConfig.caBundle）。
	CABundlePEM []byte
}

// Load 加载或生成 TLS 证书。优先从文件加载；文件缺失时自签生成。
// 返回的 Bundle 可直接用于 tls.Certificate 与 registrar 的 CA bundle 注入。
func Load(cfg Config) (*Bundle, error) {
	// 1) 优先从文件加载（生产路径）。
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		certPEM, err := os.ReadFile(cfg.CertFile)
		if err != nil {
			return nil, fmt.Errorf("read tls cert file %s: %w", cfg.CertFile, err)
		}
		keyPEM, err := os.ReadFile(cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("read tls key file %s: %w", cfg.KeyFile, err)
		}
		if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
			return nil, fmt.Errorf("parse tls cert/key: %w", err)
		}
		var caPEM []byte
		if cfg.CAFile != "" {
			caPEM, err = os.ReadFile(cfg.CAFile)
			if err != nil {
				return nil, fmt.Errorf("read ca file %s: %w", cfg.CAFile, err)
			}
		} else {
			// 未单独提供 CA 文件：假设 serving cert 自签或 CA 链包含在 cert 文件中。
			caPEM = certPEM
		}
		if err := validatePEM(caPEM); err != nil {
			return nil, fmt.Errorf("ca bundle invalid: %w", err)
		}
		return &Bundle{CertPEM: certPEM, KeyPEM: keyPEM, CABundlePEM: caPEM}, nil
	}

	// 2) 文件缺失：自签生成（开发环境）。
	if cfg.CommonName == "" {
		cfg.CommonName = "vortexops-webhook"
	}
	if len(cfg.DNSNames) == 0 {
		cfg.DNSNames = []string{"vortexops-webhook", "vortexops-webhook.vortexops", "vortexops-webhook.vortexops.svc", "localhost"}
	}
	if len(cfg.IPAddresses) == 0 {
		cfg.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	return generateSelfSigned(cfg)
}

// TLSConfig 从 Bundle 构建 crypto/tls.Config（用于 http.Server）。
func (b *Bundle) TLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair(b.CertPEM, b.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load tls keypair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		// webhook 客户端为 kube-apiserver，无需客户端证书校验。
		ClientAuth: tls.NoClientCert,
	}, nil
}

// validatePEM 校验 PEM 数据至少含一个 CERTIFICATE 块。
func validatePEM(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty pem data")
	}
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			return errors.New("no CERTIFICATE pem block found")
		}
		if block.Type == "CERTIFICATE" {
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return fmt.Errorf("parse certificate: %w", err)
			}
			return nil
		}
		data = rest
		if len(data) == 0 {
			return errors.New("no CERTIFICATE pem block found")
		}
	}
}

// generateSelfSigned 生成自签 CA + serving cert，返回完整 Bundle。
// 每次启动重新生成（开发环境可接受；生产必须用外部证书）。
func generateSelfSigned(cfg Config) (*Bundle, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("gen ca key: %w", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "vortexops-webhook-ca", Organization: []string{"VortexOps"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create ca cert: %w", err)
	}

	servingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("gen serving key: %w", err)
	}
	servingTemplate := &x509.Certificate{
		SerialNumber: randomSerial(),
		Subject:      pkix.Name{CommonName: cfg.CommonName, Organization: []string{"VortexOps"}},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(2 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     cfg.DNSNames,
		IPAddresses:  cfg.IPAddresses,
	}
	servingDER, err := x509.CreateCertificate(rand.Reader, servingTemplate, caTemplate, &servingKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create serving cert: %w", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: servingDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(servingKey)})

	// CA bundle = CA cert（kube-apiserver 据此校验 serving cert）。
	return &Bundle{CertPEM: certPEM, KeyPEM: keyPEM, CABundlePEM: caPEM}, nil
}

// randomSerial 生成正的随机证书序列号。
func randomSerial() *big.Int {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}
