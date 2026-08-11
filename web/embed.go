// Package web 把构建后的前端产物嵌入二进制。
//
// 生产运行时不需要 Node 或 Vite dev server：
//
//	React → vite build → web/dist → go:embed → DomainHunter 单容器
//
// dist 目录是构建产物且随仓库提交，这样 `go build` 与 GoReleaser 在没有
// Node 环境时同样可以产出完整可用的二进制。修改前端后必须重新
// `npm run build` 并提交 dist。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist 返回前端构建产物的文件系统（根目录为 dist 内部）
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// go:embed 保证了 dist 目录存在，这里只可能是构建期配置错误
		panic(err)
	}
	return sub
}
