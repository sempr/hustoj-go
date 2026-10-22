// showsource.php —— 查看单条提交的源代码。
//
// 权限模型分三层：
//  1. 锁榜/考试模式拦截（仅 source_browser / administrator 豁免）；
//  2. s<problem_id> 权限、本人提交、source_browser 三者之一才允许查看；
//  3. 源码本身取自 source_code_user。
package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const (
	msgSourceNotAllowed = "考试期间，不能查阅以前提交的代码。" // $MSG_SOURCE_NOT_ALLOWED_FOR_EXAM
	msgSourceCode       = "源代码"               // $MSG_SOURCE_CODE
)

// showsourceMeta 是源码上方表格里展示的提交信息。
//
// PHP 把这些字段以注释形式拼在源码末尾（showsource.php:28-40）；Go 拆成结构化
// 字段，由模板渲染成代码上方的表格，源码本身保持原样。
type showsourceMeta struct {
	Problem  int32
	User     string
	Nick     string
	Language string
	Result   int32
	ShowTime bool // 仅 AC（result==4）时展示耗时/内存。
	Time     int32
	Memory   int32
}

// showsourceView 是 showsource 模板的数据。
type showsourceView struct {
	OK     bool
	Lang   string // highlight.js 语言名（language-<Lang> 类），空表示不高亮。
	Source string
	Meta   showsourceMeta

	// 判题信息：把 reinfo.php（各用例结果）与 ceinfo.php（编译错误）的内容
	// 并到源码页 —— PHP 原本要另外点开这两个页面，这里合成一处。
	SID    int32
	Mark   string // 得分（pass_rate×100，AC 记 100），与 reinfo.php 一致
	Reinfo string // runtimeinfo.error：各用例的判题结果
	Ceinfo string // compileinfo.error：编译错误输出
}

func (s *Server) handleShowsource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.queries()

	// showsource.php:10-14 缺少 id 直接报错。
	if !r.URL.Query().Has("id") {
		s.renderContestStatsError(w, "No such code!")
		return
	}
	id := intval(r.URL.Query().Get("id"))

	// 提交不存在时 PHP 得到 $row=false 仍继续，这里用零值行等价处理。
	sol, err := q.GetSolutionByID(ctx, uint32(id))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.showsourceFailure(w, "查询提交失败", err)
		return
	}

	idn := s.resolveIdentity(w, r)
	now := time.Now()

	// showsource.php:30-73 锁榜拦截。source_browser / administrator 直接豁免。
	if !(idn.SourceBrowser || idn.Admin) {
		lockedCID, err := s.showsourceLockedContest(ctx, sol, now)
		if err != nil {
			s.showsourceFailure(w, "查询锁榜比赛失败", err)
			return
		}
		if lockedCID > 0 {
			s.renderContestStatsError(w, showsourceLockMessage(lockedCID))
			return
		}
	}

	// showsource.php:76-87 考试模式：比赛号小于阈值且非 source_browser 一律拦截。
	// 注意 PHP 不豁免 administrator，且 contest_id 为 null 时按 0 计。
	if s.cfg.ExamContestID > 0 && sol.ContestID.Int32 < s.cfg.ExamContestID && !idn.SourceBrowser {
		msg := "<center><h3>" + msgContestID + " : " + strconv.Itoa(int(s.cfg.ExamContestID)) + "+ </h3>" +
			"<p> " + msgSourceNotAllowed + " </p><br></center><br><br>"
		s.renderContestStatsError(w, msg)
		return
	}

	ok := false
	// showsource.php:98-111 s<problem_id> 权限（session 缓存，Go 直接查 privilege 表）。
	if idn.LoggedIn {
		has, err := q.HasContestPrivilege(ctx, dbgen.HasContestPrivilegeParams{
			UserID:   idn.UserID,
			Rightstr: "s" + strconv.Itoa(int(sol.ProblemID)),
		})
		if err != nil {
			s.showsourceFailure(w, "查询源码权限失败", err)
			return
		}
		if has {
			ok = true
		}
	}
	// showsource.php:113 本人可看；:114 source_browser 可看。
	if idn.LoggedIn && sol.UserID != "" && sol.UserID == idn.UserID {
		ok = true
	}
	if idn.SourceBrowser {
		ok = true
	}

	// showsource.php:116-120 源码，缺行时保留默认文案。
	// 注意 PHP 用的是请求参数 $id，而非 $row['solution_id']（showsource.php:116）。
	viewSource := "No source code available!"
	if src, err := q.GetSourceCodeForSolution(ctx, int32(id)); err == nil {
		viewSource = src
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.showsourceFailure(w, "查询源码失败", err)
		return
	}

	view := showsourceView{OK: ok}
	if ok {
		view.Lang = hljsLang(sourceBrush(sol.Language))
		view.Source = showsourceCode(viewSource)
		view.Meta = showsourceMeta{
			Problem:  sol.ProblemID,
			User:     sol.UserID,
			Nick:     sol.Nick,
			Language: languageName(int32(sol.Language)),
			Result:   int32(sol.Result),
			ShowTime: sol.Result == 4,
			Time:     sol.Time,
			Memory:   sol.Memory,
		}

		// 得分：reinfo.php 的 $mark（AC 记 100）。
		view.SID = int32(id)
		view.Mark = formatPassRate(sol.PassRate)
		if sol.Result == 4 {
			view.Mark = "100"
		}

		// reinfo.php 的内容：各用例的判题结果（runtimeinfo）。
		if runtimeErr, err := q.GetRuntimeError(ctx, int32(id)); err == nil && runtimeErr.Valid {
			view.Reinfo = runtimeErr.String
		}
		// ceinfo.php 的内容：编译错误（compileinfo）。
		if compileErr, err := q.GetCompileError(ctx, int32(id)); err == nil && compileErr.Valid {
			view.Ceinfo = compileErr.String
		}
	}
	s.renderPage(w, r, http.StatusOK, "showsourcePage",
		strconv.Itoa(id)+" - "+msgSourceCode+" - "+s.cfg.OJName, view)
}

