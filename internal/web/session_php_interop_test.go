// 本文件验证 Go 与 PHP 会话在"并存期"的边界。
//
// 关键约束：Go 只读 PHP 会话，从不写。
//
// 切流前的老用户带着 PHPSESSID 过来，Go 需要读出他的登录态才能接住；
// 但 PHP 自己也在写同一个文件（vcode、csrf_keys、contest 权限都在里面）。
// 双向可写意味着两套实现必须完全对齐序列化细节、字段语义和锁策略，
// 任何一边改动都可能静默清掉另一边的状态。
//
// 所以架构上明确切开：登录态由各自独立存储负责，
// Go 用 Redis（HJGOSESSID），PHP 继续用文件（PHPSESSID），
// PHP 会话对 Go 而言是只读的迁移输入。
//
// 单元测试只覆盖解析器。真正的"PHP 写的会话 Go 读得懂"需要拉起真实 PHP，
// 参见 php_session_test.go 文件头的说明。

package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// testOJName 必须与 phpweb/include/lib.php 里的 $OJ_NAME 一致，
// 否则解析出的键名对不上，测试会以"读不到登录态"的形式失败。
const testOJName = "吉尔吉斯倔驴大学OJ"

// bridgeDir 返回一个空的临时 session 目录。
//
// 用 t.TempDir 而不是生产的 /var/lib/php/sessions：这些用例会往里写文件
// （模拟 PHP 侧写入），绝不能碰到线上会话。
func bridgeDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestPHPGoSessionReadsPHPSession 验证 Go 能读懂 PHP 写的会话。
func TestPHPGoSessionReadsPHPSession(t *testing.T) {
	dir := bridgeDir(t)
	const sid = "gotest1234567890abcdef"
	b := NewSessionBridge(dir, testOJName)

	// 模拟 PHP 写出的会话文件：键名带 $OJ_NAME_ 前缀。
	path := filepath.Join(dir, "sess_"+sid)
	seed := testOJName + `_user_id|s:7:"goadmin";` +
		testOJName + `_administrator|b:1;` +
		testOJName + `_nick|s:8:"Go写入";`
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}

	id := b.resolveSID(sid)
	if !id.LoggedIn {
		t.Fatal("Go 无法解析 PHP 写的会话")
	}
	if id.UserID != "goadmin" {
		t.Errorf("UserID = %q, want goadmin", id.UserID)
	}
	if !id.Admin {
		t.Error("Admin 标记未解析")
	}
	if id.Nick != "Go写入" {
		t.Errorf("Nick = %q", id.Nick)
	}
}

// TestPHPGoSessionWritesPHPSession 验证 Go 写的会话格式 PHP 读得懂。
//
// Go 侧绝不能写 PHP 会话文件。
//
// 这条契约是 PHP/Go 并存期的核心：两边各写各的会互相覆盖 ——
// Go 改写会话时丢掉 PHP 刚写入的 vcode，验证码流程就会诡异地一直失败。
// 所以切流后 PHP 仍是登录态的"所有者"，Go 只读。
func TestGoSessionNeverWritesPHPSession(t *testing.T) {
	dir := bridgeDir(t)
	const sid = "readonlycheck123456789ab"
	b := NewSessionBridge(dir, testOJName)

	path := filepath.Join(dir, "sess_"+sid)
	seed := testOJName + `_user_id|s:5:"phper";` +
		testOJName + `_vcode|s:4:"ABCD";` +
		testOJName + `_group_name|s:5:"guest";`
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "PHPSESSID", Value: sid})
	id := b.Resolve(req)
	if !id.LoggedIn || id.UserID != "phper" {
		t.Fatalf("应能读出 PHP 会话: %+v", id)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != seed {
		t.Errorf("Go 侧读取改动了 PHP 会话文件:\n got %q\nwant %q", after, seed)
	}

	// Store 上不应还留着任何改写入口：
	// 与其靠 code review 保证"没人调用"，不如让 API 本身不具备该能力。
	var st any = b.store
	if _, ok := st.(interface {
		update(string, func(*phpSession) error) error
	}); ok {
		t.Error("SessionStore 仍暴露 update，应移除写路径")
	}
}
