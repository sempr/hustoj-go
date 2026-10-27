package web

// 补题榜（contestrank4.php / contestrank5.php）的回归测试。
//
// 这里锁的是三处最容易在后续重构中被"顺手修掉"的行为差异，都是本次迁移
// 实际踩到并逐条与部署版 PHP 对拍确认过的：
//
//  1. 取数按 problem_id + 开赛时间，不按 contest_id。补题榜统计的是"比赛结束
//     后在练习里补做比赛原题"的提交，所以别的比赛、甚至不属于任何比赛的提交
//     也会进榜。改成按 contest_id 取是最自然的一种"优化"，但会直接改变榜面。
//  2. contestrank5 的 pass_rate 阈值是 0.99 归零，和 contestrank-oi 的 0.95
//     封顶不是一回事，且 contestrank5 不会把 AC 强制成 1.0。
//  3. 比赛没有题目时给出空榜。部署版 PHP 会把 implode 结果直接塞进
//     `problem_id in ()`，MariaDB 报 1064，页面直接吐出 SQL 与错误信息。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// reviewFixtureID 是测试自建比赛的 contest_id，落在 990001 段，避开真实数据。
const reviewFixtureID = 990050

// timeZero 是拼 SQL 条件用的占位时间，不参与比较。
var timeZero = time.Time{}

var (
	reviewP1 = 899901
	reviewP2 = 899902
)

// reviewCellRe 从渲染结果里抓某个选手某一行的全部单元格文本。
var reviewRowRe = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
var reviewCellRe = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
var reviewTagRe = regexp.MustCompile(`(?s)<[^>]+>`)
var reviewWSRe = regexp.MustCompile(`\s+`)

// reviewTableRows 抽出表格 tbody 里每一行的单元格文本。
func reviewTableRows(t *testing.T, page string) [][]string {
	t.Helper()
	tb := regexp.MustCompile(`(?is)<tbody[^>]*>(.*?)</tbody>`).FindStringSubmatch(page)
	if tb == nil {
		return nil
	}
	var out [][]string
	for _, tr := range reviewRowRe.FindAllStringSubmatch(tb[1], -1) {
		var cells []string
		for _, c := range reviewCellRe.FindAllStringSubmatch(tr[1], -1) {
			text := reviewWSRe.ReplaceAllString(strings.TrimSpace(reviewTagRe.ReplaceAllString(c[1], " ")), " ")
			cells = append(cells, text)
		}
		if len(cells) > 0 {
			out = append(out, cells)
		}
	}
	return out
}

// reviewRowFor 返回该选手所在行。
func reviewRowFor(t *testing.T, page, userID string) []string {
	t.Helper()
	for _, row := range reviewTableRows(t, page) {
		for _, c := range row {
			if c == userID {
				return row
			}
		}
	}
	t.Fatalf("页面里找不到选手 %s 的行", userID)
	return nil
}

// renderReviewRank 渲染指定补题榜。
func renderReviewRank(t *testing.T, srv *Server, mode rankMode, cid int) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleContestReviewRank(rec,
		httptest.NewRequest(http.MethodGet, fmt.Sprintf("/contestrank.php?cid=%d", cid), nil), mode)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	return rec.Body.String()
}

