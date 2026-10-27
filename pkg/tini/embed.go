//go:build embed_tini

// Package tini 提供内置的沙箱 init（tini）二进制。
//
// 用 `-tags embed_tini` 构建时，静态链接的 tini-static 会被打进主二进制，
// 部署机上执行 `hustoj install-tini` 即可落盘，不必再单独分发这个文件。
package tini

import (
	"crypto/md5"
	_ "embed"
	"encoding/hex"
)

//go:embed tini-static
var binary []byte

// Binary 返回内置的 tini 静态二进制。
func Binary() []byte { return binary }

// MD5 返回内置二进制的 md5，落盘后用它校验。
func MD5() string {
	sum := md5.Sum(binary)
	return hex.EncodeToString(sum[:])
}
