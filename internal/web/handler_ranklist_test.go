package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// startRe 抓分页链接里的 start 偏移，用于比对分页条而不受 & 转义影响。
var startRe = regexp.MustCompile(`ranklist\.php\?start=(\d+)`)

func startsIn(body string) []string {
	return startRe.FindAllString(body, -1)
}

// ranklistRows 从渲染结果里抽出 <tbody> 的所有行，每行是剥掉标签的单元格文本。
func ranklistRows(t *testing.T, body string) [][]string {
	t.Helper()
	i := strings.Index(body, "<tbody>")
	j := strings.Index(body, "</tbody>")
	if i < 0 || j < 0 || j < i {
		t.Fatalf("响应里找不到 tbody\n%s", trunc(body, 600))
	}
	seg := body[i+len("<tbody>") : j]
	var out [][]string
	for {
		ri := strings.Index(seg, "<tr>")
		if ri < 0 {
			break
		}
		seg = seg[ri+len("<tr>"):]
		re := strings.Index(seg, "</tr>")
		if re < 0 {
			break
		}
		rowSeg := seg[:re]
		seg = seg[re+len("</tr>"):]

		var cells []string
		for {
			ci := strings.Index(rowSeg, "<td>")
			if ci < 0 {
				break
			}
			rowSeg = rowSeg[ci+len("<td>"):]
			ce := strings.Index(rowSeg, "</td>")
			if ce < 0 {
				break
			}
			cells = append(cells, strings.TrimSpace(stripTags(rowSeg[:ce])))
			rowSeg = rowSeg[ce+len("</td>"):]
		}
		out = append(out, cells)
	}
	return out
}

// TestRanklistNOIPBlocksAnonymous 锁定 ranklist.php:10-17：有进行中的 NOIP
// 比赛时，非管理员整页换成警告错误页。
func TestRanklistNOIPBlocksAnonymous(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{runningNOIP: 1}, nil)
	rec := httptest.NewRecorder()
	srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<h2> noip 比赛进行中，结束后才能查看结果。 </h2>") {
		t.Errorf("缺少 NOIP 警告\n%s", trunc(body, 800))
	}
}

