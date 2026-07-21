package jumpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// hmacSHA256Impl 计算 HMAC-SHA256 并返回 base64 编码签名。
func hmacSHA256Impl(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
