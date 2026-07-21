package buildrepo

import "encoding/json"

// jsonDecode 解码 JSONB 字节到目标。
func jsonDecode(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
