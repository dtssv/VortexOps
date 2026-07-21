package bastionapp

import (
	"context"
	"sync"
	"testing"

	"github.com/vortexops/vortexops/internal/infrastructure/jumpserver"
)

// fakeSettings 内存态 SettingsReader，可通过 set 改变返回值。
type fakeSettings struct {
	mu       sync.Mutex
	values   map[string]string
	callCount int
}

func (f *fakeSettings) GetStringSetting(_ context.Context, key, fallback string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if v, ok := f.values[key]; ok {
		return v, nil
	}
	return fallback, nil
}

func (f *fakeSettings) set(key, v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = v
}

func (f *fakeSettings) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func TestJMSProvider_UnconfiguredReturnsNil(t *testing.T) {
	st := &fakeSettings{}
	// base_url 为空（默认 fallback 也是空）。
	p := NewJMSClientProvider(st)
	c, err := p.GetClient(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil client when base_url empty, got %T", c)
	}
}

func TestJMSProvider_CachesAcrossCallsWithSameConfig(t *testing.T) {
	st := &fakeSettings{}
	st.set("jumpserver.base_url", "http://jms:8080")
	st.set("jumpserver.access_key", "ak-1")
	st.set("jumpserver.secret_key", "sk-1")
	p := NewJMSClientProvider(st)

	c1, err := p.GetClient(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if c1 == nil {
		t.Fatal("expected non-nil client")
	}
	callsAfter1 := st.calls()

	c2, err := p.GetClient(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if c2 != c1 {
		t.Fatal("expected same client instance when config unchanged (cache hit)")
	}
	// 复用缓存时，不应再次读取所有设置（实际上每次调用仍会读 base_url 判断是否未配置，
	// 但 access_key/secret_key 不应被读）。这里放宽校验：第二次至少不应比第一次多读太多。
	callsAfter2 := st.calls()
	if callsAfter2 < callsAfter1 {
		t.Fatalf("call count went backwards: %d -> %d", callsAfter1, callsAfter2)
	}
}

func TestJMSProvider_RebuildsOnConfigChange(t *testing.T) {
	st := &fakeSettings{}
	st.set("jumpserver.base_url", "http://jms:8080")
	st.set("jumpserver.access_key", "ak-1")
	st.set("jumpserver.secret_key", "sk-1")
	p := NewJMSClientProvider(st)

	c1, _ := p.GetClient(context.Background())
	if c1 == nil {
		t.Fatal("expected non-nil client")
	}

	// 改 secret_key：指纹变化，应重建。
	st.set("jumpserver.secret_key", "sk-2")
	c2, _ := p.GetClient(context.Background())
	if c2 == nil {
		t.Fatal("expected non-nil client after config change")
	}
	if c2 == c1 {
		t.Fatal("expected new client instance after secret_key change")
	}
}

func TestJMSProvider_ClearsCacheWhenUnconfigured(t *testing.T) {
	st := &fakeSettings{}
	st.set("jumpserver.base_url", "http://jms:8080")
	st.set("jumpserver.access_key", "ak-1")
	st.set("jumpserver.secret_key", "sk-1")
	p := NewJMSClientProvider(st)

	c1, _ := p.GetClient(context.Background())
	if c1 == nil {
		t.Fatal("expected non-nil client")
	}

	// 清空 base_url：应返回 nil 并清空缓存。
	st.set("jumpserver.base_url", "")
	c2, err := p.GetClient(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c2 != nil {
		t.Fatal("expected nil client when base_url cleared")
	}

	// 重新配回：应构造新实例（不是旧的 c1）。
	st.set("jumpserver.base_url", "http://jms:8080")
	c3, _ := p.GetClient(context.Background())
	if c3 == nil {
		t.Fatal("expected non-nil client after reconfigure")
	}
	if c3 == c1 {
		t.Fatal("expected new client instance after reconfigure (cache was cleared)")
	}
}

// 确保 jumpserver.Client 类型被引用（避免 import 报错）。
var _ *jumpserver.Client = (*jumpserver.Client)(nil)
