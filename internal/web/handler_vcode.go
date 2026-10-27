// 验证码图片，对应 phpweb/vcode.php。
//
// 必须和登录/注册一起切到 Go，原因很直接：验证码的答案存在会话里。
// 如果图片仍由 PHP 生成，答案就写在 PHPSESSID 里，而 Go 的登录流程读的是
// Redis —— 两边永远对不上，表现为"验证码怎么输都错"。
// 拆开迁移没有意义，所以 /vcode.php 与 /login.php 必须同一个窗口上线。
//
// 绘图用 dchest/captcha（自带字体与扭曲/干扰线，无外部资源依赖），
// 但**不**用它的答案存储：答案放进我们自己的 Go 会话，
// 这样"清空答案"和"标记失败"才能和登录校验共用同一份状态。

package web

import (
	"net/http"
	"time"

	"github.com/dchest/captcha"
)

// vcodeCacheHeaders 让验证码图片不被任何中间层缓存。
//
// 少这一行，用户刷新登录页拿到的还是上一次的图，
// 于是按新图输入必然失败 —— 而失败会把难度提上去，越试越难。
func vcodeCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", time.Unix(0, 0).UTC().Format(http.TimeFormat))
}

// handleVCode 生成验证码图片 GET /vcode.php。
func (s *Server) handleVCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 匿名用户也要有会话来挂答案，所以这里不能只解析登录态。
	sid, err := s.startGoSession(w, r)
	if err != nil {
		s.log.Error("建立验证码会话失败", "err", err)
		http.Error(w, "验证码不可用", http.StatusInternalServerError)
		return
	}

	sess, err := s.sessions.Get(r.Context(), sid)
	if err != nil {
		s.log.Error("读取验证码会话失败", "err", err)
		http.Error(w, "验证码不可用", http.StatusInternalServerError)
		return
	}

	// 失败过就加大难度，对应 phpweb/vcode.php:37-42。
	length, _ := vcodeDifficulty(sess.VFail)
	digits := captcha.RandomDigits(length)
	answer := digitsToAnswer(digits)

	if err := s.setVCode(r.Context(), sid, answer); err != nil {
		s.log.Error("写入验证码答案失败", "err", err)
		http.Error(w, "验证码不可用", http.StatusInternalServerError)
		return
	}

	vcodeCacheHeaders(w)
	w.Header().Set("Content-Type", "image/png")

	// id 只用作绘图随机数的种子，答案不靠它查，所以可以随手生成。
	img := captcha.NewImage(newVCodeSeed(), digits, 15*length, 30)
	if _, err := img.WriteTo(w); err != nil {
		// 已经开始写响应体了，只能记日志：此时再改状态码会产生
		// "header 已发送" 的二次错误，掩盖真正的失败。
		s.log.Error("写出验证码图片失败", "err", err)
	}
}

// digitsToAnswer 把绘图用的数字字节转成用户看到的字符串。
//
// captcha.RandomDigits 返回的是 0-9 的原始字节，不是 ASCII 字符，
// 直接 string() 会得到 "\x01\x02" 这种控制字符 ——
// 画出来是对的（库内部就是按字节绘图），但存进会话的答案会没法输入。
// 这里补上 '0' 的偏移，与库自己的 Verify 逻辑保持一致。
func digitsToAnswer(digits []byte) string {
	buf := make([]byte, len(digits))
	for i, d := range digits {
		buf[i] = '0' + d
	}
	return string(buf)
}

// newVCodeSeed 返回绘图用的随机种子。
//
// 它不承担安全性：真正的安全性来自"答案存在会话里、提交时比对"，
// 种子只是让每次画出来的干扰纹理不同。
func newVCodeSeed() string {
	return string(captcha.RandomDigits(8))
}
