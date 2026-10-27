// problemstatus.php —— 单题提交状态页。
//
// 展示某题的提交总数、参与人数、AC 人数、各结果分布，以及每个用户
// 最优 AC 提交的榜单。与 userinfo 一样，GET 打开页面会回写
// problem.submit / problem.accepted（PHP 亦然，见 problemstatus.php:57、:92）。
package web

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// problemStatusPageSize 是榜单每页行数（problemstatus.php:114）。
const problemStatusPageSize = 20

// problemStatusStat 是统计卡片的一行（提交记录 / 用户(提交) / 用户(解决)）。
type problemStatusStat struct {
	Label string
	Value int64
}

// problemStatusPie 是饼图的一个扇区（view_problem_title / number）。
type problemStatusPie struct {
	Label string
	Count int64
}

// problemStatusSolution 是榜单的一行（$view_solution）。
type problemStatusSolution struct {
	Rank        int
	SolutionTag string // solution_id，或 "solution_id(att)"
	UserID      string
	Memory      string
	Time        string
	Language    string
	LangHref    template.URL // 非空时语言渲染成 showsource.php 链接
	CodeLength  string
	InDate      string
}

// problemStatusView 是 problemstatus 模板的数据。
type problemStatusView struct {
	ProblemID  int
	PrevHref   template.URL
	NextHref   template.URL
	Stats      []problemStatusStat
	Pie        []problemStatusPie
	Solutions  []problemStatusSolution
	Recommends []int
}

func (s *Server) handleProblemStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.queries()

	id := intval(r.URL.Query().Get("id"))
	page := intval(r.URL.Query().Get("page"))

	// problemstatus.php:34-44。$now 是进程时区的 date('Y-m-d H:i')，
	// 分钟精度；截到分钟才能与 PHP 的字符串比较完全一致。
	now := time.Now().Truncate(time.Minute)
	if s.cfg.NoIPKeyword != "" {
		n, err := q.CountRunningNOIPProblemContests(ctx, dbgen.CountRunningNOIPProblemContestsParams{
			Now:     sql.NullTime{Time: now, Valid: true},
			Keyword: sql.NullString{String: "%" + s.cfg.NoIPKeyword + "%", Valid: true},
		})
		if err != nil {
			s.problemStatusFailure(w, "查询 NOIP 比赛失败", err)
			return
		}
		if n > 0 {
			s.renderContestStatsError(w, "<h2>"+s.noipWarning()+"</h2>")
			return
		}
	}

	// :52-58 total submit，并回写 problem.submit。
	total, err := q.CountSubmitsForProblem(ctx, int32(id))
	if err != nil {
		s.problemStatusFailure(w, "查询提交总数失败", err)
		return
	}
	if err := q.SetProblemSubmit(ctx, dbgen.SetProblemSubmitParams{
		Submit:    sql.NullInt32{Int32: int32(total), Valid: true},
		ProblemID: int32(id),
	}); err != nil {
		s.problemStatusFailure(w, "更新提交总数失败", err)
		return
	}

	// :63-75 参与人数与 AC 人数。
	users, err := q.CountProblemUsers(ctx, int32(id))
	if err != nil {
		s.problemStatusFailure(w, "查询参与人数失败", err)
		return
	}
	acUsers, err := q.CountProblemACUsers(ctx, int32(id))
	if err != nil {
		s.problemStatusFailure(w, "查询 AC 人数失败", err)
		return
	}

	stats := []problemStatusStat{
		{Label: "提交记录", Value: total}, // $MSG_SUBMIT_RECORD，lang/cn.php:478
		{Label: msgUser + "(" + msgSubmit + ")", Value: users},
		{Label: msgUser + "(" + msgSolved + ")", Value: acUsers},
	}

	// :82-96 各结果分布；result=4 时回写 problem.accepted。
	resStats, err := q.GetProblemResultStats(ctx, int32(id))
	if err != nil {
		s.problemStatusFailure(w, "查询结果分布失败", err)
		return
	}
	var pie []problemStatusPie
	for _, rs := range resStats {
		label := judgeResultLabel(int32(rs.Result))
		pie = append(pie, problemStatusPie{Label: label, Count: rs.Cnt})
		if rs.Result == 4 {
			if err := q.SetProblemAccepted(ctx, dbgen.SetProblemAcceptedParams{
				Accepted:  sql.NullInt32{Int32: int32(rs.Cnt), Valid: true},
				ProblemID: int32(id),
			}); err != nil {
				s.problemStatusFailure(w, "更新 AC 数失败", err)
				return
			}
		}
	}

	// :104-117 分页范围。PHP 用 intval((acuser-1)/20)，Go 整数除法同样向零取整。
	pageMin := 0
	pageMax := int((acUsers - 1) / problemStatusPageSize)
	if page < pageMin {
		page = pageMin
	}
	if page > pageMax {
		page = pageMax
	}
	start := page * problemStatusPageSize
	size := int64(problemStatusPageSize)
	if int64(start)+size > acUsers {
		size = acUsers - int64(start)
	}
	if size < 0 {
		size = 0
	}

	// :122-124 flag：题目是否被进行中的比赛占用。
	inRunning, err := q.ProblemInRunningContest(ctx, dbgen.ProblemInRunningContestParams{
		ProblemID: int32(id),
		Now:       sql.NullTime{Time: now, Valid: true},
	})
	if err != nil {
		s.problemStatusFailure(w, "查询比赛占用失败", err)
		return
	}
	flag := !inRunning

	idn := s.resolveIdentity(w, r)

	// :158-229 榜单。PHP 用 LIMIT $start, $sz，offset 在前。
	sols, err := q.ListProblemBestACSolutions(ctx, int32(id), int32(start), int32(size))
	if err != nil {
		s.problemStatusFailure(w, "查询榜单失败", err)
		return
	}
	view := problemStatusView{
		ProblemID: id,
		Stats:     stats,
		Pie:       pie,
	}
	if page > pageMin {
		view.PrevHref = problemStatusPageHref(id, page-1)
	}
	if page < pageMax {
		view.NextHref = problemStatusPageHref(id, page+1)
	}

	rank := start + 1
	lastUser := ""
	for _, sol := range sols {
		if sol.UserID == lastUser {
			continue
		}
		row := problemStatusSolution{
			Rank:        rank,
			SolutionTag: strconv.Itoa(int(sol.SolutionID)),
			UserID:      sol.UserID,
			InDate:      sol.InDate.Format("2006-01-02 15:04:05"),
		}
		if sol.Att > 1 {
			row.SolutionTag += "(" + strconv.FormatInt(sol.Att, 10) + ")"
		}
		if flag {
			row.Memory = fmt.Sprintf("%d KiB", sol.Memory)
			row.Time = fmt.Sprintf("%d ms", sol.Time)
			row.CodeLength = fmt.Sprintf("%d bytes", sol.CodeLength)
		} else {
			row.Memory = "------"
			row.Time = "------"
			row.CodeLength = "------"
		}

		// :212-217 只有本人（大小写不敏感）或 source_browser 能点开源码。
		// $OJ_AUTO_SHARE 在生产为 false，其分支（含 s<id> 授权）不生效。
		row.Language = problemStatusLanguageName(sol.Language)
		if (idn.LoggedIn && strings.EqualFold(sol.UserID, idn.UserID)) || idn.SourceBrowser {
			row.LangHref = template.URL("showsource.php?id=" + strconv.Itoa(int(sol.SolutionID)))
		}

		view.Solutions = append(view.Solutions, row)
		lastUser = sol.UserID
		rank++
	}

	// :237-255 相关推荐，仅当 URL 带 id 参数时才查。
	if r.URL.Query().Has("id") {
		view.Recommends = s.problemStatusRecommends(ctx, int32(id))
	}

	s.renderPage(w, r, http.StatusOK, "problemStatus",
		msgProblems+" "+msgStatistics+" - "+s.cfg.OJName, view)
}

