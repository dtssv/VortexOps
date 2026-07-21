// Package security 提供密码哈希、JWT 签发与校验、AES-GCM 字段加密。
// 所有实现为生产级：bcrypt 抗暴力破解、JWT 带 iss/aud/exp 校验、AES-256-GCM 加密敏感字段。
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/vortexops/vortexops/internal/config"
)

// PasswordHasher 使用 bcrypt 进行密码哈希与校验。
type PasswordHasher struct {
	cost int
}

// NewPasswordHasher 创建密码哈希器。cost 应在 10-14 之间。
func NewPasswordHasher(cost int) (*PasswordHasher, error) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return nil, fmt.Errorf("invalid bcrypt cost %d: must be between %d and %d", cost, bcrypt.MinCost, bcrypt.MaxCost)
	}
	return &PasswordHasher{cost: cost}, nil
}

// Hash 返回 bcrypt 哈希字符串。
func (h *PasswordHasher) Hash(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("password must not be empty")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// Compare 校验明文密码与哈希是否匹配。匹配返回 nil。
func (h *PasswordHasher) Compare(hashed, plain string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}
		return fmt.Errorf("compare password: %w", err)
	}
	return nil
}

// ErrPasswordMismatch 密码不匹配的哨兵错误。
var ErrPasswordMismatch = errors.New("password mismatch")

// Claims 是 VortexOps JWT 自定义声明。
type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"usr"`
	jwt.RegisteredClaims
}

// JWTIssuer 签发与校验 JWT access/refresh 令牌。
type JWTIssuer struct {
	signingKey   []byte
	issuer       string
	audience     string
	accessTTL    time.Duration
	refreshTTL   time.Duration
	refreshRotate bool
}

// NewJWTIssuer 创建 JWT 签发器。
func NewJWTIssuer(cfg config.JWTConfig) (*JWTIssuer, error) {
	if len(cfg.SigningKey) < 32 {
		return nil, errors.New("jwt signing key must be at least 32 bytes")
	}
	if cfg.AccessTTL <= 0 || cfg.RefreshTTL <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	if cfg.RefreshTTL <= cfg.AccessTTL {
		return nil, errors.New("refresh ttl must be greater than access ttl")
	}
	return &JWTIssuer{
		signingKey:    []byte(cfg.SigningKey),
		issuer:        cfg.Issuer,
		audience:      cfg.Audience,
		accessTTL:     cfg.AccessTTL,
		refreshTTL:    cfg.RefreshTTL,
		refreshRotate: cfg.RefreshRotate,
	}, nil
}

// IssueAccessToken 签发短期 access token。
func (j *JWTIssuer) IssueAccessToken(userID int64, username string) (string, time.Time, error) {
	return j.issue(userID, username, j.accessTTL, "access")
}

// IssueRefreshToken 签发长期 refresh token。
func (j *JWTIssuer) IssueRefreshToken(userID int64, username string) (string, time.Time, error) {
	return j.issue(userID, username, j.refreshTTL, "refresh")
}

// IssueMFAToken 签发短期 MFA 挑战 token（5 分钟有效），用于 MFA 登录第二步验证。
func (j *JWTIssuer) IssueMFAToken(userID int64, username string) (string, time.Time, error) {
	return j.issue(userID, username, 5*time.Minute, "mfa_challenge")
}

func (j *JWTIssuer) issue(userID int64, username string, ttl time.Duration, tokenType string) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Audience:  jwt.ClaimStrings{j.audience},
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now),
			ID:        tokenType,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(j.signingKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse 校验并解析 token，返回声明。token 无效或过期返回错误。
func (j *JWTIssuer) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return j.signingKey, nil
	}, jwt.WithIssuer(j.issuer), jwt.WithAudience(j.audience))
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return claims, nil
}

// FieldCipher 使用 AES-256-GCM 加密/解密 DB 中敏感字段（kubeconfig、凭证）。
// 密钥为 32 字节，配置中以 64 字符 hex 提供。
type FieldCipher struct {
	key []byte
}

// NewFieldCipher 从 hex 编码的 32 字节密钥创建加密器。
func NewFieldCipher(hexKey string) (*FieldCipher, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	return &FieldCipher{key: key}, nil
}

// Encrypt 加密明文，返回 nonce+ciphertext。
func (c *FieldCipher) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	// Seal 把 nonce 前置，便于解密时切片。
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt 解密 Encrypt 产生的密文。
func (c *FieldCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, body := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// EncryptString 加密字符串并以 hex 返回（便于存入 DB text 列）。
func (c *FieldCipher) EncryptString(plain string) (string, error) {
	ct, err := c.Encrypt([]byte(plain))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ct), nil
}

// HashTokenSHA256 返回 token 的 SHA-256 十六进制摘要（用于 API Key 等不可逆存储）。
func HashTokenSHA256(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// DecryptString 解密 hex 字符串。
func (c *FieldCipher) DecryptString(hexCT string) (string, error) {
	ct, err := hex.DecodeString(hexCT)
	if err != nil {
		return "", fmt.Errorf("decode hex: %w", err)
	}
	pt, err := c.Decrypt(ct)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
