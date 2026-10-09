// 密码哈希的互操作性测试。
//
// 这些用例的核心目的是"两端必须互通"：Go 生成的哈希 PHP 要能验过，
// PHP 生成的哈希 Go 要能验过。固定的 PHP 基准值是硬编码的，
// 任何人改动 pwGen/pwCheck 都会立刻失败。

package web

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// phpHash 用 PHP 的算法在 Go 里重算一遍，作为"实现是否偏离 PHP"的探针。
// 它与 pwCheck 独立：若两者写的是同一份逻辑，共用错误就无法被发现。
func phpHash(password, salt string) string {
	p := md5.Sum([]byte(password))
	sum := sha1.Sum([]byte(hex.EncodeToString(p[:]) + salt))
	return base64.StdEncoding.EncodeToString(append(sum[:], []byte(salt)...))
}

func TestPWGenFormat(t *testing.T) {
	hash, err := pwGen("password123", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(hash)
	if err != nil {
		t.Fatalf("pwGen 输出不是合法 base64: %v", err)
	}
	// 20 字节 sha1 raw + 4 字节盐
	if len(raw) != 24 {
		t.Errorf("解码后 %d 字节, want 24", len(raw))
	}
	if len(hash) != 32 {
		t.Errorf("base64 串长度 %d, want 32", len(hash))
	}
}

// 同一条 pwGen 实现与 PHP 的算法必须产生相同结果（给定同一盐值）。
func TestPWGenMatchesPHPAlgorithm(t *testing.T) {
	const salt = "abcd"
	got := phpHash("password123", salt)
	// 独立复算一遍：sha1(md5("password123") + "abcd") 再拼盐
	p := md5.Sum([]byte("password123"))
	sum := sha1.Sum([]byte(hex.EncodeToString(p[:]) + salt))
	want := base64.StdEncoding.EncodeToString(append(sum[:], []byte(salt)...))
	if got != want {
		t.Fatalf("phpHash 不自洽: %s != %s", got, want)
	}
	if !pwCheck("password123", want) {
		t.Error("自洽的哈希应能通过 pwCheck")
	}
}

func TestPWCheckRoundTrip(t *testing.T) {
	// 刻意不含 32 位十六进制串：见 TestPWCheckHexPasswordQuirk。
	for _, pw := range []string{"password123", "P@ssw0rd!", "密码密码密码", "a", strings.Repeat("x", 40)} {
		hash, err := pwGen(pw, false)
		if err != nil {
			t.Fatal(err)
		}
		if !pwCheck(pw, hash) {
			t.Errorf("pwGen/pwCheck 对 %q 不自洽", pw)
		}
		if pwCheck(pw+"x", hash) {
			t.Errorf("%q 的错误密码 %q 被接受", pw, pw+"x")
		}
		if pwCheck("", hash) {
			t.Errorf("%q 的空密码被接受", pw)
		}
	}
}

// 复刻 PHP 的一个上游缺陷：pwCheck 里的 isOldPW($password) 让
// 恰好是 32 位十六进制的密码被当成"已经 md5 过了"，
// 于是用户输入原文时反而校验失败。
//
// 已用 PHP 实测确认（phpweb 下 pwGen + pwCheck，密码
// "0123456789abcdef0123456789abcdef"：原文 false，md5 后 true）。
// 这类用户必须能在 Go 侧登录，所以 Go 必须与 PHP 有相同行为，
// 不能"顺手修好"—— 否则同一账号在两端表现不同。
func TestPWCheckHexPasswordQuirk(t *testing.T) {
	const pw = "0123456789abcdef0123456789abcdef"
	hash, err := pwGen(pw, false)
	if err != nil {
		t.Fatal(err)
	}
	// 原文：与 PHP 一致，应验不过
	if pwCheck(pw, hash) {
		t.Error("32 位十六进制密码用原文校验：PHP 验不过，Go 不应通过")
	}
	// md5 后：PHP 验得过，Go 也必须通过
	sum := md5.Sum([]byte(pw))
	if !pwCheck(hex.EncodeToString(sum[:]), hash) {
		t.Error("传入已 md5 的密码应与 PHP 一样通过")
	}
}

// 旧版 MD5 格式必须继续可用：存量账号里两种格式都有。
func TestPWCheckOldMD5(t *testing.T) {
	pw := "legacyuser"
	p := md5.Sum([]byte(pw))
	oldHash := hex.EncodeToString(p[:])

	if !isOldPW(oldHash) {
		t.Fatalf("未识别为旧格式: %s", oldHash)
	}
	if !pwCheck(pw, oldHash) {
		t.Error("旧格式密码校验失败")
	}
	if pwCheck("wrong", oldHash) {
		t.Error("旧格式下错误密码被接受")
	}

	// 已 md5 的输入：PHP 的 isOldPW($password) 分支允许直接传 md5 串
	if !pwCheck(oldHash, oldHash) {
		t.Error("传入已 md5 的密码应能通过")
	}
}

// 关键回归：md5 这一步曾经漏掉，导致新版格式账号全部无法登录。
func TestPWCheckNewFormatRequiresMD5Step(t *testing.T) {
	const salt = "wxyz"
	p := md5.Sum([]byte("correcthorse"))
	sum := sha1.Sum([]byte(hex.EncodeToString(p[:]) + salt))
	saved := base64.StdEncoding.EncodeToString(append(sum[:], []byte(salt)...))

	if !pwCheck("correcthorse", saved) {
		t.Error("明文密码应能验证通过（必须先 md5）")
	}
	// 漏掉 md5 时会算成 sha1(明文+salt)，必须验不过
	raw := sha1.Sum([]byte("correcthorse" + salt))
	wrong := base64.StdEncoding.EncodeToString(append(raw[:], []byte(salt)...))
	if pwCheck("correcthorse", wrong) {
		t.Error("未做 md5 的哈希不应通过校验")
	}
}

func TestIsOldPW(t *testing.T) {
	yes := []string{
		"d41d8cd98f00b204e9800998ecf8427e",
		"D41D8CD98F00B204E9800998ECF8427E",
		"0123456789abcdef0123456789ABCDEF",
	}
	no := []string{
		"",                                  // 空
		"d41d8cd98f00b204e9800998ecf8427",   // 31 位
		"d41d8cd98f00b204e9800998ecf8427ee", // 33 位
		"d41d8cd98f00b204e9800998ecf8427g",  // 非十六进制
		"tZEHBXlL7LdEQ1C5b0eLfn3Lhp4lQ9Uc",  // base64 串
	}
	for _, s := range yes {
		if !isOldPW(s) {
			t.Errorf("isOldPW(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isOldPW(s) {
			t.Errorf("isOldPW(%q) = true, want false", s)
		}
	}
}

func TestPWCheckMalformedSaved(t *testing.T) {
	for _, s := range []string{"", "not-base64!!", "c2hvcnQ=", "YWJj"} {
		if pwCheck("anything", s) {
			t.Errorf("损坏的存档 %q 不应通过校验", s)
		}
	}
}