// TestRanklistNOIPAdminBypass 管理员不受 NOIP 拦截（ranklist.php:13 只查 admin）。
func TestRanklistNOIPAdminBypass(t *testing.T) {
	q := &fakeQuerier{runningNOIP: 1, ranklistRows: []dbgen.RanklistUserRow{
		{UserID: "sempr", Nick: "sempr", Solved: nullInt(1), Submit: nullInt(1)},
	}}
	srv := newTestServer(t, q, nil)
	store := newTestSessionStore(t)
	srv.sessions = store

	const sid = "ranklistadminsession"
	if err := store.Save(context.Background(), sid, &GoSession{
		UserID:     "admin",
		Privileges: GoPrivileges{Admin: true},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ranklist.php", nil)
	req.AddCookie(&http.Cookie{Name: srv.cfg.GoSessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	srv.handleRanklist(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "比赛进行中，结束后才能查看结果。") {
		t.Errorf("管理员不应被 NOIP 拦截\n%s", trunc(body, 800))
	}
	if !strings.Contains(body, "sempr") {
		t.Errorf("管理员应看到正常榜单\n%s", trunc(body, 800))
	}
}

// TestRanklistScopeNormalization scope 非空且非 d/w/m 一律当 y（ranklist.php:22-26），
// 走的是 scoped 查询而不是无窗口查询。
func TestRanklistScopeNormalization(t *testing.T) {
	for _, tc := range []struct {
		query       string
		wantScoped  bool
		description string
	}{
		{"", false, "无 scope 走全量"},
		{"?scope=", false, "空 scope 走全量"},
		{"?scope=d", true, "日榜"},
		{"?scope=w", true, "周榜"},
		{"?scope=m", true, "月榜"},
		{"?scope=y", true, "年榜"},
		{"?scope=zzz", true, "非法值归一到 y"},
		{"?scope=Y", true, "大写也归一"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			q := &fakeQuerier{}
			srv := newTestServer(t, q, nil)
			rec := httptest.NewRecorder()
			srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php"+tc.query, nil))

			calledScoped := false
			for _, c := range q.called {
				if c == "CountRanklistScopedUsers" || c == "ListRanklistScopedUsers" {
					calledScoped = true
				}
			}
			if calledScoped != tc.wantScoped {
				t.Errorf("scoped = %v，期望 %v（called=%v）", calledScoped, tc.wantScoped, q.called)
			}
		})
	}
}

// testLoc 是测试专用的固定 +8 时区。
//
// 生产代码不含任何具体时区（由进程 TZ 决定，见 handler_ranklist.go 文件头）；
// 但断言跨日/跨周边界必须可复现，不能依赖跑测机器的 TZ，故在测试里显式固定。
var testLoc = time.FixedZone("CST", 8*60*60)

// TestRanklistScopeStart 用固定 +8 时刻验证日/周/月/年的起算与上一周期。
//
// 用周日与周一两个点覆盖 PHP strtotime('monday this week') 的边界。
func TestRanklistScopeStart(t *testing.T) {
	// 2026-10-04 是周日，当地 07:45。
	sunday := time.Date(2026, 10, 4, 7, 45, 0, 0, testLoc)
	// 2026-10-05 是周一。
	monday := time.Date(2026, 10, 5, 0, 30, 0, 0, testLoc)

	tests := []struct {
		scope     string
		now       time.Time
		wantStart string
		wantPrev  string
	}{
		{"d", sunday, "2026-10-04", "2026-10-03"},
		{"w", sunday, "2026-09-28", "2026-09-21"},
		{"m", sunday, "2026-10-01", "2026-09-01"},
		{"y", sunday, "2026-01-01", "2025-01-01"},
		{"d", monday, "2026-10-05", "2026-10-04"},
		{"w", monday, "2026-10-05", "2026-09-28"},
	}
	for _, tc := range tests {
		gotStart := ranklistScopeStart(tc.scope, tc.now).Format("2006-01-02")
		if gotStart != tc.wantStart {
			t.Errorf("scope=%s start = %s，期望 %s", tc.scope, gotStart, tc.wantStart)
		}
		gotPrev := ranklistScopePrevStart(tc.scope, tc.now).Format("2006-01-02")
		if gotPrev != tc.wantPrev {
			t.Errorf("scope=%s prev = %s，期望 %s", tc.scope, gotPrev, tc.wantPrev)
		}
	}
}

func TestRanklistRatio(t *testing.T) {
	tests := []struct {
		solved, submit int
		want           string
	}{
		{6, 6, "100.00%"},
		{1, 1, "100.00%"},
		{0, 0, "0.00%"},
		{1, 2, "50.00%"},
		{2, 10, "20.00%"},
		{1, 3, "33.33%"},
	}
	for _, tc := range tests {
		if got := ranklistRatio(tc.solved, tc.submit); got != tc.want {
			t.Errorf("ranklistRatio(%d,%d) = %q，期望 %q", tc.solved, tc.submit, tc.want, got)
		}
	}
}

// TestRanklistAssembly 用替身锁定名次、星标、链接与比率。
func TestRanklistAssembly(t *testing.T) {
	q := &fakeQuerier{
		ranklistTotal: 8,
		ranklistRows: []dbgen.RanklistUserRow{
			{UserID: "max", Nick: "max", Solved: nullInt(6), Submit: nullInt(6)},
			{UserID: "boss", Nick: "老板", Solved: nullInt(1), Submit: nullInt(0), Starred: 1},
		},
	}
	srv := newTestServer(t, q, nil)
	rec := httptest.NewRecorder()
	srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php", nil))

	rows := ranklistRows(t, rec.Body.String())
	want := [][]string{
		{"1", "max", "max", "", "6", "6", "100.00%"},
		{"2", "⭐boss⭐", "老板", "", "1", "0", "0.00%"},
	}
	if len(rows) != len(want) {
		t.Fatalf("行数 = %d，期望 %d\n%v", len(rows), len(want), rows)
	}
	for i := range want {
		if !equalCells(rows[i], want[i]) {
			t.Errorf("第 %d 行 = %v，期望 %v", i+1, rows[i], want[i])
		}
	}
	if !strings.Contains(rec.Body.String(), "ranklist.php?start=0") {
		t.Errorf("缺少分页链接")
	}
}

func nullInt(v int32) sql.NullInt32 { return sql.NullInt32{Int32: v, Valid: true} }

// TestRanklistPagination 锁定分页条：i 严格小于 view_total，步长 50；
// scope 模式在链接上带 scope。总数 150 → 3 个链接。
func TestRanklistPagination(t *testing.T) {
	t.Run("非scope", func(t *testing.T) {
		srv := newTestServer(t, &fakeQuerier{ranklistTotal: 150}, nil)
		rec := httptest.NewRecorder()
		srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php", nil))
		got := startsIn(rec.Body.String())
		want := []string{"ranklist.php?start=0", "ranklist.php?start=50", "ranklist.php?start=100"}
		if !equalCells(got, want) {
			t.Errorf("分页 = %v，期望 %v", got, want)
		}
	})
	t.Run("scope", func(t *testing.T) {
		srv := newTestServer(t, &fakeQuerier{ranklistScopedTot: 150}, nil)
		rec := httptest.NewRecorder()
		srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php?scope=y", nil))
		got := startsIn(rec.Body.String())
		// html/template 会把 & 转义成 &amp;，这里只比对 start 值。
		want := []string{"ranklist.php?start=0", "ranklist.php?start=50", "ranklist.php?start=100"}
		if !equalCells(got, want) {
			t.Errorf("分页 = %v，期望 %v", got, want)
		}
		if !strings.Contains(rec.Body.String(), "scope=y") {
			t.Errorf("scope 模式分页链接应带 scope=y")
		}
	})
	t.Run("零行无分页", func(t *testing.T) {
		srv := newTestServer(t, &fakeQuerier{}, nil)
		rec := httptest.NewRecorder()
		srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php?scope=d", nil))
		if got := startsIn(rec.Body.String()); len(got) != 0 {
			t.Errorf("零行不应有分页链接，得到 %v", got)
		}
	})
}

// TestRanklistRenderIntegration 真库渲染，分别对拍非 scope 与 scope=y。
//
// oracle 是 PHP 的原 SQL 直查影子库：非 scope 按 solved/submit/reg_time 排序，
// scope 用 solution_id 窗口 + first_time=1 聚合。真库缺失时跳过。
func TestRanklistRenderIntegration(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_DSN") == "" {
		t.Skip("未设置 HUSTOJ_EQUIV_DSN，跳过需要真实库的排名页测试")
	}
	srv := newPageDB(t)
	ctx := context.Background()

	// ── 非 scope ────────────────────────────────────────────────────
	var wantBase []string
	dbRows, err := srv.db.QueryContext(ctx, `
SELECT user_id, solved, submit FROM users
WHERE user_id NOT IN ('admin','zhblue') AND defunct='N'
ORDER BY solved DESC, submit, reg_time LIMIT 50`)
	if err != nil {
		t.Fatal(err)
	}
	for dbRows.Next() {
		var uid string
		var solved, submit int
		if err := dbRows.Scan(&uid, &solved, &submit); err != nil {
			t.Fatal(err)
		}
		wantBase = append(wantBase, uid)
	}
	dbRows.Close()

	rec := httptest.NewRecorder()
	srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	baseRows := ranklistRows(t, rec.Body.String())
	if len(baseRows) != len(wantBase) {
		t.Fatalf("非 scope 行数 = %d，期望 %d", len(baseRows), len(wantBase))
	}
	for i, uid := range wantBase {
		if len(baseRows[i]) < 2 || baseRows[i][1] != uid {
			t.Errorf("第 %d 行用户 = %v，期望 %s", i+1, baseRows[i], uid)
		}
	}

	// ── scope=y：与 PHP 的窗口聚合对拍 ──────────────────────────────
	// scope=y 的起点是当年 1 月 1 日（ranklistScopeStart），不是今天。
	// 用 testLoc 构造，让 SQL 里 CURDATE() 与本地计算落在同一时区。
	yearStart := ranklistScopeStart("y", time.Now().In(testLoc)).Format("2006-01-02")
	var lastID int64
	err = srv.db.QueryRowContext(ctx, `
SELECT solution_id FROM solution
WHERE in_date < STR_TO_DATE(?, '%Y-%m-%d') ORDER BY solution_id DESC LIMIT 1`,
		yearStart).Scan(&lastID)
	if err == sql.ErrNoRows {
		t.Skip("影子库没有任何提交，无法测 scope")
	} else if err != nil {
		t.Fatal(err)
	}

	type wantRow struct {
		uid    string
		solved int
		submit int
	}
	var wants []wantRow
	oracle, err := srv.db.QueryContext(ctx, `
SELECT users.user_id, s.solved, t.submit
FROM users
INNER JOIN (
  SELECT count(DISTINCT problem_id) solved, user_id FROM solution
  WHERE solution_id > ? AND user_id NOT IN ('admin','zhblue') AND problem_id > 0 AND result = 4 AND first_time = 1
  GROUP BY user_id ORDER BY solved DESC LIMIT 0, 50
) s ON users.user_id = s.user_id
INNER JOIN (
  SELECT count(problem_id) submit, user_id FROM solution
  WHERE solution_id > ? GROUP BY user_id ORDER BY submit DESC
) t ON users.user_id = t.user_id AND users.user_id NOT IN ('admin','zhblue') AND defunct='N'
ORDER BY s.solved DESC, t.submit, reg_time LIMIT 0, 50`, lastID, lastID)
	if err != nil {
		t.Fatal(err)
	}
	for oracle.Next() {
		var w wantRow
		if err := oracle.Scan(&w.uid, &w.solved, &w.submit); err != nil {
			t.Fatal(err)
		}
		wants = append(wants, w)
	}
	oracle.Close()

	rec = httptest.NewRecorder()
	srv.handleRanklist(rec, httptest.NewRequest(http.MethodGet, "/ranklist.php?scope=y", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scope=y 状态码 = %d，期望 200", rec.Code)
	}
	yRows := ranklistRows(t, rec.Body.String())
	if len(yRows) != len(wants) {
		t.Fatalf("scope=y 行数 = %d，期望 %d\n%v", len(yRows), len(wants), yRows)
	}
	for i, w := range wants {
		if len(yRows[i]) != 7 {
			t.Fatalf("scope=y 第 %d 行列数 = %d，期望 7", i+1, len(yRows[i]))
		}
		if yRows[i][1] != w.uid ||
			yRows[i][4] != itoa32(int32(w.solved)) ||
			yRows[i][5] != itoa32(int32(w.submit)) {
			t.Errorf("scope=y 第 %d 行 = %v，期望 %s %d/%d", i+1, yRows[i], w.uid, w.solved, w.submit)
		}
	}
}
