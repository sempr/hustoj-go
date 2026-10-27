package web

import (
	"strings"
	"testing"
)

func TestValidEmail(t *testing.T) {
	// 正常地址必须通过。
	ok := []string{
		"user@example.com",
		"first.last@sub.domain.co.uk",
		"user+tag@example.com",
		"a@b.co",
		"o'brien@example.com",
		"very.long.address.with.many.labels@a-very-long-domain-name.example.museum",
	}
	for _, s := range ok {
		if !validEmail(s) {
			t.Errorf("validEmail(%q) = false, want true", s)
		}
	}

	// 明显非法的必须拒绝。
	bad := []string{
		"",
		"no-at-sign",
		"@example.com",
		"user@",
		"user@@example.com",
		"user@exam ple.com",
		"user name@example.com",
		"user@.com",
		"user@example..com",
		"user@example.com.",
		"user@\n",
		"Name <a@b.com>", // 带显示名不应被接受
		"<a@b.com>",
	}
	for _, s := range bad {
		if validEmail(s) {
			t.Errorf("validEmail(%q) = true, want false", s)
		}
	}
}

func TestValidEmailLengthLimit(t *testing.T) {
	// 254 是上限，255 必须拒绝。
	const local = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 64
	// 域名需要 254-64-1 = 189 字节，按每标签 ≤63 切成 "63.63.57.com"。
	addr := local + "@" + joinDots([]string{
		strings.Repeat("b", 63),
		strings.Repeat("c", 63),
		strings.Repeat("d", 57),
		"com",
	})
	if len(addr) != 254 {
		t.Fatalf("构造的用例长度 = %d, want 254", len(addr))
	}
	if !validEmail(addr) {
		t.Error("254 字符的合法邮箱被拒绝")
	}
	if validEmail(addr + "a") {
		t.Error("255 字符的邮箱被接受")
	}
}

func joinDots(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "."
		}
		out += p
	}
	return out
}

func TestValidUsername(t *testing.T) {
	ok := []string{"abc", "ABC123", "user_name", "user-name", "12", "*star", "*"}
	for _, s := range ok {
		if !validUsername(s) {
			t.Errorf("validUsername(%q) = false, want true", s)
		}
	}
	// "*" 只允许出现在首位，这是 PHP 侧的历史约束。
	bad := []string{"", "a b", "用户", "a*b", "a*b*c", "a.", ".a", "a@b", "a\tb", "*a b"}
	for _, s := range bad {
		if validUsername(s) {
			t.Errorf("validUsername(%q) = true, want false", s)
		}
	}
}
