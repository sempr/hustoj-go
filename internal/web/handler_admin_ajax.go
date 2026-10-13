// admin/ajax.php：表格内联编辑的公共端点（news_list / user_list / problem_list）。
//
// 权限：PHP 顶部只放行 administrator / problem_editor / contest_creator / tag_adder，
// 具体每个操作再叠加表级角色。Identity 目前不建模 tag_adder（见 session_redis.go），
// 故 gate 为 administrator||problem_editor||contest_creator；纯 tag_adder 会被拒（已知限制）。
//
// CSRF：nosurf 全局中间件（POST 必带 token）。PHP 用非消费式 token，Go 的 nosurf 是
// 无状态双提交 Cookie，同样支持单页连续多次编辑。
//
// 返回：PHP 用 pdo_query 回显 affected rows，内联 JS 并不读取，这里写入成功即 200。
// 只读的 problem_get_title / get_user_list_of_contest 回显文本。

package web

import (
	"database/sql"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// uniqueSource 复刻 ajax.php 的 uniqueSource()：按空白切分、去重（保留首次出现）、
// 单空格拼接。
func uniqueSource(s string) string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, f := range strings.Fields(s) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}

// ajaxUint32 把表单字符串转成无符号整数；PHP 端让 MySQL 强转，非法即 0。
func ajaxUint32(s string) uint32 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}

// ajaxFloat 把表单字符串转成浮点（time_limit 是 decimal）。
func ajaxFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

