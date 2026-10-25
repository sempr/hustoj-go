package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const homepageSID = "homepagetest1234567890abcd"

// doHomepage 构造一次 homepage 请求。session 非空时写 PHP 会话文件并带 PHPSESSID。
func doHomepage(t *testing.T, f *fakeQuerier, mutate func(*config.Config), session, target string,
	h func(*Server, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(dir, "sess_"+homepageSID), []byte(session), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
		if mutate != nil {
			mutate(c)
		}
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	if session != "" {
		srv.sessions = newTestSessionStore(t)
	}

	req := httptest.NewRequest(http.MethodGet, target, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: homepageSID})
	}
	rec := httptest.NewRecorder()
	h(srv, rec, req)
	return rec
}

// ── 首页（index.php）────────────────────────────────────────────────

func TestIndexPage(t *testing.T) {
	f := &fakeQuerier{
		newsList: []dbgen.GetNewsListRow{
			{NewsID: 1, Title: "Test News"},
		},
		monthStars: []dbgen.GetMonthStarRankRow{
			{UserID: "alice", Nick: "Alice", Ac: 5},
		},
		recentProblems: []dbgen.GetRecentProblemsRow{
			{ProblemID: 1001, Title: "Test Problem"},
		},
		recentContests: []dbgen.GetRecentContestsRow{
			{ContestID: 1, Title: sql.NullString{String: "Test Contest", Valid: true}},
		},
		chartDataAll: []dbgen.GetChartDataAllRow{},
		chartDataAC:  []dbgen.GetChartDataACRow{},
	}
	rec := doHomepage(t, f, nil, "", "/index.php", (*Server).handleIndex)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Test News") {
		t.Errorf("body should contain news title")
	}
	if !strings.Contains(body, "alice") {
		t.Errorf("body should contain month star user")
	}
	if !strings.Contains(body, "Test Problem") {
		t.Errorf("body should contain recent problem")
	}
	if !strings.Contains(body, "Test Contest") {
		t.Errorf("body should contain recent contest")
	}
}

// 全新库还没有任何提交时，本月之星边界查询返回 sql.ErrNoRows。
// PHP 把空结果当 0 处理，首页必须照常渲染，不能变成 500。
func TestIndexPageWithoutSolutions(t *testing.T) {
	f := &fakeQuerier{
		monthStarBoundaryErr: sql.ErrNoRows,
		newsList:             []dbgen.GetNewsListRow{},
		chartDataAll:         []dbgen.GetChartDataAllRow{},
		chartDataAC:          []dbgen.GetChartDataACRow{},
	}
	rec := doHomepage(t, f, nil, "", "/index.php", (*Server).handleIndex)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestIndexPageWithOnSiteContest(t *testing.T) {
	f := &fakeQuerier{}
	rec := doHomepage(t, f, func(c *config.Config) {
		c.OnSiteContestID = 1000
	}, "", "/index.php", (*Server).handleIndex)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/contest.php?cid=1000" {
		t.Errorf("Location = %q, want /contest.php?cid=1000", loc)
	}
}

// ── 门户（portal.php）────────────────────────────────────────────────

func TestPortalPageRequiresLogin(t *testing.T) {
	f := &fakeQuerier{}
	rec := doHomepage(t, f, nil, "", "/portal.php", (*Server).handlePortal)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "请先登录") {
		t.Errorf("body should contain login prompt")
	}
}

func TestPortalPageWithLogin(t *testing.T) {
	f := &fakeQuerier{
		submittedProblems: []dbgen.GetUserSubmittedProblemsRow{
			{ProblemID: 1001, Result: int64(4)},
		},
		unsolvedProblems: []dbgen.GetUserUnsolvedProblemsRow{
			{ProblemID: 1002, Title: "Unsolved Problem"},
		},
		activeContests: []dbgen.GetActiveContestsRow{
			{ContestID: 1, Title: sql.NullString{String: "Active Contest", Valid: true}},
		},
	}
	rec := doHomepage(t, f, nil, seedAlice(), "/portal.php", (*Server).handlePortal)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Unsolved Problem") {
		t.Errorf("body should contain unsolved problem")
	}
	if !strings.Contains(body, "Active Contest") {
		t.Errorf("body should contain active contest")
	}
}

// ── 新闻详情（viewnews.php）─────────────────────────────────────────

func TestViewNewsPageNotFound(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrNoRows}
	rec := doHomepage(t, f, nil, "", "/viewnews.php?id=999", (*Server).handleViewNews)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "公告不存在") {
		t.Errorf("body should contain not found message")
	}
}

func TestViewNewsPageWithContent(t *testing.T) {
	f := &fakeQuerier{
		newsByID: []dbgen.GetNewsByIDRow{
			{
				NewsID:  1,
				Title:   "Test News",
				Content: "Test Content",
				UserID:  "admin",
			},
		},
	}
	rec := doHomepage(t, f, nil, "", "/viewnews.php?id=1", (*Server).handleViewNews)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Test News") {
		t.Errorf("body should contain news title")
	}
	if !strings.Contains(body, "Test Content") {
		t.Errorf("body should contain news content")
	}
	if !strings.Contains(body, "admin") {
		t.Errorf("body should contain news writer")
	}
}

func TestViewNewsPageWithOnSiteContest(t *testing.T) {
	f := &fakeQuerier{}
	rec := doHomepage(t, f, func(c *config.Config) {
		c.OnSiteContestID = 1000
	}, "", "/viewnews.php?id=1", (*Server).handleViewNews)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/contest.php?cid=1000" {
		t.Errorf("Location = %q, want /contest.php?cid=1000", loc)
	}
}
