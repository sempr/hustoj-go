// 验证码流程测试。
//
// 覆盖的是"答案存哪、失败后怎么升级难度"这两个最容易在迁移中出错的地方：
// 答案存在 Go 会话里（Redis），而 /vcode.php 与 /login.php 都在 Go 侧，
// 两边看到的必须是同一份状态。

package web

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// getVCode 取一次验证码图片，返回 Go 会话 ID 与图片字节。
func getVCode(t *testing.T, srv *Server, jar *cookieJar) (string, []byte) {
	t.Helper()
	rec := jar.get(t, srv, "/vcode.php")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /vcode.php = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if jar.sid == "" {
		t.Fatal("取验证码时未下发 Go 会话 Cookie")
	}
	return jar.sid, rec.Body.Bytes()
}

// 当前会话里的验证码答案。
func currentVCode(t *testing.T, srv *Server, sid string) (string, bool) {
	t.Helper()
	sess, err := srv.sessions.Get(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	return sess.VCode, sess.VFail
}

// 验证码图片必须是合法 PNG，且答案落在会话里。
//
// 用 PNG 头而不是"图片非空"来断言：内容损坏的 PNG 也能通过非空检查，
// 而浏览器只会显示一个破图。
func TestVCodeWritesAnswerIntoGoSession(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	sid, img := getVCode(t, f.srv, jar)

	if _, err := png.Decode(bytes.NewReader(img)); err != nil {
		t.Fatalf("验证码不是合法 PNG: %v", err)
	}

	answer, vfail := currentVCode(t, f.srv, sid)
	if len(answer) != 4 {
		t.Errorf("首次发码位数 = %d, want 4 (%q)", len(answer), answer)
	}
	if vfail {
		t.Error("首次发码不应处于失败态")
	}
}

// 两次取码应给出不同答案，否则旧图上的答案能一直用。
func TestVCodeRegeneratesAnswer(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	sid, _ := getVCode(t, f.srv, jar)
	first, _ := currentVCode(t, f.srv, sid)

	getVCode(t, f.srv, jar)
	second, _ := currentVCode(t, f.srv, sid)

	if first == second {
		t.Errorf("两次发码答案相同: %q", first)
	}
}

// 校验失败后要清空答案并置失败位：不清空的话同一个答案能无限重放。
func TestVCodeCheckAndFail(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	sid, _ := getVCode(t, f.srv, jar)
	answer, _ := currentVCode(t, f.srv, sid)

	ctx := context.Background()
	if !f.srv.checkVCode(ctx, sid, answer) {
		t.Fatal("正确答案应通过校验")
	}
	if f.srv.checkVCode(ctx, sid, answer+"9") {
		t.Error("错误答案不应通过")
	}
	if f.srv.checkVCode(ctx, sid, "") {
		t.Error("空答案不应通过")
	}

	if err := f.srv.markVFail(ctx, sid); err != nil {
		t.Fatal(err)
	}
	got, vfail := currentVCode(t, f.srv, sid)
	if got != "" {
		t.Errorf("失败后答案应被清空, got %q", got)
	}
	if !vfail {
		t.Error("失败后应置上失败位")
	}
	// 清空之后原答案必须失效
	if f.srv.checkVCode(ctx, sid, answer) {
		t.Error("失败后原答案不应再能通过")
	}
}

// 失败过一次后发码位数应加大，对应 phpweb/vcode.php:37-42。
func TestVCodeRaisesDifficultyAfterFailure(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	sid, _ := getVCode(t, f.srv, jar)

	ctx := context.Background()
	if err := f.srv.markVFail(ctx, sid); err != nil {
		t.Fatal(err)
	}

	getVCode(t, f.srv, jar)
	answer, vfail := currentVCode(t, f.srv, sid)
	if !vfail {
		t.Error("重新发码后失败位应保留")
	}
	if len(answer) != 8 {
		t.Errorf("失败后位数 = %d, want 8 (%q)", len(answer), answer)
	}
}

// 答案必须全是数字：PHP 侧用 vcodetype='n'，非数字会与用户习惯不符。
func TestVCodeIsNumeric(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	sid, _ := getVCode(t, f.srv, jar)
	answer, _ := currentVCode(t, f.srv, sid)
	for _, c := range answer {
		if c < '0' || c > '9' {
			t.Fatalf("答案含非数字字符: %q", answer)
		}
	}
}

// 校验走完整 HTTP 链路：登录页 → 验证码 → 登录。
func TestLoginWithVCode(t *testing.T) {
	f := newAuthFixture(t)
	f.srv.cfg.VCode = true
	f.seedUser(t, authTestUser{userID: "vuser", password: "secret123"})

	jar := &cookieJar{}
	rec := jar.get(t, f.srv, "/loginpage.php")
	if !csrfRe.MatchString(rec.Body.String()) {
		t.Fatal("登录页未渲染表单")
	}
	if !strings.Contains(rec.Body.String(), `name="vcode"`) {
		t.Error("启用验证码时登录页应渲染验证码输入框")
	}

	sid, _ := getVCode(t, f.srv, jar)
	answer, _ := currentVCode(t, f.srv, sid)
	token := tokenFromBody(t, rec.Body.String())

	// 错答案：应被挡下
	rec = jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"vuser"}, "password": {"secret123"},
		"vcode": {"0000000"[:len(answer)]}, "csrf_token": {token},
	})
	if strings.Contains(rec.Body.String(), "index.php") {
		t.Error("验证码错误却登录成功")
	}
	if !strings.Contains(rec.Body.String(), "验证码错误") {
		t.Errorf("应提示验证码错误; body=%s", rec.Body.String())
	}

	// 失败位已置上，同一答案作废；重新取码后用新答案登录
	getVCode(t, f.srv, jar)
	answer, _ = currentVCode(t, f.srv, sid)
	rec = jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"vuser"}, "password": {"secret123"},
		"vcode": {answer}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("正确验证码登录 = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
}

// 关闭验证码时不应校验，登录页也不该出现输入框。
func TestVCodeDisabledIsIgnored(t *testing.T) {
	f := newAuthFixture(t)
	f.srv.cfg.VCode = false
	f.seedUser(t, authTestUser{userID: "nocode", password: "secret123"})

	jar := &cookieJar{}
	rec := jar.get(t, f.srv, "/loginpage.php")
	if strings.Contains(rec.Body.String(), `name="vcode"`) {
		t.Error("关闭验证码时不应渲染验证码输入框")
	}

	token := tokenFromBody(t, rec.Body.String())
	rec = jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"nocode"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("登录 = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
}

// 图片绝不能被缓存：否则刷新后图变了、答案没变，用户永远输不对。
func TestVCodeNotCacheable(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	rec := jar.get(t, f.srv, "/vcode.php")
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, 应含 no-store", cc)
	}
	if rec.Header().Get("Pragma") != "no-cache" {
		t.Error("缺少 Pragma: no-cache")
	}
}

// 尚未登录的匿名用户也要能拿到验证码，故而必须有 Go 会话。
func TestVCodeCreatesSessionForAnonymous(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	sid, _ := getVCode(t, f.srv, jar)

	id := f.srv.resolveIdentity(nil, newReqWithSID(sid))
	if id.LoggedIn {
		t.Error("取验证码不应把用户变成已登录")
	}
	// 但会话必须真实存在，否则答案无处安放
	if _, err := f.srv.sessions.Get(context.Background(), sid); err != nil {
		t.Errorf("匿名验证码会话应已建立: %v", err)
	}
}