// ajaxDate 解析日期表单值；支持 date 与 datetime 两种写法。
func ajaxDate(s string) (t time.Time, ok bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (s *Server) handleAdminAjax(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ProblemEditor || identity.ContestCreator) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	m := r.FormValue("m")
	q := s.queries()

	switch m {
	case "problem_set_source":
		if !(identity.Admin || identity.ProblemEditor) {
			return
		}
		_ = q.SetProblemSource(ctx, dbgen.SetProblemSourceParams{
			Source:    sql.NullString{String: uniqueSource(r.FormValue("ns")), Valid: true},
			ProblemID: int32(parseInt(r.FormValue("pid"))),
		})

	case "problem_add_source":
		if !(identity.Admin || identity.ProblemEditor) {
			return
		}
		pid := int32(parseInt(r.FormValue("pid")))
		old, err := q.GetProblemSource(ctx, pid)
		if err == nil {
			combined := uniqueSource(r.FormValue("ns") + " " + old.String)
			_ = q.SetProblemSource(ctx, dbgen.SetProblemSourceParams{
				Source:    sql.NullString{String: combined, Valid: true},
				ProblemID: pid,
			})
		}

	case "problem_update_time":
		if !(identity.Admin || identity.ProblemEditor) {
			return
		}
		_ = q.SetProblemTimeLimit(ctx, dbgen.SetProblemTimeLimitParams{
			TimeLimit: float64(parseInt(r.FormValue("t"))),
			ProblemID: int32(parseInt(r.FormValue("pid"))),
		})

	case "problem_get_title":
		if !(identity.Admin || identity.ProblemEditor) {
			return
		}
		row, err := q.GetProblemTitleSource(ctx, int32(parseInt(r.FormValue("pid"))))
		if err != nil {
			return
		}
		// PHP 未转义，这里补上转义（迁移期有意修掉的上游 XSS）。
		_, _ = w.Write([]byte(html.EscapeString(row.Title) +
			"&nbsp;&nbsp;<span class='label label-success'>" + html.EscapeString(row.Source.String) + "</span>"))

	case "user_update_nick":
		if !identity.Admin {
			return
		}
		uid := r.FormValue("user_id")
		nick := r.FormValue("nick")
		_ = q.SetUserNick(ctx, dbgen.SetUserNickParams{Nick: nick, UserID: uid})
		_ = q.UpdateUserSolutionNick(ctx, dbgen.UpdateUserSolutionNickParams{Nick: nick, UserID: uid})

	case "user_update_expiry_date":
		if !identity.Admin {
			return
		}
		t, ok := ajaxDate(r.FormValue("expiry_date"))
		if !ok {
			return
		}
		_ = q.SetUserExpiryDate(ctx, dbgen.SetUserExpiryDateParams{ExpiryDate: t, UserID: r.FormValue("user_id")})

	case "user_update_school":
		if !identity.Admin {
			return
		}
		_ = q.SetUserSchool(ctx, dbgen.SetUserSchoolParams{School: r.FormValue("school"), UserID: r.FormValue("user_id")})

	case "user_update_group_name":
		if !identity.Admin {
			return
		}
		_ = q.SetUserGroupName(ctx, dbgen.SetUserGroupNameParams{GroupName: r.FormValue("group_name"), UserID: r.FormValue("user_id")})

	case "user_update_parent_phone":
		if !identity.Admin {
			return
		}
		phone := strings.TrimSpace(r.FormValue("parent_phone"))
		// 服务端兜底校验，与 ajax.php 的注释块一致。
		if phone != "" && !validParentPhone(phone) {
			_, _ = w.Write([]byte("ERR: invalid phone"))
			return
		}
		_ = q.SetUserParentPhone(ctx, dbgen.SetUserParentPhoneParams{ParentPhone: phone, UserID: r.FormValue("user_id")})

	case "user_update_coin_bonus":
		if !identity.Admin {
			return
		}
		_ = q.SetUserCoinBonus(ctx, dbgen.SetUserCoinBonusParams{CoinBonus: ajaxUint32(r.FormValue("coin_bonus")), UserID: r.FormValue("user_id")})

	case "user_update_coin_spent":
		if !identity.Admin {
			return
		}
		_ = q.SetUserCoinSpent(ctx, dbgen.SetUserCoinSpentParams{CoinSpent: ajaxUint32(r.FormValue("coin_spent")), UserID: r.FormValue("user_id")})

	case "problem_update_coin":
		if !(identity.Admin || identity.ProblemEditor) {
			return
		}
		_ = q.SetProblemCoin(ctx, dbgen.SetProblemCoinParams{Coin: ajaxUint32(r.FormValue("coin")), ProblemID: int32(parseInt(r.FormValue("problem_id")))})

	case "problem_update_time_limit":
		if !identity.Admin {
			return
		}
		_ = q.SetProblemTimeLimit(ctx, dbgen.SetProblemTimeLimitParams{TimeLimit: ajaxFloat(r.FormValue("time_limit")), ProblemID: int32(parseInt(r.FormValue("problem_id")))})

	case "problem_update_memory_limit":
		if !identity.Admin {
			return
		}
		_ = q.SetProblemMemoryLimit(ctx, dbgen.SetProblemMemoryLimitParams{MemoryLimit: int32(parseInt(r.FormValue("memory_limit"))), ProblemID: int32(parseInt(r.FormValue("problem_id")))})

	case "news_update_importance":
		if !identity.Admin {
			return
		}
		_ = q.SetNewsImportance(ctx, dbgen.SetNewsImportanceParams{Importance: int8(parseInt(r.FormValue("importance"))), NewsID: int32(parseInt(r.FormValue("news_id")))})

	case "get_user_list_of_contest":
		if !(identity.Admin || identity.ContestCreator) {
			return
		}
		users, err := q.ListContestPrivilegeUsers(ctx, "c"+strconv.Itoa(parseInt(r.FormValue("contest_id"))))
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				s.log.Error("查询比赛用户失败", "err", err)
			}
			return
		}
		for _, u := range users {
			_, _ = w.Write([]byte(u + "\r\n"))
		}
	}
}

// validParentPhone 对应 ajax.php 里 /^1[3-9][0-9]{9}$/。
func validParentPhone(s string) bool {
	if len(s) != 11 || s[0] != '1' || s[1] < '3' || s[1] > '9' {
		return false
	}
	for i := 2; i < 11; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
