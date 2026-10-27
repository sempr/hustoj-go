// PHP session 解析与写回的测试。
//
// 这些用例都锚定"线上真实会话文件的格式"，而不是构造的理想输入：
// 前缀是中文的 $OJ_NAME、值里含 ; } 等分隔符、权限键名带数字后缀。

package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 线上真实会话文件的内容（已脱敏，值本身无关紧要）。
//
// 注意 s:N 里的 N 是字节数而非字符数：PHP 的 strlen() 计字节，
// 中文昵称 "用户" 是 6 字节，所以必须写 s:6。
const realSession = "吉尔吉斯倔驴大学OJ_nick|s:6:\"用户\";" +
	"吉尔吉斯倔驴大学OJ_user_id|s:6:\"tester\";" +
	"吉尔吉斯倔驴大学OJ_group_name|s:4:\"user\";" +
	"吉尔吉斯倔驴大学OJ_administrator|b:1;" +
	"吉尔吉斯倔驴大学OJ_c1000|b:1;" +
	"吉尔吉斯倔驴大学OJ_c1001|b:0;" +
	"吉尔吉斯倔驴大学OJ_m1000|b:1;" +
	"吉尔吉斯倔驴大学OJ_csrf_keys|a:2:{i:0;s:32:\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\";i:1;s:32:\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\";}"

func TestParsePHPSessionChinesePrefix(t *testing.T) {
	s := parsePHPSession(realSession)

	if got, _ := s.getString("吉尔吉斯倔驴大学OJ_user_id"); got != "tester" {
		t.Errorf("user_id = %q, want %q", got, "tester")
	}
	if got, _ := s.getString("吉尔吉斯倔驴大学OJ_nick"); got != "用户" {
		t.Errorf("nick = %q, want %q", got, "用户")
	}
	if !s.getBool("吉尔吉斯倔驴大学OJ_administrator") {
		t.Error("administrator = false, want true")
	}
	// b:0; 必须判为 false —— 私有比赛受邀标记写错会泄露题目。
	if s.getBool("吉尔吉斯倔驴大学OJ_c1001") {
		t.Error("c1001 = true, want false")
	}
	keys := s.getStringSlice("吉尔吉斯倔驴大学OJ_csrf_keys")
	if len(keys) != 2 {
		t.Fatalf("csrf_keys len = %d, want 2", len(keys))
	}
	if keys[0] != strings.Repeat("a", 32) || keys[1] != strings.Repeat("b", 32) {
		t.Errorf("csrf_keys = %v", keys)
	}
}

// 分隔符出现在值内部时不能解析错位。
// 旧的按分隔符扫描的实现会在昵称含 ";" 时截断。
func TestParsePHPSessionSeparatorsInValue(t *testing.T) {
	raw := "OJ_nick|s:10:\"a;b}c:d{ef\";OJ_user_id|s:2:\"u1\";"
	s := parsePHPSession(raw)

	if got, _ := s.getString("OJ_nick"); got != "a;b}c:d{ef" {
		t.Errorf("nick = %q, want %q", got, "a;b}c:d{ef")
	}
	if got, _ := s.getString("OJ_user_id"); got != "u1" {
		t.Errorf("user_id = %q, want %q", got, "u1")
	}
}

// 嵌套数组必须能原样读出。
//
// 这里断言的是"原始序列化文本被完整保留"，而不是往返相等：
// Go 已不再写回 PHP 会话（见文件头），序列化能力本身不存在了。
// 但保留原始文本仍然重要 —— 解析器若在嵌套层数上截断，
// 会让 getStringSlice 之类的读取悄悄拿到半个值。
func TestParsePHPSessionNestedArray(t *testing.T) {
	raw := `OJ_a|a:2:{i:0;s:1:"x";s:3:"key";a:1:{i:0;b:1;}}`
	s := parsePHPSession(raw)

	if !s.has("OJ_a") {
		t.Fatal("缺少键 OJ_a")
	}
	if got := s.vals["OJ_a"]; got != `a:2:{i:0;s:1:"x";s:3:"key";a:1:{i:0;b:1;}}` {
		t.Errorf("嵌套数组未原样保留:\n got %s", got)
	}
}