// setupReviewFixture 建一个两题的比赛，并造出几种典型提交。
//
// 关键点：这些 solution 的 contest_id 指向另一场比赛（999999），用来证明补题榜
// 确实不看 contest_id；如果实现被改成按 contest_id 取，下面的断言就会失败。
func setupReviewFixture(t *testing.T, srv *Server) {
	t.Helper()
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := srv.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("准备数据失败 (%s): %v", q, err)
		}
	}
	cleanup := func() {
		t.Helper()
		// 清理失败必须报出来。之前这里是 `_, _ =`，删除被静默吞掉，下一个
		// 用例再插同一批固定 ID 就会撞 Duplicate entry，而且看不出是哪一步
		// 没删掉。
		//
		// 也不能用 `DELETE FROM solution WHERE solution_id >= 9900000` 这种
		// 阈值删除：下面要显式指定 solution_id（Add 的累加依赖提交顺序），
		// 那些显式 ID 会把 AUTO_INCREMENT 顶到 9900007，于是之后所有自动
		// 生成的 solution_id 都 >= 9900000，会被这个阈值删掉 —— 别的用例
		// （以及手工造的影子库数据）会莫名其妙地丢提交。
		for _, d := range []struct {
			q    string
			args []any
		}{
			{`DELETE FROM solution WHERE contest_id IN (?, 999999)`,
				[]any{reviewFixtureID}},
			{`DELETE FROM solution WHERE user_id IN (?, ?, ?, ?)`,
				[]any{"rv1", "rv2", "rv3", "rv4"}},
			{`DELETE FROM contest_problem WHERE contest_id = ?`, []any{reviewFixtureID}},
			// 999999 是字面量，不是占位符，只能传一个参数。
			{`DELETE FROM contest WHERE contest_id IN (?, 999999)`,
				[]any{reviewFixtureID}},
		} {
			if _, err := srv.db.ExecContext(ctx, d.q, d.args...); err != nil {
				t.Errorf("清理 fixture 失败 (%s): %v", d.q, err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	exec(`INSERT INTO contest (contest_id, title, start_time, end_time, defunct, contest_type)
	      VALUES (?, 'REVIEW-FIXTURE', '2020-01-01 00:00:00', '2020-01-02 00:00:00', 'N', 0)`,
		reviewFixtureID)
	exec(`INSERT INTO contest (contest_id, title, start_time, end_time, defunct, contest_type)
	      VALUES (999999, 'UNRELATED', '2020-01-01 00:00:00', '2020-01-02 00:00:00', 'N', 0)`)
	exec(`INSERT INTO contest_problem (contest_id, num, problem_id, title) VALUES
	      (?, 0, ?, 'P1'), (?, 1, ?, 'P2')`, reviewFixtureID, reviewP1, reviewFixtureID, reviewP2)

	// rv1 两题全 AC；rv2 两题都卡在 0.995；rv3 一题 0.90；rv4 只交了一次 CE。
	const sid = 9900000
	exec(`INSERT INTO solution
	      (solution_id, problem_id, user_id, num, result, in_date, pass_rate, contest_id, nick) VALUES
	      (?, ?, 'rv1', 0, 4,  '2020-01-01 01:00:00', 1.000, 999999, 'rv1nick'),
	      (?, ?, 'rv1', 1, 4,  '2020-01-01 02:00:00', 1.000, 999999, 'rv1nick'),
	      (?, ?, 'rv2', 0, 6,  '2020-01-01 01:30:00', 0.995, 999999, 'rv2nick'),
	      (?, ?, 'rv2', 1, 6,  '2020-01-01 02:30:00', 0.995, 999999, 'rv2nick'),
	      (?, ?, 'rv3', 0, 7,  '2020-01-01 03:00:00', 0.900, 999999, 'rv3nick'),
	      (?, ?, 'rv4', 0, 11, '2020-01-01 04:00:00', 0.000, 999999, 'rv4nick')`,
		sid+1, reviewP1, sid+2, reviewP2, sid+3, reviewP1,
		sid+4, reviewP2, sid+5, reviewP1, sid+6, reviewP1)
}

// TestReviewRankCountsSubmissionsFromOtherContests 锁死取数口径：
// 提交的 contest_id 指向 999999，补题榜依然要统计它们。
func TestReviewRankCountsSubmissionsFromOtherContests(t *testing.T) {
	srv := newPageDB(t)
	setupReviewFixture(t, srv)

	for _, tc := range []struct {
		name string
		mode rankMode
	}{{"ACM", rankACM}, {"OI", rankOI}} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderReviewRank(t, srv, tc.mode, reviewFixtureID)
			rows := reviewTableRows(t, page)
			if len(rows) != 4 {
				t.Fatalf("行数 = %d，期望 4（rv1..rv4）", len(rows))
			}
			// rv1 两题都通过，解题数应为 2。
			row := reviewRowFor(t, page, "rv1")
			if row[3] != "2" {
				t.Errorf("rv1 解题数 = %q，期望 2", row[3])
			}
		})
	}
}

// TestReviewRankOIPassRateZeroedAt099 锁死 contestrank5 的 0.99 归零规则。
//
// 同一份数据在 contestrank-oi.php 上会被封顶成 95 分，两个榜面刻意不同。
func TestReviewRankOIPassRateZeroedAt099(t *testing.T) {
	srv := newPageDB(t)
	setupReviewFixture(t, srv)

	page := renderReviewRank(t, srv, rankOI, reviewFixtureID)
	row := reviewRowFor(t, page, "rv2")

	// OI 榜固定多一列总分，列序：排名/用户/昵称/解题数/罚时/总分/题A/题B。
	if len(row) < 8 {
		t.Fatalf("列数 = %d，至少 8", len(row))
	}
	if row[5] != "0" {
		t.Errorf("rv2 总分 = %q，期望 0（pass_rate 0.995 应被归零，而不是封顶 95）", row[5])
	}
	if row[6] != "0" || row[7] != "0" {
		t.Errorf("rv2 两题得分 = %q/%q，期望都是 0", row[6], row[7])
	}

	// rv3 的 0.90 没到 0.99，应原样保留。
	row3 := reviewRowFor(t, page, "rv3")
	if row3[6] != "90" {
		t.Errorf("rv3 题A得分 = %q，期望 90", row3[6])
	}
}

