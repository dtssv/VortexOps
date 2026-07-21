package jenkins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	cred, _ := json.Marshal(map[string]string{"username": "u", "api_token": "t"})
	c, err := New(srv.URL, cred)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestGetQueueItemBuildNumber_Queued 队列项尚未被调度：executable.number 缺失/为 0，
// 应返回 (0, false, nil) 而非错误。
func TestGetQueueItemBuildNumber_Queued(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/queue/item/42/api/json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"why":"Still waiting to schedule on a node"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	num, ready, err := c.GetQueueItemBuildNumber(context.Background(), "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false while still queued")
	}
	if num != 0 {
		t.Fatalf("expected num=0 while still queued, got %d", num)
	}
}

// TestGetQueueItemBuildNumber_Ready 队列项已被调度并分配构建号 7。
func TestGetQueueItemBuildNumber_Ready(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"executable":{"number":7},"why":null}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	num, ready, err := c.GetQueueItemBuildNumber(context.Background(), "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true after Jenkins assigned build number")
	}
	if num != 7 {
		t.Fatalf("expected num=7, got %d", num)
	}
}

// TestGetQueueItemBuildNumber_NotFound 队列项已离开队列且未留下 executable
// （被取消/丢弃）：Jenkins 返回 404，应返回 (0, false, nil)。
func TestGetQueueItemBuildNumber_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	num, ready, err := c.GetQueueItemBuildNumber(context.Background(), "42")
	if err != nil {
		t.Fatalf("unexpected error on 404: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false on 404")
	}
	if num != 0 {
		t.Fatalf("expected num=0 on 404, got %d", num)
	}
}

// TestGetQueueItemBuildNumber_EmptyQueueID 空 queueID 应直接返回错误，不打网络。
func TestGetQueueItemBuildNumber_EmptyQueueID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not hit network for empty queue id")
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, _, err := c.GetQueueItemBuildNumber(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty queue id")
	}
}
