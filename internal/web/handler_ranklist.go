// 全局排名页（/ranklist.php）。
//
// 对应 phpweb/ranklist.php 与 template/syzoj/ranklist.php。
//
// ── 时区 ────────────────────────────────────────────────────────────
//
// PHP 在 include/init.php:147-148 同时设了
// date_default_timezone_set("Asia/Shanghai") 与 SET time_zone='+8:00'，
// 全站时间都是同一时区。Go 侧不把它写死在代码里，而是都由部署环境决定：
//
//   - 进程 TZ（systemd 单元注入）→ time.Now() 与 parseTime 的 loc=Local；
//   - repository.DSN 的 time_zone → NOW()/CURDATE()，默认取进程本地时区。
//
// 因此本页直接用 time.Now()：它的 Location 就是 time.Local，由 TZ 决定，
// 再通过 now.Location() 一路传给日期边界计算，不出现任何具体时区。
// 单元测试则显式传入固定 Location，保证断言不依赖跑测机器的 TZ。
//
// ── 分页口径的两个"怪"处（照抄 PHP）──────────────────────────────
//
//  1. 非 scope 模式的页数用 defunct='N' 的**全部**账号数，不减
//     $OJ_RANK_HIDDEN，也不受 prefix/group 过滤影响；
//  2. scope 模式的页数用区间内的不同 user_id 数，不要求 result=4。
//
// 两者都会让分页条比实际行数"大"，是 PHP 的既有表现，不要"修正"。

package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// ranklistPageSize 对应 ranklist.php:55 的 $page_size。
const ranklistPageSize = 50

// 全局榜单用到的 $MSG_*（lang/cn.php），未见于其他文件的这里补齐。
const (
	msgRanklist          = "排名"    // $MSG_RANKLIST :8
	msgRanklistDay       = "日排行"   // $MSG_DAY :123
	msgRanklistWeek      = "周排行"   // $MSG_WEEK :124
	msgRanklistLastMonth = "上月排行"  // $MSG_LAST_MONTH :125
	msgRanklistMonth     = "本月排行"  // $MSG_MONTH :126
	msgRanklistYear      = "年排行"   // $MSG_YEAR :127
	msgUser              = "用户"    // $MSG_USER :69
	msgNick              = "昵称"    // $MSG_NICK :120
	msgGroupName         = "班级/小组" // $MSG_GROUP_NAME :135
	msgNumber            = "名次"    // $MSG_Number :119
	msgSolved            = "解决"    // $MSG_SOVLED :121
	msgRatio             = "比率"    // $MSG_RATIO :122
	msgSearch            = "查找"    // $MSG_SEARCH :97
	msgErrorInfo         = "错误信息"  // $MSG_ERROR_INFO :486
)

// ranklistRow 是榜单表格的一行。
type ranklistRow struct {
	Rank      int
	UserID    string
	Nick      string
	GroupName string
	Starred   bool
	Solved    int
	Submit    int
	Ratio     string
}

// ranklistPage 是分页条上的一个区间链接。
type ranklistPage struct {
	Start int
	Label string
}

// ranklistView 是模板数据。
type ranklistView struct {
	Scope     string
	Prefix    string
	GroupName string
	Rows      []ranklistRow
	Pages     []ranklistPage
}

