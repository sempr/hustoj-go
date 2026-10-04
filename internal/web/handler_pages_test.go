package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// newPageDB 打开影子库，供档案页与比赛页的集成测试使用。
func newPageDB(t *testing.T) *Server {
	t.Helper()
	dsn := os.Getenv("HUSTOJ_EQUIV_DSN")
	if dsn == "" {
		t.Skip("未设置 HUSTOJ_EQUIV_DSN，跳过需要真实库的页面测试")
	}
	f := newAuthFixture(t)
	f.srv.cfg.SessionBridge = false
	return f.srv
}

// ── 档案页 ──────────────────────────────────────────────────────────────

// TestUserInfoRenderIntegration 真库跑一次档案页。
//
// 目的有三个：模板能在运行时解析（text/template 不在编译期校验）、
// userinfo.sql.go 的 SQL 在真实表结构上能跑通、写回 solved/submit
// 不会因为列不存在而 500。
func TestUserInfoRenderIntegration(t *testing.T) {
	srv := newPageDB(t)

	for _, tc := range []struct {
		name    string
		user    string
		wantSub []string
	}{
		{"真实用户", "sempr", []string{"sempr", "吉尔吉斯倔驴大学OJ"}},
		{"非法用户名走纯文本", "bad!name", []string{"No such User!"}},
		{"缺失用户走错误页", "definitely_absent_user", []string{"No such User!"}},
		{"隐藏账号对游客不可见", "admin", []string{"No such User!"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.handleUserInfo(rec, httptest.NewRequest(http.MethodGet,
				"/userinfo.php?user="+url.QueryEscape(tc.user), nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d, 期望 200", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tc.wantSub {
				if !strings.Contains(body, want) {
					t.Errorf("响应缺少 %q\n前 500 字: %s", want, trunc(body, 500))
				}
			}
		})
	}
}

// TestUserInfoUpdatesSolvedSubmit 验证 GET 档案页会重算 solved/submit。
//
// 这是 PHP 既有行为（userinfo.php:90-95），迁移必须保留，
// 否则靠"打开一次个人页"刷新的统计会永远停在旧值。
func TestUserInfoUpdatesSolvedSubmit(t *testing.T) {
	srv := newPageDB(t)
	db := srv.db
	ctx := context.Background()

	const uid = "uisync01"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM solution WHERE user_id = ?", uid)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE user_id = ?", uid)
	})

	// 造四条提交，覆盖三个计数口径：
	//   pid=0  练习提交，problem_id>0 过滤掉，两项都不计
	//   pid=101 AC 一道
	//   pid=101 AC 重复一道（验证 DISTINCT）
	//   pid=102 非 AC 一道（计 submit 不计 solved）
	if _, err := db.ExecContext(ctx, `INSERT INTO users
		(user_id, email, ip, accesstime, password, reg_time, nick, school,
		 group_name, defunct, expiry_date)
		VALUES (?, 'u@test.local', '127.0.0.1', NOW(), 'x', NOW(), 'u', 'S', 'g', 'N', '2099-01-01')
		ON DUPLICATE KEY UPDATE defunct = 'N'`, uid); err != nil {
		t.Fatalf("建用户失败: %v", err)
	}
	// 清掉历史 solution，避免残留影响计数。
	if _, err := db.ExecContext(ctx, "DELETE FROM solution WHERE user_id = ?", uid); err != nil {
		t.Fatalf("清理旧提交失败: %v", err)
	}
	for _, row := range []struct{ pid, result int }{
		{0, 4},   // 练习提交：problem_id=0，两项都不计
		{101, 4}, // AC
		{101, 4}, // 重复 AC 同一题：DISTINCT 后仍只算一题
		{102, 5}, // 非 AC：只进 submit
	} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO solution (user_id, problem_id, result, num, contest_id, time) VALUES (?, ?, ?, 0, 0, UNIX_TIMESTAMP())",
			uid, row.pid, row.result); err != nil {
			t.Fatalf("写提交失败: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE users SET solved = 99, submit = 99 WHERE user_id = ?", uid); err != nil {
		t.Fatalf("预置脏值失败: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.handleUserInfo(rec, httptest.NewRequest(http.MethodGet,
		"/userinfo.php?user="+uid, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	var solved, submit int
	if err := db.QueryRowContext(ctx,
		"SELECT solved, submit FROM users WHERE user_id = ?", uid).Scan(&solved, &submit); err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	// PHP 用 count(DISTINCT problem_id) 且要求 problem_id>0（userinfo.php:130,135）：
	// 101 题两次 AC 只算一题，102 题的错解只进 submit，练习提交两项都不计。
	if solved != 1 || submit != 2 {
		t.Errorf("重算后 solved=%d submit=%d, 期望 1/2", solved, submit)
	}
}

// ── 比赛页 ──────────────────────────────────────────────────────────────

// TestContestListRenderIntegration 真库渲染比赛列表页。
//
// 覆盖三件事：模板运行时可解析、ContestSetFilter 动态 SQL 能在真实表上跑、
// 分页与总页数不越界。
func TestContestListRenderIntegration(t *testing.T) {
	srv := newPageDB(t)

	for _, q := range []string{
		"",
		"?page=1",
		"?page=9999",
		"?keyword=" + url.QueryEscape("不存在的比赛名"),
		"?my=1",
		"?page=2&my=1",
	} {
		t.Run("GET contest.php"+q, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.handleContestList(rec, httptest.NewRequest(http.MethodGet, "/contest.php"+q, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d, 期望 200", rec.Code)
			}
			if body := rec.Body.String(); !strings.Contains(body, "contest.php?cid=") &&
				!strings.Contains(body, "暂无比赛") {
				t.Errorf("既没有比赛链接也没有空态提示，前 500 字: %s", trunc(body, 500))
			}
		})
	}
}

// TestContestListAcceptsPOST 验证搜索表单的 POST 能落到同一分支。
//
// contestset.php:7 是 method=post，若路由只挂 GET，这里会直接 405。
func TestContestListAcceptsPOST(t *testing.T) {
	srv := newPageDB(t)

	form := url.Values{"keyword": {"比赛"}}
	req := httptest.NewRequest(http.MethodPost, "/contest.php", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	srv.handleContestList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST 状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `value="比赛"`) {
		t.Error("搜索框应回填 POST 提交的关键词")
	}
}

// TestContestProblemsFill 造一场比赛，验证题目页的标记与打码逻辑。
//
// 用影子库自造数据：插入 contest + contest_problem + solution，
// 覆盖 AC/非 AC/未交三种情形，以及 NOIP 下只出问号。
func TestContestProblemsFill(t *testing.T) {
	srv := newPageDB(t)
	db := srv.db
	ctx := context.Background()

	const (
		cid = int32(90210)
		uid = "cprobfill01"
	)
	pids := make([]int, 3)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM solution WHERE contest_id = ?", cid)
		_, _ = db.ExecContext(ctx, "DELETE FROM contest_problem WHERE contest_id = ?", cid)
		_, _ = db.ExecContext(ctx, "DELETE FROM contest WHERE contest_id = ?", cid)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE user_id = ?", uid)
		for i := range pids {
			_, _ = db.ExecContext(ctx, "DELETE FROM problem WHERE problem_id = ?", pids[i])
		}
	})

	// 用固定 ID 段而不是自增：自增会让每跑一次测试就在库里多留三道题，
	// 几次下来 problem 表就被'cp test'污染，也不好排查。
	const basePID = 999001
	for i := range pids {
		pid := basePID + i
		// problem_id 之外的必填列在表定义里没有默认值，必须显式给全。
		if _, err := db.ExecContext(ctx, `INSERT INTO problem
			(problem_id, title, defunct, time_limit, memory_limit, source, coin,
			 description, input, output, hint)
			VALUES (?, 'cp test', 'N', 1000, 65536, '', ?, '', '', '', '')
			ON DUPLICATE KEY UPDATE defunct = 'N', coin = VALUES(coin)`, pid, 30+i); err != nil {
			t.Fatalf("建题目 %d 失败: %v", pid, err)
		}
		pids[i] = pid
	}

	// 标题含 NOIP，让 noip 判定走到"标题命中"这一支。
	if _, err := db.ExecContext(ctx, `INSERT INTO contest
		(contest_id, title, start_time, end_time, defunct, private, contest_type, user_id)
		VALUES (?, 'Go 对拍 NOIP 场', ?, ?, 'N', 1, 0, 0)
		ON DUPLICATE KEY UPDATE title = VALUES(title), start_time = VALUES(start_time),
			end_time = VALUES(end_time), defunct = 'N'`,
		cid, time.Now().Add(-time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("建比赛失败: %v", err)
	}

	for num, pid := range pids {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO contest_problem (contest_id, num, problem_id) VALUES (?, ?, ?)",
			cid, num, pid); err != nil {
			t.Fatalf("挂题失败: %v", err)
		}
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO users
		(user_id, email, ip, accesstime, password, reg_time, nick, school,
		 group_name, defunct, expiry_date)
		VALUES (?, 'c@test.local', '127.0.0.1', NOW(), 'x', NOW(), 'c', 'S', 'g', 'N', '2099-01-01')
		ON DUPLICATE KEY UPDATE defunct = 'N'`, uid); err != nil {
		t.Fatalf("建用户失败: %v", err)
	}
	// 第 0 题交对、第 1 题交错、第 2 题没交。
	if _, err := db.ExecContext(ctx,
		"INSERT INTO solution (user_id, problem_id, result, num, contest_id, time) VALUES (?, ?, 4, 0, ?, UNIX_TIMESTAMP())",
		uid, pids[0], cid); err != nil {
		t.Fatalf("写 AC 提交失败: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO solution (user_id, problem_id, result, num, contest_id, time) VALUES (?, ?, 5, 1, ?, UNIX_TIMESTAMP())",
		uid, pids[1], cid); err != nil {
		t.Fatalf("写错误提交失败: %v", err)
	}

	id := Identity{UserID: uid, LoggedIn: true}
	contest, err := srv.queries().GetContestByID(ctx, cid)
	if err != nil {
		t.Fatalf("读比赛失败: %v", err)
	}
	flags, err := srv.contestNoIPFlags(ctx, srv.queries(), contest, cid, id, time.Now().Unix())
	if err != nil {
		t.Fatalf("NOIP 判定失败: %v", err)
	}
	if !flags.NOIP {
		t.Fatalf("标题含 NOIP 且比赛进行中，应判定为 NOIP 模式")
	}
	flags.LoggedIn = true

	probs, err := GetContestProblems(ctx, srv.queries(), nullInt32(cid))
	if err != nil {
		t.Fatalf("读题目失败: %v", err)
	}
	if len(probs) != 3 {
		t.Fatalf("题目数 = %d, 期望 3", len(probs))
	}
	if err := srv.fillContestProblems(ctx, srv.queries(), probs, cid, id, flags); err != nil {
		t.Fatalf("填充题目状态失败: %v", err)
	}

	// NOIP 下：交过的一律 "?"，没交的没有标记。
	if probs[0].Mark != "?" || probs[1].Mark != "?" || probs[2].Mark != "" {
		t.Errorf("NOIP 标记 = %q/%q/%q, 期望 ?/?/空", probs[0].Mark, probs[1].Mark, probs[2].Mark)
	}
	// 题号字母应是 A/B/C。
	for i, want := range []string{"A", "B", "C"} {
		if probs[i].Letter != want {
			t.Errorf("第 %d 题字母 = %q, 期望 %q", i, probs[i].Letter, want)
		}
	}
	// 该用户不是管理员也不持 m<cid>，AC/提交数应被打码。
	for i := range probs {
		if !probs[i].Masked {
			t.Errorf("第 %d 题应打码（NOIP + 非豁免角色）", i)
		}
	}

	// 去掉 NOIP 后：应恢复 Y / N / 空。
	flags.NOIP = false
	flags.HideOthers = false
	if err := srv.fillContestProblems(ctx, srv.queries(), probs, cid, id, flags); err != nil {
		t.Fatalf("填充题目状态失败: %v", err)
	}
	if probs[0].Mark != "Y" || probs[1].Mark != "N" || probs[2].Mark != "" {
		t.Errorf("普通模式标记 = %q/%q/%q, 期望 Y/N/空", probs[0].Mark, probs[1].Mark, probs[2].Mark)
	}
	for i := range probs {
		if probs[i].Masked {
			t.Errorf("第 %d 题不应打码（非 NOIP、非封榜）", i)
		}
	}
}

// TestCountUserContestProblemACMatchesCheckAC 核对三个计数的过滤条件。
//
// 直接对着 check_ac() 的三条 SQL 的语义打桩验证，
// 重点是"AC 计数不过滤 problem_id"这条反直觉的规则。
func TestCountUserContestProblemACMatchesCheckAC(t *testing.T) {
	srv := newPageDB(t)
	db := srv.db
	ctx := context.Background()

	const (
		cid = int32(90211)
		uid = "cprobcount1"
	)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM solution WHERE contest_id = ?", cid)
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE user_id = ?", uid)
	})

	if _, err := db.ExecContext(ctx, `INSERT INTO users
		(user_id, email, ip, accesstime, password, reg_time, nick, school,
		 group_name, defunct, expiry_date)
		VALUES (?, 'k@test.local', '127.0.0.1', NOW(), 'x', NOW(), 'k', 'S', 'g', 'N', '2099-01-01')
		ON DUPLICATE KEY UPDATE defunct = 'N'`, uid); err != nil {
		t.Fatalf("建用户失败: %v", err)
	}

	// num=0 三条：AC(problem_id=100)、错解(problem_id=101)、练习提交(problem_id=0, AC)
	// 按 check_ac：SubTotal=2（过滤 problem_id=0）、ACCount=2（**不过滤** problem_id，
	// 所以那条练习提交也算 AC）、BadCount=1
	for _, row := range []struct {
		pid, result int
	}{
		{100, 4}, {101, 5}, {0, 4},
	} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO solution (user_id, problem_id, result, num, contest_id, time) VALUES (?, ?, ?, 0, ?, UNIX_TIMESTAMP())",
			uid, row.pid, row.result, cid); err != nil {
			t.Fatalf("写提交失败: %v", err)
		}
	}

	got, err := srv.queries().CountUserContestProblemAC(ctx, uid, cid, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.SubTotal != 2 || got.ACCount != 2 || got.BadCount != 1 {
		t.Errorf("计数 = %+v, 期望 SubTotal=2 ACCount=2 BadCount=1", got)
	}
}

// nullInt32 造一个带 Valid 标记的 sql.NullInt32。
func nullInt32(v int32) (n sql.NullInt32) {
	n.Int32, n.Valid = v, true
	return n
}

// trunc 截断长文本，便于断言失败时展示。
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// 编译期断言：确保 dbgen.Querier 的新方法都已在测试替身中实现。
var _ dbgen.Querier = (*fakeQuerier)(nil)
