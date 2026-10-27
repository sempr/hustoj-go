package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// statRowCells 从渲染结果里抽出包含 marker 的那一整个 <tr> 的单元格文本，
// 含题号/标签那一格；单元格内的标签被剥掉（如链接只留字母）。
func statRowCells(t *testing.T, body, marker string) []string {
	t.Helper()
	m := strings.Index(body, marker)
	if m < 0 {
		t.Fatalf("找不到行标记 %q\n%s", marker, trunc(body, 800))
	}
	start := strings.LastIndex(body[:m], "<tr")
	if start < 0 {
		t.Fatalf("标记 %q 前找不到 <tr>", marker)
	}
	seg := body[start:]
	if k := strings.Index(seg, "</tr>"); k >= 0 {
		seg = seg[:k]
	}
	const td = `<td class="text-center">`
	var cells []string
	for {
		j := strings.Index(seg, td)
		if j < 0 {
			break
		}
		seg = seg[j+len(td):]
		k := strings.Index(seg, "</td>")
		if k < 0 {
			break
		}
		cells = append(cells, stripTags(seg[:k]))
		seg = seg[k:]
	}
	return cells
}

// stripTags 去掉一小段 HTML 里的标签，只留文本。
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func equalCells(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestContestStatisticsMissingCid 对应 conteststatistics.php:10：
// 缺 cid 直接 die("No Such Contest!")，是纯文本，不是错误页。
func TestContestStatisticsMissingCid(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, nil)
	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet, "/conteststatistics.php", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if got := rec.Body.String(); got != "No Such Contest!" {
		t.Errorf("响应 = %q，期望纯文本 No Such Contest!", got)
	}
}

// TestContestStatisticsPending 比赛不存在/未开始都走"未开始"错误页，HTTP 200。
func TestContestStatisticsPending(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{err: sql.ErrNoRows}, nil)
	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet, "/conteststatistics.php?cid=99999", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "竞赛&作业 未开始!") {
		t.Errorf("缺少未开始文案\n%s", trunc(body, 800))
	}
	if !strings.Contains(body, msgBack) {
		t.Errorf("缺少返回链接")
	}
}

