// problem-ajax.php —— 题目命题人异步加载。
//
// 题目详情页用 $("#creator").load("problem-ajax.php?pid=N") 显示命题人：
// privilege 表里 rightstr='p<N>' 的第一个用户；没有则显示"外部导入"。
package web

import (
	"database/sql"
	"errors"
	"html"
	"io"
	"net/http"
	"strconv"
)

// msgImported 对应 $MSG_IMPORTED（lang/cn.php:269）。
const msgImported = "外部导入"

func (s *Server) handleProblemAjax(w http.ResponseWriter, r *http.Request) {
	pid := intval(r.URL.Query().Get("pid"))
	user, err := s.queries().GetProblemCreator(r.Context(), "p"+strconv.Itoa(pid))
	if errors.Is(err, sql.ErrNoRows) {
		writeHTML(w, msgImported)
		return
	}
	if err != nil {
		s.log.Error("查询命题人失败", "err", err, "pid", pid)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	// PHP 用 htmlentities() 转义用户名；Go 侧同样转义，避免昵称里的尖括号
	// 被 .load() 当成 HTML 注入（见设计文档 3.7）。
	esc := html.EscapeString(user)
	writeHTML(w, "<a href='userinfo.php?user="+esc+"'>"+esc+"</a>")
}

// writeHTML 输出一段 AJAX 片段。PHP 未显式设置 Content-Type，
// 沿用默认的 text/html，这里保持一致。
func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, body)
}

func (s *Server) registerAjaxRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /problem-ajax.php", s.handleProblemAjax)
	mux.HandleFunc("GET /status-ajax.php", s.handleStatusAjax)
}
