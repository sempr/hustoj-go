// 密码哈希，对应 phpweb/include/my_func.inc.php 的 pwGen / pwCheck /
// isOldPW。
//
// 三个函数必须与 PHP 逐字等价，否则会出现"Go 生成的密码 PHP 登不进去"
// 或"PHP 生成的密码 Go 登不进去"。迁移期两端会并存，格式必须互通。

package web

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// saltLen 对应 PHP 的 substr(sha1(rand()), 0, 4)：4 字节。
const saltLen = 4

// isOldPW 判断是否为旧版 MD5 格式（32 位十六进制），对应 my_func.inc.php:566。
func isOldPW(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}

// md5Hex 返回小写十六进制 md5。
func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// pwGen 生成新版密码哈希，对应 my_func.inc.php:541。
//
//	$password = md5($password);
//	$salt     = substr(sha1(rand()), 0, 4);
//	$hash     = base64_encode(sha1($password . $salt, true) . $salt);
//
// sha1 的第二参数 true 表示取 raw（20 字节），因此解码后固定 24 字节。
func pwGen(password string, md5ed bool) (string, error) {
	if !md5ed {
		password = md5Hex(password)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐值失败: %w", err)
	}
	sum := sha1.Sum([]byte(password + string(salt)))
	return base64.StdEncoding.EncodeToString(append(sum[:], salt...)), nil
}

// pwCheck 验证密码，对应 my_func.inc.php:550。
//
// 旧版存的是 md5(password) 的十六进制串；新版存的是
// base64(sha1_raw(md5(password) + salt) + salt)。
//
// "先 md5 再拼 salt" 这一步是新版分支的前提。漏掉它会让所有用新版
// 格式存储的账号都无法登录，而旧格式账号一切正常 —— 表现为
// "部分用户能登、部分不能"，很难从现象反推到这一行。
func pwCheck(password, saved string) bool {
	if isOldPW(saved) {
		if !isOldPW(password) {
			password = md5Hex(password)
		}
		return subtle.ConstantTimeCompare([]byte(password), []byte(saved)) == 1
	}

	svd, err := base64.StdEncoding.DecodeString(saved)
	if err != nil || len(svd) < sha1.Size {
		return false
	}
	salt := string(svd[sha1.Size:])
	if !isOldPW(password) {
		password = md5Hex(password)
	}
	sum := sha1.Sum([]byte(password + salt))
	want := base64.StdEncoding.EncodeToString(append(sum[:], []byte(salt)...))
	return subtle.ConstantTimeCompare([]byte(want), []byte(saved)) == 1
}
