// Package webassets 将 Web 构建产物嵌入服务端，运行时不依赖资源目录。
package webassets

import (
	"embed"
	"io/fs"
)

// .keep 允许未构建前端的源码检出运行 Go 单元测试；发布构建必须先执行 make web-build。
//
//go:embed all:assets
var files embed.FS

// Files 返回编译时嵌入的前端文件系统。
func Files() fs.FS {
	assets, _ := fs.Sub(files, "assets") // 固定的内嵌目录，路径始终有效。
	return assets
}
