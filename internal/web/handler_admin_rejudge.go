// 后台重新判题（admin/rejudge.php）与解题统计（admin/solution_statistics.php）。
//
// rejudge：把 solution.result 置 1、pass_rate=0（judged-go 轮询 result<2 自动取件），
// 因此不复刻 PHP 的 Redis lpush / UDP trigger_judge。仅 administrator。
// solution_statistics：administrator 或 contest_creator。透视逻辑在 Go 侧完成。

package web

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

type adminRejudgeVM struct {
	FromResults []int32
	ToResults   []int32
	Letters     []string
	Token       *http.Request
}

// pidLetters 供比赛题目下拉使用（A..Z 起，超出部分用序号）。
func pidLetters(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, pidLetter(int32(i)))
	}
	return out
}

func (s *Server) handleAdminRejudge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		_, hasPid := r.Form["rjpid"]
		_, hasSid := r.Form["rjsid"]
		_, hasResult := r.Form["result"]
		_, hasCid := r.Form["rjcid"]
		switch {
		case hasPid:
			pid := int32(parseInt(r.FormValue("rjpid")))
			if pid == 0 {
				renderPage(w, http.StatusOK, "errorPage", "重新判题", map[string]string{
					"Message": "Rejudge Problem ID should not equal to 0",
				})
				return
			}
			_ = s.queries().RejudgeProblemSolutions(ctx, pid)
			_ = s.queries().ResetProblemAccepted(ctx, pid)
			_ = s.queries().DeleteSimByProblem(ctx, pid)
			http.Redirect(w, r, "/status.php?problem_id="+strconv.Itoa(int(pid)), http.StatusFound)
		case hasSid:
			sid := uint32(parseInt(r.FormValue("rjsid")))
			_ = s.queries().DeleteSimBySolution(ctx, int32(sid))
			_ = s.queries().RejudgeSolution(ctx, sid)
			cid, err := s.queries().GetSolutionContestID(ctx, sid)
			if err == nil && cid.Valid && cid.Int32 > 0 {
				http.Redirect(w, r, "/status.php?cid="+strconv.Itoa(int(cid.Int32))+"&top="+strconv.Itoa(int(sid+1)), http.StatusFound)
			} else {
				http.Redirect(w, r, "/status.php?top="+strconv.Itoa(int(sid+1)), http.StatusFound)
			}
		case hasResult:
			_ = s.queries().RejudgeByResult(ctx, dbgen.RejudgeByResultParams{
				ToResult:   int16(parseInt(r.FormValue("to"))),
				FromResult: int16(parseInt(r.FormValue("result"))),
			})
			http.Redirect(w, r, "/status.php?jresult="+strconv.Itoa(parseInt(r.FormValue("to"))), http.StatusFound)
		case hasCid:
			cid := int32(parseInt(r.FormValue("rjcid")))
			if cid <= 0 {
				http.Redirect(w, r, "/admin/rejudge.php", http.StatusFound)
				return
			}
			if _, hasContestPid := r.Form["pid"]; hasContestPid {
				_ = s.queries().RejudgeContestProblem(ctx, dbgen.RejudgeContestProblemParams{
					ContestID: sql.NullInt32{Int32: cid, Valid: true},
					Num:       int8(parseInt(r.FormValue("pid"))),
				})
			} else {
				_ = s.queries().RejudgeContest(ctx, sql.NullInt32{Int32: cid, Valid: true})
			}
			http.Redirect(w, r, "/status.php?cid="+strconv.Itoa(int(cid)), http.StatusFound)
		}
		return
	}

	from := make([]int32, 0, 5)
	from = append(from, 0, 1, 2, 3, 16)
	to := make([]int32, 0, 16)
	to = append(to, 1)
	for i := int32(4); i < 18; i++ {
		to = append(to, i)
	}
	renderPage(w, http.StatusOK, "adminRejudgePage", "重新判题", adminRejudgeVM{
		FromResults: from,
		ToResults:   to,
		Letters:     pidLetters(30),
		Token:       r,
	})
}

// ── admin/solution_statistics.php ────────────────────────────────────

