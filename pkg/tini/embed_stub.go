//go:build !embed_tini

// Package tini 提供内置的沙箱 init（tini）二进制。
//
// 这个文件是未启用 embed_tini 标签时的占位实现：普通 `go build` / `go test`
// 在没有 tini-static 产物的环境里也能编译通过，只是二进制里不带 tini，
// 此时 `hustoj install-tini` 会明确报错。
package tini

// Binary 返回 nil：未用 embed_tini 构建时没有内置 tini。
func Binary() []byte { return nil }

// MD5 返回空串：未用 embed_tini 构建时没有内置 tini。
func MD5() string { return "" }
