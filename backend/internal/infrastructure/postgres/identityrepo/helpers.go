// Package identityrepo 通用 PG 辅助函数。
package identityrepo

import (
	"encoding/json"
	"time"
)

// nullableInt64 0 视为 NULL（外键未设置）。
func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// nullableTime 零值视为 NULL。
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// jsonbMarshal 将 map 序列化为 JSONB 兼容的 []byte，nil/空返回 []byte("{}")。
func jsonbMarshal(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}"), err
	}
	return b, nil
}

// scanJSONB 把 JSONB 字节解析到 map。
func scanJSONB(b []byte, m *map[string]any) error {
	if len(b) == 0 {
		*m = map[string]any{}
		return nil
	}
	if err := json.Unmarshal(b, m); err != nil {
		*m = map[string]any{}
		return err
	}
	return nil
}

// jsonbMarshalSlice 将字符串切片序列化为 JSONB 兼容的 []byte，nil/空返回 []byte("[]")。
func jsonbMarshalSlice(s []string) ([]byte, error) {
	if len(s) == 0 {
		return []byte("[]"), nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return []byte("[]"), err
	}
	return b, nil
}

// scanJSONBSlice 把 JSONB 字节解析到字符串切片。
func scanJSONBSlice(b []byte, s *[]string) error {
	if len(b) == 0 {
		*s = []string{}
		return nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		*s = []string{}
		return err
	}
	return nil
}