type statCellVM struct {
	Kind string // "AC" | "WA" | "blank"
}

type statRowVM struct {
	UserID string
	Nick   string
	Cells  []statCellVM
}

type adminSolutionStatsVM struct {
	Problems []int32
	Rows     []statRowVM
	Contests []dbgen.ListRecentContests50Row
	CID      int32
	Token    *http.Request
}

func (s *Server) handleAdminSolutionStatistics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	cid := int32(parseInt(r.URL.Query().Get("cid")))
	_, hasPids := r.Form["pids"]
	if hasPids || cid > 0 {
		var pids []int32
		if cid > 0 {
			cps, err := s.queries().ListContestProblemIDs(ctx, cid)
			if err != nil {
				s.log.Error("查询比赛题目失败", "err", err)
			}
			for _, cp := range cps {
				pids = append(pids, cp.ProblemID)
			}
		} else {
			seen := map[int32]bool{}
			for _, raw := range strings.Split(r.FormValue("pids"), ",") {
				if id := int32(parseInt(raw)); id != 0 && !seen[id] {
					seen[id] = true
					pids = append(pids, id)
				}
			}
		}

		var rows []statRowVM
		if len(pids) > 0 {
			// 取回 (user,problem,min) 明细后由 Go 透视。
			minMap := map[string]map[int32]int32{}
			nicks := map[string]string{}
			var order []string
			addRow := func(uid, nick string, pid int32, result int64) {
				if _, ok := minMap[uid]; !ok {
					minMap[uid] = map[int32]int32{}
					nicks[uid] = nick
					order = append(order, uid)
				}
				minMap[uid][pid] = int32(result)
			}
			if cid > 0 {
				got, err := s.queries().SolutionMinByContest(ctx, sql.NullInt32{Int32: cid, Valid: true})
				if err != nil {
					s.log.Error("统计失败", "err", err)
				}
				for _, g := range got {
					addRow(g.UserID, g.Nick, g.ProblemID, g.R)
				}
			} else {
				var uids []string
				seen := map[string]bool{}
				for _, u := range strings.Split(strings.TrimSpace(r.FormValue("ulist")), "\n") {
					u = strings.TrimSpace(u)
					if u != "" && !seen[u] {
						seen[u] = true
						uids = append(uids, u)
					}
				}
				if len(uids) > 0 {
					got, err := s.queries().SolutionMinByUsers(ctx, uids)
					if err != nil {
						s.log.Error("统计失败", "err", err)
					}
					for _, g := range got {
						addRow(g.UserID, g.Nick, g.ProblemID, g.R)
					}
				}
			}
			sort.Strings(order)
			for _, uid := range order {
				cells := make([]statCellVM, 0, len(pids))
				for _, pid := range pids {
					v, ok := minMap[uid][pid]
					switch {
					case ok && v == 4:
						cells = append(cells, statCellVM{Kind: "AC"})
					case !ok || v == 15:
						cells = append(cells, statCellVM{Kind: "blank"})
					default:
						cells = append(cells, statCellVM{Kind: "WA"})
					}
				}
				rows = append(rows, statRowVM{UserID: uid, Nick: nicks[uid], Cells: cells})
			}
		}

		renderPage(w, http.StatusOK, "adminSolutionStatsPage", "解题统计", adminSolutionStatsVM{
			Problems: pids, Rows: rows, CID: cid, Token: r,
		})
		return
	}

	contests, err := s.queries().ListRecentContests50(ctx)
	if err != nil {
		s.log.Error("查询比赛失败", "err", err)
	}
	renderPage(w, http.StatusOK, "adminSolutionStatsPage", "解题统计", adminSolutionStatsVM{
		Contests: contests, CID: cid, Token: r,
	})
}

func (s *Server) registerAdminSystemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/rejudge.php", s.handleAdminRejudge)
	mux.HandleFunc("POST /admin/rejudge.php", s.handleAdminRejudge)
	mux.HandleFunc("GET /admin/solution_statistics.php", s.handleAdminSolutionStatistics)
	mux.HandleFunc("POST /admin/solution_statistics.php", s.handleAdminSolutionStatistics)
}