// handleRanklist 对应 ranklist.php:1-140 + 模板。
func (s *Server) handleRanklist(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// PHP 的"现在"：与 MySQL 会话同一时区（由进程 TZ 决定），分钟精度
	// （ranklist.php:9）。
	now := time.Now()

	id := s.resolveIdentity(w, r)

	// ranklist.php:10-17：有任何进行中的 NOIP 比赛且访问者不是管理员时，
	// 整页替换成警告错误页（管理员不受限）。
	cnt, err := s.queries().CountRunningNOIPContests(r.Context(),
		now.Format("2006-01-02 15:04"), s.cfg.NoIPKeyword)
	if err != nil {
		s.log.Error("查询进行中 NOIP 比赛失败", "err", err)
		http.Error(w, "排名暂时不可用", http.StatusInternalServerError)
		return
	}
	if cnt > 0 && !id.Admin {
		// :14 $view_errors = "<h2> $MSG_NOIP_WARNING </h2>"，注意两侧空格。
		s.renderRanklistError(w, "<h2> "+s.noipWarning()+" </h2>")
		return
	}

	// scope 归一化（ranklist.php:22-26）：非空且不是 d/w/m 的一律当 y。
	scope := q.Get("scope")
	if scope != "" && scope != "d" && scope != "w" && scope != "m" {
		scope = "y"
	}

	// prefix 用 isset 判断：空串也算"设置"（ranklist.php:29）。
	prefix, prefixSet := "", false
	if vs, ok := q["prefix"]; ok {
		prefixSet = true
		if len(vs) > 0 {
			prefix = vs[0]
		}
	}
	// group_name 用 isset && !empty 判断（ranklist.php:36）。
	groupName := q.Get("group_name")

	// start 偏移，负值归零（ranklist.php:49-58）。
	rank := parseInt(q.Get("start"))
	if rank < 0 {
		rank = 0
	}

	var (
		total int64
		users []dbgen.RanklistUserRow
	)
	if scope == "" {
		total, err = s.queries().CountRanklistUsers(r.Context())
		if err != nil {
			s.log.Error("查询排名总数失败", "err", err)
			http.Error(w, "排名暂时不可用", http.StatusInternalServerError)
			return
		}
		users, err = s.queries().ListRanklistUsers(r.Context(), dbgen.RanklistFilter{
			PrefixSet: prefixSet,
			Prefix:    prefix,
			GroupName: groupName,
			Hidden:    s.cfg.RankHidden,
		}, int32(rank), ranklistPageSize)
		if err != nil {
			s.log.Error("查询排名列表失败", "err", err)
			http.Error(w, "排名暂时不可用", http.StatusInternalServerError)
			return
		}
	} else {
		// pre 用 isset 判断（ranklist.php:71）。
		_, preSet := q["pre"]
		sc, err := s.buildRanklistScope(r.Context(), scope, now, preSet)
		if err != nil {
			s.log.Error("查询排名区间失败", "err", err, "scope", scope)
			http.Error(w, "排名暂时不可用", http.StatusInternalServerError)
			return
		}
		total, err = s.queries().CountRanklistScopedUsers(r.Context(), sc)
		if err != nil {
			s.log.Error("查询区间排名总数失败", "err", err)
			http.Error(w, "排名暂时不可用", http.StatusInternalServerError)
			return
		}
		users, err = s.queries().ListRanklistScopedUsers(r.Context(), sc,
			s.cfg.RankHidden, int32(rank), ranklistPageSize)
		if err != nil {
			s.log.Error("查询区间排名列表失败", "err", err)
			http.Error(w, "排名暂时不可用", http.StatusInternalServerError)
			return
		}
	}

	view := ranklistView{Scope: scope, Prefix: prefix, GroupName: groupName}
	rankNo := rank
	for _, u := range users {
		rankNo++
		row := ranklistRow{
			Rank:      rankNo,
			UserID:    u.UserID,
			Nick:      u.Nick,
			GroupName: u.GroupName,
			Starred:   u.Starred > 0,
		}
		// solved/submit 可空；PHP 把 NULL 当 0（submit==0 走 0.00% 分支）。
		if u.Solved.Valid {
			row.Solved = int(u.Solved.Int32)
		}
		if u.Submit.Valid {
			row.Submit = int(u.Submit.Int32)
		}
		row.Ratio = ranklistRatio(row.Solved, row.Submit)
		view.Rows = append(view.Rows, row)
	}

	// 分页条（模板 :52-60）：i 从 0 到 view_total 步长 page_size，
	// 条件是严格小于，故正好整除时最后一页不会重复。
	for i := int64(0); i < total; i += ranklistPageSize {
		view.Pages = append(view.Pages, ranklistPage{
			Start: int(i),
			Label: fmt.Sprintf("%d-%d", i+1, i+ranklistPageSize),
		})
	}

	s.renderPage(w, r, http.StatusOK, "ranklist", msgRanklist+" - "+s.cfg.OJName, view)
}

