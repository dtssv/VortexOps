package jumpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSign_ProducesHTTPSignatureFormat 验证 sign 生成的 Authorization 头符合
// JumpServer v3 的 HTTP Signature 规范（Signature keyId=...,algorithm="hmac-sha256",
// headers="(request-target) accept date",signature=...）。
func TestSign_ProducesHTTPSignatureFormat(t *testing.T) {
	c := &Client{
		baseURL:   "http://jumpserver:8080",
		accessKey: "92c94a9d-ca23-40b6-af79-7786d15208c7",
		secretKey: "x9U09kxxqbRqk56XDAEh2JW5IbvlROrtJs7N",
		http:      &http.Client{Timeout: 15 * time.Second},
	}

	req := httptest.NewRequest(http.MethodGet, "http://jumpserver:8080/api/v1/assets/assets/?limit=500", nil)
	c.sign(req, "", "/api/v1/assets/assets/?limit=500")

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, `Signature keyId="92c94a9d-ca23-40b6-af79-7786d15208c7",`) {
		t.Fatalf("Authorization missing Signature keyId prefix: %q", auth)
	}
	if !strings.Contains(auth, `algorithm="hmac-sha256"`) {
		t.Fatalf("Authorization missing algorithm: %q", auth)
	}
	if !strings.Contains(auth, `headers="(request-target) accept date"`) {
		t.Fatalf("Authorization missing headers list: %q", auth)
	}
	if !strings.Contains(auth, `signature="`) {
		t.Fatalf("Authorization missing signature value: %q", auth)
	}
	// 旧的错误格式不应出现。
	if strings.HasPrefix(auth, "Sign ") {
		t.Fatalf("Authorization uses old broken 'Sign ak:sig' format: %q", auth)
	}

	// 必填请求头。
	if req.Header.Get("Date") == "" {
		t.Fatal("Date header not set")
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept header = %q, want application/json", got)
	}
	if got := req.Header.Get("X-JMS-ORG"); got != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("X-JMS-ORG header = %q, want default org", got)
	}
}

// TestSign_StringToSignContent 验证签名字符串内容（通过重算比对）。
// request-target 必须是小写 method + path（含 query）。
func TestSign_StringToSignContent(t *testing.T) {
	accessKey := "ak-test"
	secretKey := "sk-test"
	c := &Client{
		baseURL:   "http://jms:8080",
		accessKey: accessKey,
		secretKey: secretKey,
		http:      &http.Client{Timeout: 15 * time.Second},
	}

	path := "/api/v1/terminal/sessions/?limit=200&date_from=2026-06-25"
	req := httptest.NewRequest(http.MethodGet, "http://jms:8080"+path, nil)
	c.sign(req, "", path)

	date := req.Header.Get("Date")
	// 期望的签名字符串：三行，无尾随换行。
	expectedStringToSign := "(request-target): get " + path + "\n" +
		"accept: application/json\n" +
		"date: " + date
	expectedSig := hmacSHA256(secretKey, expectedStringToSign)

	wantAuth := `Signature keyId="` + accessKey + `",algorithm="hmac-sha256",headers="(request-target) accept date",signature="` + expectedSig + `"`
	if got := req.Header.Get("Authorization"); got != wantAuth {
		t.Fatalf("Authorization = %q\nwant         %q", got, wantAuth)
	}
}
