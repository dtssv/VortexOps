package configapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type configFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Mode     string `json:"mode"`
	IsSecret bool   `json:"is_secret"`
}

// extractFiles 从 content JSON 提取 files 数组。
func extractFiles(content map[string]any) []configFile {
	if content == nil {
		return nil
	}
	raw, ok := content["files"].([]any)
	if !ok {
		return nil
	}
	out := make([]configFile, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		f := configFile{
			Path:    fmt.Sprint(m["path"]),
			Content: fmt.Sprint(m["content"]),
			Mode:    fmt.Sprint(m["mode"]),
		}
		if f.Mode == "" {
			f.Mode = "0644"
		}
		if v, ok := m["is_secret"].(bool); ok {
			f.IsSecret = v
		}
		if f.Path != "" {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// hashFiles 计算 files 部分稳定哈希（用于判断是否变更）。
func hashFiles(files []configFile) string {
	b, _ := json.Marshal(files)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// filesChanged 比较两份 content 的 files 是否不同。
func filesChanged(before, after map[string]any) bool {
	return hashFiles(extractFiles(before)) != hashFiles(extractFiles(after))
}

// listFilePaths 列出 content 中全部文件路径。
func listFilePaths(content map[string]any) []string {
	files := extractFiles(content)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

// getFileContent 按路径取文件内容。
func getFileContent(content map[string]any, path string) string {
	for _, f := range extractFiles(content) {
		if f.Path == path {
			return f.Content
		}
	}
	return ""
}

// mergeSelectedFiles 将 source 中选定的文件合并进 target（按 path 覆盖）。
func mergeSelectedFiles(target, source map[string]any, paths []string) map[string]any {
	if target == nil {
		target = map[string]any{}
	}
	out := cloneContentMap(target)
	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}
	srcFiles := extractFiles(source)
	existing := extractFiles(out)
	byPath := make(map[string]configFile)
	for _, f := range existing {
		byPath[f.Path] = f
	}
	for _, f := range srcFiles {
		if pathSet[f.Path] {
			byPath[f.Path] = f
		}
	}
	merged := make([]any, 0, len(byPath))
	pathsSorted := make([]string, 0, len(byPath))
	for p := range byPath {
		pathsSorted = append(pathsSorted, p)
	}
	sort.Strings(pathsSorted)
	for _, p := range pathsSorted {
		f := byPath[p]
		merged = append(merged, map[string]any{
			"path": f.Path, "content": f.Content, "mode": f.Mode, "is_secret": f.IsSecret,
		})
	}
	out["files"] = merged
	return out
}

func mergeEnvCommandArgs(target, source map[string]any, includeEnv, includeCmd, includeArgs bool) map[string]any {
	out := cloneContentMap(target)
	if includeEnv && source["env"] != nil {
		out["env"] = source["env"]
	}
	if includeCmd && source["command"] != nil {
		out["command"] = source["command"]
	}
	if includeArgs && source["args"] != nil {
		out["args"] = source["args"]
	}
	return out
}

func cloneContentMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func languageForPath(path string) string {
	ext := ""
	if i := strings.LastIndex(path, "."); i >= 0 {
		ext = strings.ToLower(path[i+1:])
	}
	switch ext {
	case "yaml", "yml":
		return "yaml"
	case "json":
		return "json"
	case "properties", "conf", "ini":
		return "ini"
	case "sh", "bash":
		return "shell"
	case "xml":
		return "xml"
	case "py":
		return "python"
	case "go":
		return "go"
	default:
		return "plaintext"
	}
}
