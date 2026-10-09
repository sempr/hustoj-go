// 认证页面的响应工具。
//
// PHP 侧的失败路径不是返回 4xx，而是输出一段 <script> 弹窗后 history.go(-1)。
// 迁移期两端并存时如果改成标准 4xx，页面会直接显示 nginx/PHP 的默认错误页，
// 用户看到的样式与其它页面不一致，所以这里复刻脚本弹窗的行为。

package web

import (
	"net/http"
)

// alertAndBack 弹出提示并后退，对应 PHP 的 alert + history.go(-1)。
//
// 消息经模板的 {{ }} 插值，由 html/template 做 JS 上下文转义。
// 手写 alert('...') 拼接在这里是危险的：错误文案里混入 </script> 就能
// 提前闭合脚本块，后续内容变成可执行 HTML。
func (s *Server) alertAndBack(w http.ResponseWriter, msg string) {
	renderRaw(w, http.StatusOK, "alertAndBack", alertView{Message: msg})
}

// alertView 是弹窗提示模板的数据。
type alertView struct {
	Message string
}

// renderErrorPage 渲染错误页，对应 PHP 的 template/.../error.php。
func (s *Server) renderErrorPage(w http.ResponseWriter, msg string) {
	renderPage(w, http.StatusOK, "errorPage", "错误", errorView{Message: msg})
}

// formView 是登录/注册表单模板的数据。
//
// Token 是 nosurf 生成的 CSRF 值，模板直接调 csrfToken 渲染成隐藏字段，
// 免得 handler 里多一次变量搬运。nosurf 只在校验侧读它，签发无状态。
type formView struct {
	Token     *http.Request
	ShowVCode bool
}

// errorView 是错误页模板的数据。
type errorView struct {
	Message string
}

// formView 返回当前请求对应的表单视图。
func (s *Server) formView(r *http.Request) formView {
	return formView{Token: r, ShowVCode: s.cfg.VCode}
}
