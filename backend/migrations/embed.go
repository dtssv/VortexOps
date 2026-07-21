// Package migrations 通过 embed 嵌入 SQL 迁移文件，供 golang-migrate 加载。
package migrations

import "embed"

// FS 嵌入 migrations 目录下所有 .sql 文件。
//
//go:embed *.sql
var FS embed.FS
