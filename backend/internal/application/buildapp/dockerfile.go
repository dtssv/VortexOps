package buildapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/vortexops/vortexops/internal/domain/build"
)

// renderDockerfileTemplate 使用 text/template 渲染 Dockerfile 模板。
// 模板内可用的占位符：
//   - {{.BaseImage}}    运行时基础镜像引用（如 eclipse-temurin:17-jre）
//   - {{.BuildCommand}} 构建命令（如 mvn -B clean package -DskipTests）
//   - {{.ArtifactPath}} 制品路径（如 target/*.jar）
//   - {{.Entrypoint}}   运行时启动命令（JSON 数组字面量，如 ["java","-jar","/app/app.jar"]）
//
// 渲染失败（模板语法错误或缺失字段）时返回错误，由调用方决定是否中止构建。
func renderDockerfileTemplate(tmplText string, data map[string]string) (string, error) {
	if tmplText == "" {
		return "", fmt.Errorf("empty dockerfile template")
	}
	t, err := template.New("dockerfile").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// EffectiveEntrypoint 返回 Dockerfile 渲染用的实际 ENTRYPOINT。
// is_web 时在应用启动命令之外额外启动 nginx（nginx 以 daemon 后台运行，应用为前台主进程）。
func EffectiveEntrypoint(bi *build.BaseImage) []string {
	if bi == nil {
		return []string{"sh", "-c", "exec \"$@\""}
	}
	if !bi.IsWeb {
		ep := bi.Entrypoint
		if len(ep) == 0 {
			return []string{"sh", "-c", "exec \"$@\""}
		}
		return ep
	}
	appShell := entrypointToShellCommand(bi.Entrypoint)
	if appShell == "" {
		return []string{"nginx", "-g", "daemon off;"}
	}
	return []string{"sh", "-c", "nginx && " + appShell}
}

func entrypointToShellCommand(ep []string) string {
	if len(ep) == 0 {
		return ""
	}
	if len(ep) >= 3 && ep[0] == "sh" && ep[1] == "-c" {
		return ep[2]
	}
	if len(ep) == 1 {
		return "exec " + shellQuoteArg(ep[0])
	}
	parts := make([]string, len(ep))
	for i, a := range ep {
		parts[i] = shellQuoteArg(a)
	}
	return "exec " + strings.Join(parts, " ")
}

func shellQuoteArg(s string) string {
	if strings.ContainsAny(s, " '\"$&|;<>()") {
		return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
	}
	return s
}

// dockerfileTemplateData 根据 BaseImage 与制品路径构造模板渲染数据。
// Entrypoint 序列化为 JSON 数组字面量（如 ["java","-jar","/app/app.jar"]），
// 空数组时回退为 ["sh","-c","exec \"$@\""]，避免模板里出现 ENTRYPOINT [] 导致语法错误。
// 调用方应优先使用此 helper 而非手拼 map，保证 Entrypoint 渲染一致。
func dockerfileTemplateData(bi *build.BaseImage, artifactPath string) (map[string]string, error) {
	ep := EffectiveEntrypoint(bi)
	epJSON, err := json.Marshal(ep)
	if err != nil {
		return nil, fmt.Errorf("marshal entrypoint: %w", err)
	}
	return map[string]string{
		"BaseImage":    bi.ImageRef,
		"ArtifactPath": artifactPath,
		"Entrypoint":   string(epJSON),
	}, nil
}

// jsonBuildArgs 将 build_args map 序列化为 JSON 字符串（供 Jenkins pipeline / Tekton Task 透传）。
// 空 map 或序列化失败时返回空字符串。
func jsonBuildArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(b)
}

// sanitizeImageTag 把任意字符串转为合法的 OCI 镜像 tag 片段：
// 小写化，非 [a-z0-9.-] 字符替换为 '-'，合并连续 '-'，去首尾 '-'。
// 用于把应用名嵌入镜像版本号（<app_name>-<unix>）。
func sanitizeImageTag(s string) string {
	if s == "" {
		return ""
	}
	out := make([]byte, 0, len(s))
	prevDash := true // 抑制开头 '-'
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32) // 小写
			prevDash = false
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.':
			out = append(out, c)
			prevDash = false
		default:
			// 非法字符（含 '_'、中文等）替换为 '-'，合并连续 '-'
			if !prevDash {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	// 去尾 '-'
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
