// 排行榜导出（admin/ranklist_export.php）。
//
// 仅 administrator。输出是带 UTF-8 BOM 的 CSV（非 XLSX）。
// NOIP 拦截：只要有进行中的 NOIP 比赛就整页报错（PHP 对管理员也不放行，照搬）。
// scope 的周起点照抄 PHP 的 `date("d")-(date("w")+7)%8+1` 公式（含其可能的偏差）。

package web

import (
	"encoding/csv"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

var xssPrefixRe = regexp.MustCompile(`^[=+\-@]`)

// exportScopeDate 计算 scope 的时间窗起点（PHP date('Y-m-d H:i') 等价的字符串）。
func exportScopeDate(scope string, now time.Time) string {
	switch scope {
	case "d":
		return now.Format("2006-01-02")
	case "w":
		mday := now.Day() - (int(now.Weekday())+7)%8 + 1
		monday := time.Date(now.Year(), now.Month(), mday, 0, 0, 0, 0, now.Location())
		return monday.Format("2006-01-02 15:04")
	case "m":
		return now.Format("2006-01") + "-01"
	default:
		return now.Format("2006") + "-01-01"
	}
}

func (s *Server) handleAdminRanklistExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	now := time.Now()
	if s.cfg.NoIPKeyword != "" {
		n, err := s.queries().CountRunningNOIPContests(ctx, now.Format("2006-01-02 15:04"), s.cfg.NoIPKeyword)
		if err == nil && n > 0 {
			renderPage(w, http.StatusOK, "errorPage", "NOIP", map[string]string{
				"Message": "NOIP 比赛进行中，暂不可导出排行榜。",
			})
			return
		}
	}

	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != "d" && scope != "w" && scope != "m" {
		scope = "y"
	}

	prefix := r.URL.Query().Get("prefix")
	_, prefixSet := r.URL.Query()["prefix"]
	if prefixSet && !isValidUserName(prefix) {
		renderPage(w, http.StatusOK, "errorPage", "非法前缀", map[string]string{
			"Message": "invalid user name prefix",
		})
		return
	}

	rank := parseInt(r.URL.Query().Get("start"))
	if rank < 0 {
		rank = 0
	}
	const pageSize = 5000

	var (
		rows []dbgen.RanklistExportRow
		err  error
	)
	if scope != "" {
		rows, err = s.queries().ExportRanklistScoped(ctx, exportScopeDate(scope, now), int32(rank), pageSize)
	} else {
		rows, err = s.queries().ExportRanklistUsers(ctx, dbgen.RanklistExportFilter{
			PrefixSet: prefixSet,
			Prefix:    prefix,
			Hidden:    s.cfg.RankHidden,
		}, int32(rank), pageSize)
	}
	if err != nil {
		s.log.Error("导出排行榜失败", "err", err)
		http.Error(w, "导出失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "filename=Number_"+strconv.FormatInt(now.Unix(), 10)+".csv")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"序号", "用户", "昵称", "解决", "提交", "比例"})
	rk := rank
	for _, row := range rows {
		rk++
		nick := row.Nick
		if xssPrefixRe.MatchString(nick) {
			nick = "'" + nick
		}
		nick = html.EscapeString(nick)
		solved, submit := int(row.Solved.Int32), int(row.Submit.Int32)
		ratio := "0.00%"
		if submit != 0 {
			ratio = fmt.Sprintf("%.02f%%", 100*float64(solved)/float64(submit))
		}
		_ = cw.Write([]string{
			strconv.Itoa(rk), row.UserID, nick,
			strconv.Itoa(solved), strconv.Itoa(submit), ratio,
		})
	}
	cw.Flush()
}

func (s *Server) registerAdminRanklistExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/ranklist_export.php", s.handleAdminRanklistExport)
}
