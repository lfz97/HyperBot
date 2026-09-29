//go:build tools

// Package tools 以 go.mod 钉住 .gsx 代码生成器版本，
// 使 `go generate ./service/tui` 用与 go.mod 一致的 tui CLI 运行。
package tools

import (
	_ "github.com/grindlemire/go-tui/cmd/tui"
)
