// 帮助/常见问答页（/faqs.php）。
//
// 对应 PHP faqs.php：先在 news 表里找标题为 "faqs.<lang>" 的公告，
//   - 命中：整页只渲染该公告（template/syzoj/faqs.news.php 的形态）；
//   - 未命中：渲染静态帮助（template/syzoj/faqs.php），其中"评测"一节
//     替换为已支持语言的版本号与编译/运行命令。
//
// 语言信息不在请求期重新探测（那需要挂载语言 rootfs 并跑 ver），而是由
// `hustoj-go langinfo` 预生成到 <ojhome>/etc/faq-langs.html，这里只读取。
// 文件缺失时页面退化为静态帮助，不报错。
package web

import (
	"database/sql"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
)

// FaqPageVM 是帮助页视图模型。
type FaqPageVM struct {
	// NewsContent 非空表示 faqs.<lang> 公告命中，整页只渲染公告正文。
	NewsContent template.HTML
	// LangInfo 是预生成的语言信息片段（版本/编译/运行），
	// 仅在无公告时随静态帮助一起渲染；文件缺失时为空。
	LangInfo template.HTML
	// Admin 是管理员邮箱（$OJ_ADMIN），用于"请向管理员……反馈"。
	Admin string
}

// faqLangInfoFile 是 langinfo 子命令的默认输出路径，也是本页读取的位置。
func (s *Server) faqLangInfoFile() string {
	return filepath.Join(s.cfg.OJHome, "etc", "faq-langs.html")
}

// faqLangInfo 读取预生成的语言信息片段。文件不存在（未跑过 langinfo）
// 时返回空串，页面退化为静态帮助，不报错。
func (s *Server) faqLangInfo() template.HTML {
	path := s.faqLangInfoFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Error("读取 FAQ 语言信息失败", "path", path, "err", err)
		}
		return ""
	}
	return template.HTML(data)
}

// handleFaq 处理帮助页请求。
func (s *Server) handleFaq(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := s.queries().GetNewsByTitle(ctx, "faqs."+s.cfg.Lang)
	if err != nil && err != sql.ErrNoRows {
		s.log.Error("查询 FAQ 公告失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	if len(rows) > 0 {
		s.renderPage(w, r, http.StatusOK, "faqPage", "帮助", FaqPageVM{
			NewsContent: markdownHTML(rows[0].Content),
		})
		return
	}

	s.renderPage(w, r, http.StatusOK, "faqPage", "帮助", FaqPageVM{
		LangInfo: s.faqLangInfo(),
		Admin:    s.cfg.Admin,
	})
}
