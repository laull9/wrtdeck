// Package webui 通过 embed.FS 把前端构建产物打包进二进制。
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist_content 是 Vite 的输出目录，构建前留有一份占位 index.html
//
//go:embed all:dist
var dist_content embed.FS

// Handler 返回静态资源处理器，未知路径回退到 index.html 以支持前端路由
func Handler() http.Handler {
	sub, err := fs.Sub(dist_content, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "前端资源不可用", http.StatusInternalServerError)
		})
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := strings.TrimPrefix(r.URL.Path, "/")
		if target == "" {
			target = "index.html"
		}
		if !exists(sub, target) {
			serve_index(w, r, sub)
			return
		}
		// index.html 不缓存，保证升级后立即生效；带哈希的静态资源可以长缓存
		if target == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		files.ServeHTTP(w, r)
	})
}

// serve_index 直接吐出 index.html，用于 SPA 前端路由回退
func serve_index(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "前端未构建，请先执行前端构建", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// exists 判断嵌入文件系统中是否存在该文件
func exists(sub fs.FS, name string) bool {
	info, err := fs.Stat(sub, name)
	return err == nil && !info.IsDir()
}