// buildRanklistScope 组装 scope 模式的 solution_id 区间（ranklist.php:62-84）。
//
// 顺序很关键：$last_id 先用**当期**起点算，随后 pre 分支才把 $s 改写成上期起点
// 去算 $pre_id。如果先改写 $s 再算 $last_id，区间会整体前移一个周期。
func (s *Server) buildRanklistScope(ctx context.Context, scope string, now time.Time, pre bool) (dbgen.RanklistScope, error) {
	lastID, err := s.queries().LastSolutionIDBefore(ctx,
		ranklistScopeStart(scope, now).Format("2006-01-02"))
	if err != nil {
		return dbgen.RanklistScope{}, err
	}
	sc := dbgen.RanklistScope{LastID: lastID}
	if pre {
		preID, err := s.queries().LastSolutionIDBefore(ctx,
			ranklistScopePrevStart(scope, now).Format("2006-01-02"))
		if err != nil {
			return dbgen.RanklistScope{}, err
		}
		sc.Pre = true
		sc.PreID = preID
	}
	return sc, nil
}

// ranklistScopeStart 返回当期起点日期，对应 ranklist.php:63-68 的 match。
//
//	d → 今天；w → 本周一；m → 本月 1 日；其余（含 y）→ 今年 1 月 1 日。
func ranklistScopeStart(scope string, now time.Time) time.Time {
	switch scope {
	case "d":
		return dayStart(now)
	case "w":
		return dayStart(now.AddDate(0, 0, -mondayOffset(now)))
	case "m":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	default:
		return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	}
}

// ranklistScopePrevStart 返回上一周期起点，对应 ranklist.php:73-78 的 match。
//
// 注意日周期的 PHP 写法是 strtotime('last day')，实测即"昨天"。
func ranklistScopePrevStart(scope string, now time.Time) time.Time {
	switch scope {
	case "d":
		return dayStart(now.AddDate(0, 0, -1))
	case "w":
		return dayStart(now.AddDate(0, 0, -mondayOffset(now)-7))
	case "m":
		d := now.AddDate(0, -1, 0)
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, now.Location())
	default:
		return time.Date(now.Year()-1, 1, 1, 0, 0, 0, 0, now.Location())
	}
}

// mondayOffset 返回"本周一"距今天的天数（0..6），与 PHP
// strtotime('monday this week') 对齐：周一为 0，周日为 6。
func mondayOffset(t time.Time) int {
	// time.Weekday: Sunday=0 ... Saturday=6。
	return (int(t.Weekday()) + 6) % 7
}

// dayStart 把时间归到当天 00:00。
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ranklistRatio 复刻 ranklist.php:127-130 的比率格式。
func ranklistRatio(solved, submit int) string {
	if submit == 0 {
		return "0.00%"
	}
	return fmt.Sprintf("%.2f%%", 100*float64(solved)/float64(submit))
}

// renderRanklistError 渲染 NOIP 警告页（走 error.php，标题 $MSG_ERROR_INFO）。
func (s *Server) renderRanklistError(w http.ResponseWriter, message string) {
	s.renderPage(w, nil, http.StatusOK, "contestStatsError", msgErrorInfo+" - "+s.cfg.OJName,
		contestStatsErrorView{Message: message})
}

// registerRanklistRoutes 挂载全局排名路由。
func (s *Server) registerRanklistRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ranklist.php", s.handleRanklist)
}