// showsourceLockedContest 对应 showsource.php:29-73 + my_func.inc.php:216-223。
//
// 返回被锁榜的比赛号；0 表示未锁。规则：仅当提交所属比赛尚未结束（或非比赛
// 提交、或比赛查不到）时才检查题目是否被另一场进行中的比赛占用。
func (s *Server) showsourceLockedContest(ctx context.Context, sol dbgen.GetSolutionByIDRow, now time.Time) (int32, error) {
	needCheck := true
	if sol.ContestID.Valid && sol.ContestID.Int32 > 0 {
		win, err := s.queries().GetContestWindow(ctx, sol.ContestID.Int32)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// 比赛查不到：needCheck 保持 true。
		case err != nil:
			return 0, err
		default:
			// 已结束（含 end_time 为空）才需要检查是否被进行中比赛占用。
			needCheck = !win.EndTime.Valid || win.EndTime.Time.Before(now)
		}
	}
	if !needCheck {
		return 0, nil
	}
	cid, err := s.queries().ProblemLockedContestID(ctx, dbgen.ProblemLockedContestIDParams{
		ProblemID: sol.ProblemID,
		Level:     sql.NullInt16{Int16: 1, Valid: true},
		Keyword:   LikeArg("%" + s.cfg.NoIPKeyword + "%"),
		Now:       nowArg(now),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if cid <= 0 {
		return 0, nil
	}
	return cid, nil
}

// showsourceLockMessage 拼锁榜错误文案（showsource.php:62-67）。
//
// 上游把锁榜比赛的 id 写成了 $result[0][0]（实际是 start_time 或 solution_id），
// 属明显 bug（见设计文档 3.7）；这里展示真正的比赛号。
func showsourceLockMessage(cid int32) string {
	return "<center><h3>" + msgContestID + " : " + strconv.Itoa(int(cid)) + "</h3>" +
		"<p> " + msgSourceNotAllowed + " </p><br></center><br><br>"
}

// showsourceCode 规范化源码的行尾（PHP 的 str_replace("\n\r","\n")）。
//
// 提交信息不再拼成注释贴在代码末尾，改由模板在代码上方以表格展示。
// 内容交给 html/template 转义，因此这里返回未转义原文。
func showsourceCode(viewSource string) string {
	return strings.ReplaceAll(viewSource, "\n\r", "\n")
}

// sourceBrush 复刻 showsource 模板对 SyntaxHighlighter brush 的改写。
//
// brush 名来自注册表的 brush 字段（缺省取展示名小写），不再在代码里维护
// 别名表。越界语言返回空串，与 PHP 取到 undefined index 的行为一致。
func sourceBrush(lang uint32) string {
	brush, ok := languageBrushAt(int32(lang))
	if !ok {
		return ""
	}
	return brush
}

// hljsLang 把注册表里的 SyntaxHighlighter brush 名翻译成 highlight.js 的语言名。
//
// 两套命名体系不同：SH 用 c++/c#/obj-c/vb，hljs 用 cpp/csharp/objectivec/vbnet；
// Pascal 两边都叫 delphi。这里只做展示层别名翻译，认不出时原样返回，最终由前端
// 用 hljs.getLanguage 过滤掉当前 bundle 未收录的语言（退化成纯文本）。
func hljsLang(brush string) string {
	switch strings.ToLower(strings.TrimSpace(brush)) {
	case "":
		return ""
	case "c++", "cpp":
		return "cpp"
	case "c#", "csharp":
		return "csharp"
	case "obj-c", "objc", "objective-c":
		return "objectivec"
	case "vb", "vbnet":
		return "vbnet"
	case "pascal", "delphi":
		return "delphi"
	default:
		return strings.ToLower(strings.TrimSpace(brush))
	}
}

// showsourceFailure 记录错误并返回 500。
func (s *Server) showsourceFailure(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
}

func (s *Server) registerShowsourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /showsource.php", s.handleShowsource)
}
