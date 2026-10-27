// status-ajax.php —— 状态页与提交页的判题结果轮询 / 详情加载。
//
// 三种消费方：
//   - auto_refresh.js：轮询 ?solution_id=N，返回 "result,memory KB,time ms,
//     judger,pass_rate×100,user_id"；
//   - submitpage.php：?tr=1&solution_id=N 拉取本人提交的编译/运行错误；
//   - 双击状态行：?q=user_id&solution_id=N 取 "user_id[nick]"。
package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// msgWarningAccessDenied 对应 $MSG_WARNING_ACCESS_DENIED（lang/cn.php:333）。
// PHP 双引号字符串里的 \$OJ_SHOW_DIFF 会还原成字面量 $OJ_SHOW_DIFF。
const msgWarningAccessDenied = "抱歉，您无法查看此消息！因为它不属于您，或者管理员设定系统状态为不显示此类信息。<br>如果你是管理员，请给自己增加source_browser权限，然后重新登录。<br>如果希望学生能看出错对比，编辑db_info.inc.php,设置 $OJ_SHOW_DIFF=true;<br> 更多细节查看hustoj.com。"

func (s *Server) handleStatusAjax(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.queries()
	query := r.URL.Query()
	solutionID := intval(query.Get("solution_id"))

	// status-ajax.php:24-25 SELECT * FROM solution WHERE solution_id=? LIMIT 1
	sol, err := q.GetSolutionStatusByID(ctx, uint32(solutionID))
	if errors.Is(err, sql.ErrNoRows) {
		// status-ajax.php:121-123：无此提交时原样回显 id 再补一段占位。
		writeHTML(w, strconv.Itoa(solutionID)+"0, 0, 0,unknown,0")
		return
	}
	if err != nil {
		s.log.Error("查询提交失败", "err", err, "solution_id", solutionID)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	idn := s.resolveIdentity(w, r)
	contestID := int32(0)
	if sol.ContestID.Valid {
		contestID = sol.ContestID.Int32
	}

	// status-ajax.php:28-29：远程评测等待中触发一次判题。
	// Go 侧判题机（judged-go）走数据库轮询，没有 trigger_judge 这个 UDP 动作，
	// 且线上 $OJ_REMOTE_JUDGE=false，故仅保留开关、不实现副作用。

	// status-ajax.php:30-57：本人查看判题详情（tr=1）。
	_, hasTR := query["tr"]
	if hasTR &&
		(sol.ProblemID == 0 || (sol.ProblemID > 0 && s.cfg.ShowDiff && !s.contestLocked16(ctx, contestID))) &&
		idn.LoggedIn && idn.UserID == sol.UserID {
		s.statusAjaxDetail(w, ctx, sol)
		return
	}

	// status-ajax.php:59-61：双击状态行显示被测者/抄袭对象的账号。
	if query.Get("q") == "user_id" {
		// PHP 原样输出 user_id[nick]；这里转义以防止 .load() 把昵称里的
		// 尖括号当 HTML 注入（上游存储型 XSS，见设计文档 3.7）。
		writeHTML(w, html.EscapeString(sol.UserID)+"["+html.EscapeString(sol.Nick)+"]")
		return
	}

	// status-ajax.php:62-92：公开状态、本人、source_browser 三者之一可见结果。
	if s.cfg.PublicStatus || (idn.LoggedIn && idn.UserID == sol.UserID) || idn.SourceBrowser {
		if contestID > 0 && sol.ProblemID > 0 {
			ct, err := q.GetContestTitleType(ctx, contestID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				s.log.Error("查询比赛信息失败", "err", err, "contest_id", contestID)
				http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
				return
			}
			if err == nil && s.noipMaskedContest(ct) {
				writeHTML(w, s.cfg.NoIPKeyword)
				return
			}
		}
		if sol.Result == 4 {
			sol.PassRate = 1 // 避免学生追问 AC 为何不是 100（status-ajax.php:72）
		}
		if query.Get("t") == "json" {
			// PHP json_encode($row)：类型细节（布尔/浮点/时间）与 Go 的
			// encoding/json 不完全一致；该分支没有任何页面调用，见文件头。
			out, err := json.Marshal(sol)
			if err != nil {
				http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
				return
			}
			writeHTML(w, string(out))
			return
		}
		judger := "none"
		if idn.Admin {
			judger = sol.Judger
		}
		writeHTML(w, strconv.Itoa(int(sol.Result))+","+
			strconv.Itoa(int(sol.Memory))+" KB,"+
			strconv.Itoa(int(sol.Time))+" ms,"+
			judger+","+
			phpFloat(sol.PassRate*100)+","+
			sol.UserID)
		return
	}

	// status-ajax.php:90：其余人看到 NOIP 提示（$contest_locks[4]）。
	writeHTML(w, s.noipWarning())
}

// statusAjaxDetail 复刻 status-ajax.php:31-57 的编译/运行错误分支。
func (s *Server) statusAjaxDetail(w http.ResponseWriter, ctx context.Context, sol dbgen.Solution) {
	q := s.queries()

	var (
		errText  string
		rowFound bool
	)
	if sol.Result == 11 {
		row, err := q.GetCompileError(ctx, int32(sol.SolutionID))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		rowFound = err == nil
		errText = row.String
	} else {
		// 填空/判断题（spj==2）在隐藏答案时一律拒绝，防止泄露正确答案。
		spj, err := q.GetProblemSPJ(ctx, sol.ProblemID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		if err == nil && spj == "2" && s.cfg.HideRightAnswer {
			writeHTML(w, msgWarningAccessDenied)
			return
		}
		row, err := q.GetRuntimeError(ctx, int32(sol.SolutionID))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		rowFound = err == nil
		errText = row.String
	}

	// 没有详情行时 PHP 什么都不输出、也不清理 custominput。
	if !rowFound {
		return
	}
	switch {
	case strings.Contains(errText, "judge"):
		writeHTML(w, "error1")
	case strings.Contains(errText, "php"):
		writeHTML(w, "error2")
	case strings.Contains(errText, "PASS"):
		writeHTML(w, "error3")
	default:
		writeHTML(w, html.EscapeString(strings.ReplaceAll(errText, "\n\r", "\n")))
	}
	if err := q.DeleteCustomInput(ctx, int32(sol.SolutionID)); err != nil {
		s.log.Error("清理自定义输入失败", "err", err, "solution_id", sol.SolutionID)
	}
}

// noipMaskedContest 判断比赛是否触发 NOIP 屏蔽：
// 标题含关键字（不区分大小写）或 contest_type 含 16 位（status-ajax.php:67）。
func (s *Server) noipMaskedContest(ct dbgen.GetContestTitleTypeRow) bool {
	title := ""
	if ct.Title.Valid {
		title = ct.Title.String
	}
	if s.cfg.NoIPKeyword != "" &&
		strings.Contains(strings.ToLower(title), strings.ToLower(s.cfg.NoIPKeyword)) {
		return true
	}
	return ct.ContestType.Valid && (ct.ContestType.Int16&16) > 0
}

// contestLocked16 是 contest_locked($cid, 16)（my_func.inc.php:210）。
func (s *Server) contestLocked16(ctx context.Context, contestID int32) bool {
	locked, err := s.queries().IsContestLocked(ctx, dbgen.IsContestLockedParams{
		ContestID: contestID,
		Level:     sql.NullInt16{Int16: 16, Valid: true},
		Keyword:   LikeArg("%" + s.cfg.NoIPKeyword + "%"),
		Now:       nowArg(time.Now()),
	})
	if err != nil {
		// PHP 查询出错返回 false（contest_locked 只在有结果时为真），
		// 这里按"未锁"处理，避免详情接口因数据库抖动整体 500。
		s.log.Error("查询比赛锁榜失败", "err", err, "contest_id", contestID)
		return false
	}
	return locked
}