func TestParsePHPSessionNullAndInt(t *testing.T) {
	raw := "OJ_vcode|N;OJ_attempt|i:42;OJ_off|b:0;"
	s := parsePHPSession(raw)

	if v, ok := s.getString("OJ_vcode"); ok || v != "" {
		t.Errorf("N; 应解析为空字符串，实际 %q ok=%v", v, ok)
	}
	if n, ok := phpIntValue(s.vals["OJ_attempt"]); !ok || n != 42 {
		t.Errorf("attempt = %d ok=%v, want 42", n, ok)
	}
	if s.getBool("OJ_off") {
		t.Error("b:0; 应为 false")
	}
	// 值为 null 时 getBool 也应为 false
	if s.getBool("OJ_vcode") {
		t.Error("N; 的 getBool 应为 false")
	}
}

// 截断的会话文件不应 panic，也不应丢掉已解析出的部分。
func TestParsePHPSessionTruncated(t *testing.T) {
	raw := "OJ_user_id|s:6:\"tester\";OJ_csrf_keys|a:5:{i:0;s:3:\"abc\";"
	s := parsePHPSession(raw)

	if got, _ := s.getString("OJ_user_id"); got != "tester" {
		t.Errorf("应保留已解析的键，user_id = %q", got)
	}
}

func TestSessionIDSafety(t *testing.T) {
	ok := []string{"abc123", "a-b,c", strings.Repeat("a", 128)}
	bad := []string{
		"",                       // 空
		"../../etc/passwd",       // 路径穿越
		"a/b",                    // 斜杠
		`a\b`,                    // 反斜杠
		"a b",                    // 空格
		"a\x00b",                 // NUL
		"a\nb",                   // 换行
		"..",                     // 上级目录
		strings.Repeat("a", 129), // 超长
		"a.b",                    // 点：PHP 不会生成
	}
	for _, v := range ok {
		if !sessionIDAllowed(v) {
			t.Errorf("sessionIDAllowed(%q) = false, want true", v)
		}
	}
	for _, v := range bad {
		if sessionIDAllowed(v) {
			t.Errorf("sessionIDAllowed(%q) = true, want false", v)
		}
	}
}

// Go 侧读 PHP 会话时必须看到全部键，且绝不修改那个文件。
//
// 旧实现有一个"读-改-写 + flock"的往返测试，用来保证 Go 写回 PHP 时
// 不丢 PHP 自己写的键。现在 Go 明确不写 PHP 会话（PHP 的登录态仍归 PHP 管，
// 两边各写各的会互相覆盖），所以契约反过来了：
//
//	读到要全，文件要一个字节都不动。
//
// 未知键必须保留是因为迁移期 Go 仍要读 PHP 侧的状态，
// 解析时若丢键，切流前的用户会莫名其妙掉登录。
func TestSessionStoreReadIsSideEffectFree(t *testing.T) {
	dir := t.TempDir()
	st := NewSessionStore(dir, "OJ")

	// 模拟 PHP 侧签发的会话：含一个 Go 不认识的键。
	seed := "OJ_vcode|s:4:\"8xkt\";OJ_user_id|s:2:\"u1\";OJ_vfail|b:0;"
	path := filepath.Join(dir, "sess_abc123")
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := st.get("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.getString(st.key("vcode")); v != "8xkt" {
		t.Errorf("vcode = %q, want 8xkt", v)
	}
	if v, _ := got.getString(st.key("user_id")); v != "u1" {
		t.Errorf("user_id = %q, want u1", v)
	}
	if got.getBool(st.key("vfail")) {
		t.Error("b:0; 的 vfail 应为 false")
	}
	// 未知键也要读得到：Go 自己的解析器不认它，但不能因此丢弃。
	if _, ok := got.getString(st.key("some_php_only_key")); ok {
		t.Error("未写入过的键不应凭空出现")
	}
	if len(got.vals) != 3 {
		t.Errorf("解析出 %d 个键, want 3; vals=%v", len(got.vals), got.vals)
	}

	// 关键：文件必须原封不动。
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != seed {
		t.Errorf("Go 侧读取改动了 PHP 会话文件:\n got %q\nwant %q", after, seed)
	}
}

func TestSessionStoreRejectsBadID(t *testing.T) {
	dir := t.TempDir()
	st := NewSessionStore(dir, "OJ")

	if _, err := st.get("../escape"); err == nil {
		t.Error("应拒绝含路径穿越的会话 ID")
	}
	// 确认没有在目录外创建文件
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "sess_escape")); err == nil {
		t.Error("不应在 session 目录外创建文件")
	}
}
