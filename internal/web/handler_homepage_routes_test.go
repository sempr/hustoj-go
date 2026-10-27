package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// TestHomepageRoutesAreMounted 通过真实 mux 验证首页三条路由确实挂上了。
//
// 背景：handleIndex/handlePortal/handleViewNews 早已实现且有 handler 级单测，
// 但一直没有 registerHomepageRoutes —— 那些测试直接调方法，绕过了 mux，
// 所以"实现了却没接线"既没被单测发现也没被对拍发现（首页还落在根占位分支）。
// 这里从 s.handler() 走一遍真实路由表，钉住这个回归。
func TestHomepageRoutesAreMounted(t *testing.T) {
	f := &fakeQuerier{
		newsList: []dbgen.GetNewsListRow{
			{NewsID: 1, Title: "Mounted News"},
		},
		newsByID: []dbgen.GetNewsByIDRow{
			{NewsID: 1, Title: "Mounted News", Content: "Mounted Content", UserID: "admin"},
		},
		monthStars: []dbgen.GetMonthStarRankRow{
			{UserID: "alice", Nick: "Alice", Ac: 5},
		},
		recentProblems: []dbgen.GetRecentProblemsRow{
			{ProblemID: 1001, Title: "Mounted Problem"},
		},
		recentContests: []dbgen.GetRecentContestsRow{
			{ContestID: 1, Title: sqlNullString("Mounted Contest")},
		},
		chartDataAll: []dbgen.GetChartDataAllRow{},
		chartDataAC:  []dbgen.GetChartDataACRow{},
	}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
	})
	h := srv.handler()

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"根路径走首页", "/", "Mounted News"},
		{"index.php", "/index.php", "Mounted News"},
		{"portal.php", "/portal.php", "请先登录"},
		{"viewnews.php", "/viewnews.php?id=1", "Mounted Content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d, 期望 200", rec.Code)
			}
			body := rec.Body.String()
			if strings.Contains(body, "业务路由尚未挂载") {
				t.Fatalf("%s 仍落在根占位分支，路由未接线", tc.path)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s 响应缺少 %q", tc.path, tc.want)
			}
		})
	}
}

// sqlNullString 造一个带 Valid 标记的 sql.NullString。
func sqlNullString(v string) (n sql.NullString) {
	n.String, n.Valid = v, true
	return n
}