// TestReviewRankNoProblemsRendersEmptyBoard 锁死空题目比赛的兜底。
//
// 部署版 PHP 在这里会拼出 `problem_id in ()` 并把 SQLSTATE 1064 吐到页面上。
func TestReviewRankNoProblemsRendersEmptyBoard(t *testing.T) {
	srv := newPageDB(t)
	ctx := context.Background()

	const emptyCID = 990002
	if _, err := srv.db.ExecContext(ctx, `DELETE FROM contest WHERE contest_id = ?`, emptyCID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = srv.db.ExecContext(ctx, `DELETE FROM contest WHERE contest_id = ?`, emptyCID)
	})
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO contest
	    (contest_id, title, start_time, end_time, defunct, contest_type)
	    VALUES (?, 'EMPTY-REVIEW', '2020-01-01 00:00:00', '2020-01-02 00:00:00', 'N', 0)`, emptyCID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		mode rankMode
	}{{"ACM", rankACM}, {"OI", rankOI}} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderReviewRank(t, srv, tc.mode, emptyCID)
			for _, bad := range []string{"SQLSTATE", "1064", "problem_id in ()"} {
				if strings.Contains(page, bad) {
					t.Errorf("页面泄露了数据库错误信息 %q", bad)
				}
			}
			if !strings.Contains(page, "暂无选手提交") {
				t.Error("空题目比赛应渲染空榜占位")
			}
		})
	}
}

// TestReviewProblemFilter 覆盖 SQL 片段拼装，尤其是空题目列表的短路。
func TestReviewProblemFilter(t *testing.T) {
	t.Run("空题目列表直接短路", func(t *testing.T) {
		cond, args, ok := reviewProblemFilter(nil, timeZero, []string{"admin"})
		if ok {
			t.Error("空题目列表应返回 ok=false，避免发出 `problem_id in ()`")
		}
		if cond != "" || args != nil {
			t.Errorf("短路时应返回空条件，实际 cond=%q args=%v", cond, args)
		}
	})

	t.Run("占位符与参数顺序", func(t *testing.T) {
		cond, args, ok := reviewProblemFilter([]int32{7, 8, 9}, timeZero, []string{"admin", "zhblue"})
		if !ok {
			t.Fatal("ok 应为 true")
		}
		want := "in_date >= ? AND problem_id IN (?,?,?) AND user_id NOT IN (?,?)"
		if cond != want {
			t.Errorf("条件 =\n  %q\n期望\n  %q", cond, want)
		}
		// 第一个参数是开始时间，其后依次是 problem_id，最后是隐藏账号。
		if len(args) != 6 {
			t.Fatalf("参数个数 = %d，期望 6", len(args))
		}
		if args[0] != timeZero {
			t.Errorf("args[0] = %v，期望开始时间", args[0])
		}
		if args[1] != int32(7) || args[3] != int32(9) {
			t.Errorf("problem_id 参数位置不对: %v", args[1:4])
		}
		if args[4] != "admin" || args[5] != "zhblue" {
			t.Errorf("隐藏账号参数位置不对: %v", args[4:])
		}
	})

	t.Run("无隐藏账号时省略 NOT IN", func(t *testing.T) {
		cond, args, ok := reviewProblemFilter([]int32{7}, timeZero, nil)
		if !ok {
			t.Fatal("ok 应为 true")
		}
		if strings.Contains(cond, "NOT IN") {
			t.Errorf("隐藏名单为空时不该带 NOT IN: %q", cond)
		}
		if len(args) != 2 {
			t.Errorf("参数个数 = %d，期望 2", len(args))
		}
	})
}

// TestReviewPlaceholders 单测占位符生成。
func TestReviewPlaceholders(t *testing.T) {
	cases := map[int]string{0: "", 1: "?", 3: "?,?,?", -1: ""}
	for n, want := range cases {
		if got := reviewPlaceholders(n); got != want {
			t.Errorf("reviewPlaceholders(%d) = %q，期望 %q", n, got, want)
		}
	}
}

// TestReviewBackURL 返回按钮应指回对应的正式榜。
func TestReviewBackURL(t *testing.T) {
	if got := reviewBackURL(rankACM, 7); got != "contestrank.php?cid=7" {
		t.Errorf("ACM 返回链接 = %q", got)
	}
	if got := reviewBackURL(rankOI, 7); got != "contestrank-oi.php?cid=7" {
		t.Errorf("OI 返回链接 = %q", got)
	}
}
