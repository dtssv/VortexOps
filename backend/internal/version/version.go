// Package version 暴露构建期注入的版本信息。
// 这些变量由 -ldflags -X 在构建时设置，缺省值为占位符。
package version

import "fmt"

// 以下变量在构建时通过 ldflags 注入。
var (
	// Version 语义化版本号，如 v1.2.3。缺省 dev。
	Version = "dev"
	// BuildDate 构建时间（RFC3339）。
	BuildDate = "unknown"
	// Commit 源码 commit hash。
	Commit = "unknown"
)

// String 返回完整版本字符串。
func String() string {
	return fmt.Sprintf("vortexops %s (commit: %s, built: %s)", Version, Commit, BuildDate)
}
