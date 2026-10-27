// 登录/注册验证码，对应 phpweb/vcode.php 与 login.php:38-41。
//
// 答案存在会话里（PHP 存 $_SESSION，Go 存 Redis 的 GoSession），
// 而不是存在一个独立的短时 key：这样"清掉答案"和"标记失败"能在
// 同一个会话上原子地表达，和 PHP 的行为一致。

package web

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// setVCode 把当前会话的验证码答案记为 answer。
//
// 由 vcode.php 的图片处理端在生成图片后调用。
func (s *Server) setVCode(ctx context.Context, sid, answer string) error {
	_, err := s.sessions.Update(ctx, sid, func(sess *GoSession) error {
		sess.VCode = answer
		return nil
	})
	return err
}

// checkVCode 校验 answer 是否等于当前会话的验证码答案。
//
// 对应 login.php:38：答案不等、为空、或压根没发过码，一律判失败。
// 比较用 subtle.ConstantTimeCompare：这里比较的是用户可见的验证码，
// 真正的风险是攻击者用它逐位试探，但顺手做成常量时间没有成本。
func (s *Server) checkVCode(ctx context.Context, sid, answer string) bool {
	sess, err := s.sessions.Get(ctx, sid)
	if err != nil || sess.VCode == "" {
		return false
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(answer), []byte(sess.VCode)) == 1
}

// markVFail 记一次验证码校验失败，对应 login.php:39-40。
//
// 失败时要把答案清掉：否则同一个答案可以无限次重放，
// 而 vcode.php 下次发码会依据 VFail 加大难度。
func (s *Server) markVFail(ctx context.Context, sid string) error {
	_, err := s.sessions.Update(ctx, sid, func(sess *GoSession) error {
		sess.VCode = ""
		sess.VFail = true
		return nil
	})
	return err
}

// vcodeDifficulty 返回本次发码的位数与字符集。
//
// 对应 phpweb/vcode.php:37-42：失败过就用 8 位纯数字，
// 否则 4 位纯数字。区分难度是为了让暴力猜码的成本随失败次数上升。
func vcodeDifficulty(vfail bool) (length int, numeric bool) {
	if vfail {
		return 8, true
	}
	return 4, true
}

// vcodeFromRequest 取出请求里的验证码答案。
func vcodeFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.FormValue("vcode"))
}

// markVFailPassed 在验证码答对后清掉失败标记。
//
// 对应 submit.php:50 的 `$_SESSION[..._vfail] = false`。
// 与 markVFail 分成两个函数而不是加一个布尔参数：这两个动作总是
// 成对出现在不同分支里，加参数会诱使调用方传错方向。
func (s *Server) markVFailPassed(ctx context.Context, sid string) error {
	_, err := s.sessions.Update(ctx, sid, func(sess *GoSession) error {
		sess.VCode = ""
		sess.VFail = false
		return nil
	})
	return err
}