// TestContestStatsNOIP 锁住 conteststatistics.php:27-32 的 NOIP 判定与豁免。
//
// 与 contest.php 的 NOIP 逻辑不同：这里只看标题关键字与 contest_type&20，
// 豁免名单里也**没有** problem_editor。
func TestContestStatsNOIP(t *testing.T) {
	now := time.Now()
	row := func(title string, ctype int16) dbgen.GetContestByIDRow {
		return dbgen.GetContestByIDRow{
			Title:       sql.NullString{String: title, Valid: true},
			ContestType: sql.NullInt16{Int16: ctype, Valid: true},
			EndTime:     sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		}
	}
	tests := []struct {
		name string
		row  dbgen.GetContestByIDRow
		id   Identity
		want bool
	}{
		{"普通进行中比赛", row("周赛", 0), Anonymous(), false},
		{"标题含关键字", row("NOIP模拟", 0), Anonymous(), true},
		{"contest_type&16", row("周赛", 16), Anonymous(), true},
		{"contest_type&4", row("周赛", 4), Anonymous(), true},
		{"关键字大小写不敏感", row("noip2024", 0), Anonymous(), true},
		{"管理员豁免", row("NOIP模拟", 0), Identity{Admin: true}, false},
		{"限时赛标记豁免", row("NOIP模拟", 0), Identity{ContestTimeLimitUsed: map[int32]bool{7: true}}, false},
		{"源码浏览器豁免", row("NOIP模拟", 0), Identity{SourceBrowser: true}, false},
		{"比赛创建者豁免", row("NOIP模拟", 0), Identity{ContestCreator: true}, false},
		// problem_editor 不在统计页的豁免名单里（contest.php 里在）。
		{"题目编辑不豁免", row("NOIP模拟", 0), Identity{ProblemEditor: true}, true},
	}
	srv := newTestServer(t, &fakeQuerier{}, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := srv.contestStatsNOIP(tc.row, 7, tc.id, now); got != tc.want {
				t.Errorf("contestStatsNOIP = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// TestContestStatisticsNOIPErrorPage 匿名访问进行中的 NOIP 比赛 → 警告错误页。
func TestContestStatisticsNOIPErrorPage(t *testing.T) {
	contest := defaultContestRow(42)
	contest.Title = sql.NullString{String: "noip 模拟赛", Valid: true}
	srv := newTestServer(t, &fakeQuerier{contest: contest}, nil)
	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet, "/conteststatistics.php?cid=42", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "<h2>") || !strings.Contains(body, "比赛进行中，结束后才能查看结果。") {
		t.Errorf("缺少 NOIP 警告\n%s", trunc(body, 800))
	}
}

// TestContestStatisticsAggregates 用替身锁定聚合与列映射。
//
// 场景：cid=42 进行中，两题；
//
//	num0：AC(C++) + WA(C++)
//	num1：PE(C)
//
// 期望语言列只出现 C(j=11) 与 C++(j=12)，且各自计数正确。
func TestContestStatisticsAggregates(t *testing.T) {
	q := &fakeQuerier{
		contest: defaultContestRow(42),
		contestProblemIDs: []dbgen.ContestProblemID{
			{Num: 0, ProblemID: 1013},
			{Num: 1, ProblemID: 1014},
		},
		contestSolutionStats: []dbgen.ContestSolutionStat{
			{Result: 4, Num: 0, Language: 1}, // AC / C++
			{Result: 6, Num: 0, Language: 1}, // WA / C++
			{Result: 5, Num: 1, Language: 0}, // PE / C
		},
	}
	srv := newTestServer(t, q, nil)
	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet, "/conteststatistics.php?cid=42", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()

	// 表头：11 固定列 + C + C++。
	for _, h := range []string{"AC", "PE", "WA", "TLE", "MLE", "OLE", "RE", "CE", "TR", "Total"} {
		if !strings.Contains(body, ">"+h+"</th>") {
			t.Errorf("表头缺少 %q", h)
		}
	}
	if !strings.Contains(body, ">C</th>") || !strings.Contains(body, ">C&#43;&#43;</th>") {
		t.Errorf("语言列表头缺少 C 或 C++")
	}
	// 只应有这两个语言列：不该出现 Pascal。
	if strings.Contains(body, ">Pascal</th>") {
		t.Errorf("语言列多出了未使用的 Pascal")
	}

	wantA := []string{"A", "1", "", "1", "", "", "", "", "", "", "", "2", "", "2"}
	if got := statRowCells(t, body, `pid=0">A</a>`); !equalCells(got, wantA) {
		t.Errorf("A 行 = %v，期望 %v", got, wantA)
	}
	wantB := []string{"B", "", "1", "", "", "", "", "", "", "", "", "1", "1", ""}
	if got := statRowCells(t, body, `pid=1">B</a>`); !equalCells(got, wantB) {
		t.Errorf("B 行 = %v，期望 %v", got, wantB)
	}
	wantTotal := []string{"Total", "1", "1", "1", "", "", "", "", "", "", "", "3", "1", "2"}
	if got := statRowCells(t, body, ">Total</td>"); !equalCells(got, wantTotal) {
		t.Errorf("Total 行 = %v，期望 %v", got, wantTotal)
	}
}

// TestContestStatisticsEndedLinkHiddenWhenUsed 赛后题被未结束比赛占用时不给链接。
func TestContestStatisticsEndedLinkHiddenWhenUsed(t *testing.T) {
	contest := defaultContestRow(42)
	contest.EndTime = sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
	q := &fakeQuerier{
		contest: contest,
		contestProblemIDs: []dbgen.ContestProblemID{
			{Num: 0, ProblemID: 1013},
		},
		usedInOngoing: true,
	}
	srv := newTestServer(t, q, nil)
	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet, "/conteststatistics.php?cid=42", nil))

	body := rec.Body.String()
	if strings.Contains(body, "problem.php?id=1013") {
		t.Errorf("题目被未结束比赛占用，不应给出 problem.php?id 链接")
	}
	if !strings.Contains(body, `<td class="text-center">A</td>`) {
		t.Errorf("题号应保留为纯文本 A\n%s", trunc(body, 800))
	}
}

// TestContestStatisticsEndedLinkWhenFree 赛后题目无占用 → 链到 problem.php?id。
func TestContestStatisticsEndedLinkWhenFree(t *testing.T) {
	contest := defaultContestRow(42)
	contest.EndTime = sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
	q := &fakeQuerier{
		contest: contest,
		contestProblemIDs: []dbgen.ContestProblemID{
			{Num: 0, ProblemID: 1013},
		},
	}
	srv := newTestServer(t, q, nil)
	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet, "/conteststatistics.php?cid=42", nil))

	if body := rec.Body.String(); !strings.Contains(body, `href="problem.php?id=1013"`) {
		t.Errorf("赛后空闲题目应链到 problem.php?id=1013\n%s", trunc(body, 800))
	}
}

// TestContestStatisticsRenderIntegration 真库渲染 cid=1000，校验合计行的
// 提交总数等于库里的原始行数。真库缺失时跳过。
func TestContestStatisticsRenderIntegration(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_DSN") == "" {
		t.Skip("未设置 HUSTOJ_EQUIV_DSN，跳过需要真实库的统计页测试")
	}
	srv := newPageDB(t)
	ctx := context.Background()

	var cid int32
	err := srv.db.QueryRowContext(ctx, `SELECT contest_id FROM contest
		WHERE start_time < NOW() AND EXISTS (
			SELECT 1 FROM contest_problem cp WHERE cp.contest_id = contest.contest_id
		) ORDER BY contest_id LIMIT 1`).Scan(&cid)
	if err != nil {
		t.Skip("影子库没有已开始且带题目的比赛")
	}

	var want int
	if err := srv.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM solution WHERE contest_id = ? AND num >= 0`, cid).Scan(&want); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.handleContestStatistics(rec, httptest.NewRequest(http.MethodGet,
		"/conteststatistics.php?cid="+itoa32(cid), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}

	cells := statRowCells(t, rec.Body.String(), ">Total</td>")
	// 固定 11 列：j=10 是 Total（提交总数），后面可能再跟语言列。
	if len(cells) < 12 {
		t.Fatalf("合计行列数 = %d，至少应有 12", len(cells))
	}
	if cells[11] != itoa32(int32(want)) {
		t.Errorf("合计行提交总数 = %q，期望 %d", cells[11], want)
	}
}