// problemStatusRecommends 返回按 source 匹配的相关题号（problemstatus.php:237-255）。
func (s *Server) problemStatusRecommends(ctx context.Context, id int32) []int {
	q := s.queries()
	src, err := q.GetProblemSource(ctx, id)
	if err != nil {
		// 题目不存在时 PHP 会取到 null（并留下 warning），推荐列表为空。
		return nil
	}
	pattern := "%"
	if src.Valid {
		pattern = "%" + src.String + "%"
	}
	ids, err := q.ListProblemIDsBySource(ctx, dbgen.ListProblemIDsBySourceParams{
		Source:    sql.NullString{String: pattern, Valid: true},
		ProblemID: id,
	})
	if err != nil {
		return nil
	}
	out := make([]int, 0, len(ids))
	for _, pid := range ids {
		out = append(out, int(pid))
	}
	return out
}

// problemStatusLanguageName 取 $language_name（lang/cn.php:12）里的语言名。
//
// 越界（语言 id 超出表长）时 PHP 取到 undefined index，渲染为空串。
func problemStatusLanguageName(lang uint32) string {
	names := languageNamesAll()
	if int(lang) < len(names) {
		return names[lang]
	}
	return ""
}

// problemStatusPageHref 拼分页链接。
func problemStatusPageHref(id, page int) template.URL {
	return template.URL("problemstatus.php?id=" + strconv.Itoa(id) + "&page=" + strconv.Itoa(page))
}

// problemStatusFailure 统一记录错误并返回 500。
func (s *Server) problemStatusFailure(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
}

func (s *Server) registerProblemStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /problemstatus.php", s.handleProblemStatus)
}
