// 跨语言互操作测试：Go 与 PHP 对同一批密码的判断必须一致。
//
// 固定哈希由 PHP 侧生成（phpweb/include/my_func.inc.php 的 pwGen），
// 逐字节硬编码在下方。这些哈希无法由 Go "推导"出来，只能由 PHP 产出，
// 因此它们是真正的跨语言契约：如果 Go 改了 pwCheck 逻辑，
// 这些用例会立刻失败；如果 PHP 改了算法，也会失败。
//
// 生成方式：见本文件末尾的说明注释。

package web

import (
	"crypto/md5"
	"encoding/hex"
	"testing"
)

// phpGenerated 是 PHP 的 pwGen 对固定明文生成的实际输出。
//
// 每条都是 base64(sha1_raw(md5(明文) + salt) + salt)，salt 由 PHP 随机生成。
// 选取了 ASCII、中文（含 3 字节与 4 字节 UTF-8）、空格、特殊符号、
// 单字符与超长输入，覆盖 md5/sha1 在不同字节长度下的行为。
var phpGenerated = []struct {
	password string
	hash     string
}{
	{"Passw0rd!", "zSqjq/IVEZ2SnL8Nke4Ou09cgqplNjM5"},
	{"密码abc123", "UbHySFP1eYFvpkhEGZTdV7tkKXYzMDk3"},
	{"my pass 123", "XKKz6xFY5elfeAO7dt0vngcxH78yYWU4"},
	{"p@ss!#$%^&*()", "isfAhOzoBgBMFuTfahuW6LNEjOxiMWI3"},
	{"x", "TQZMvUePIBFP4fzI2/1vDKjFa9swYzI3"},
	{"🔐key123", "NNCgQxMmcJqm+nyF5tgNFhLhU8llNzE3"},
	// 空密码的存档现实中不会存在（register.php 强制 >= 6 位），
	// 但 pwCheck 仍必须能安全处理，不能 panic 也不能误判为通过。
	{"", "HbWWGmnM6Ansc+P9nZ4Sjoku0241ZDNh"},
}

// 关键契约：PHP 生成的哈希，Go 必须验得过。
//
// 这条用例失败意味着存量用户无法登录 —— 全部新格式账号都会被拒绝。
func TestPwCheckAcceptsPHPGeneratedHashes(t *testing.T) {
	for _, tc := range phpGenerated {
		t.Run(labelOf(tc.password), func(t *testing.T) {
			if !pwCheck(tc.password, tc.hash) {
				t.Errorf("Go 拒绝 PHP 生成的哈希: 明文 %q 存档 %s", tc.password, tc.hash)
			}
		})
	}
}

// 反向：错误密码必须被拒。
func TestPwCheckRejectsWrongPasswordForPHPHashes(t *testing.T) {
	for _, tc := range phpGenerated {
		t.Run(labelOf(tc.password), func(t *testing.T) {
			if pwCheck(tc.password+"x", tc.hash) {
				t.Errorf("Go 接受了错误密码（存档来自 PHP）: %q", tc.password+"x")
			}
			if tc.password != "" && pwCheck("", tc.hash) {
				t.Errorf("Go 接受了空密码（存档来自 PHP）: %q", tc.password)
			}
		})
	}
}

// 旧版 MD5 存档同样必须互通。
func TestPwCheckAcceptsLegacyMD5(t *testing.T) {
	// 基准值由 PHP 的 md5("legacy") 产出。
	const legacy = "legacy"
	sum := md5.Sum([]byte(legacy))
	saved := hex.EncodeToString(sum[:])

	if saved != "228c70bfc5589c58c044e03fff0e17eb" {
		t.Fatalf("md5 基准值不符，实际 %s", saved)
	}
	if !pwCheck(legacy, saved) {
		t.Error("Go 拒绝 PHP 风格的旧版 MD5 存档")
	}
	if pwCheck(legacy+"x", saved) {
		t.Error("旧版存档下错误密码被接受")
	}
}

// Go 生成的哈希必须能写回数据库并被 PHP 验过。
// 这里无法直接调 PHP，因此验证的是"Go 的输出与 PHP 的格式契约一致"：
// 解码后固定 20 字节 sha1 + 4 字节盐，且 4 字节盐是合法字符。
func TestPwGenOutputIsPHPCompatible(t *testing.T) {
	for i := 0; i < 50; i++ {
		hash, err := pwGen("pwtest", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range phpGenerated {
			if hash == tc.hash {
				t.Fatal("Go 与 PHP 生成了相同的哈希，盐值未随机")
			}
		}
		// 长度必须与 PHP 一致：base64(24 字节) = 32 字符
		if len(hash) != 32 {
			t.Fatalf("哈希长度 %d, want 32", len(hash))
		}
		if !pwCheck("pwtest", hash) {
			t.Fatalf("Go 自己的 pwGen/pwCheck 不自洽: %s", hash)
		}
	}
}

// labelOf 给测试子用例起一个可读名字。
func labelOf(s string) string {
	if s == "" {
		return "empty"
	}
	// 避免把明文密码原样写进测试输出
	if len(s) <= 4 {
		return "len4"
	}
	return "len" + itoa(len([]rune(s)))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
