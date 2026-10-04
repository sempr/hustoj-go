package web

// 本文件验证绞杀迁移真正要守住的东西：
// 同一数据库上，Go 生成的 SQL 与 phpweb/problemset.php 拼出的 SQL
// 返回**完全相同的行与顺序**。
//
// 为什么不用 sqlmock：mock 只能证明"我们调用了自己写死的字符串"，
// 而切流事故的根因恰恰是 SQL 语义不等价（漏掉 problem_id > 0、LIKE 参数
// 顺序错、比赛子查询的 JOIN 条件差一列）。只有对着真实 MariaDB 跑才能发现。
//
// 已知盲区：真实库当前没有任何 defunct='Y' 的题目，problem_id 也全为正数，
// 所以 defunct='N' 与 problem_id > 0 这两个谓词对返回行没有任何影响。
// 已实测确认，给成员搜索查询多加一个 problem_id > 0，本文件的比对依旧全绿。
// 这两类漂移由 pkg/dbgen/problem_predicates_test.go 用 SQL 文本断言兜住。
// 将来题库真的出现已删除题目或非正 id，这里会自然多一层覆盖。
//
// 默认跳过，需显式打开：
//
//	HUSTOJ_EQUIV_TEST=1 HUSTOJ_EQUIV_DSN='user:pass@tcp(127.0.0.1:3306)/jol?parseTime=true&loc=Local&time_zone=%27%2B08%3A00%27' \
//	  go test ./internal/web/ -run Equiv -v
//
// time_zone 必须与进程 TZ 一致（生产由 repository.DSN 自动带上）：缺了它，
// SQL 的 NOW() 用服务器默认时区而 Go 用进程时区，登录限流、提交冷却期
// 这类按时间窗口的断言会误报失败。
//
// 需要 php 在 PATH 中，且需在仓库内运行（探针要借 phpweb/include/db_info.inc.php 取连接）。

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// equivDSN 从环境取连接串，避免把口令写进测试文件。
func equivDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("HUSTOJ_EQUIV_DSN")
	if dsn == "" {
		t.Skip("未设置 HUSTOJ_EQUIV_DSN，跳过与 PHP 的等价性验证")
	}
	return dsn
}

func equivDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", equivDSN(t))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}
	return db
}

// ── PHP 侧：problemset.php 的逐行转写 ──────────────────────────────────
//
// 这里**故意**不用 Go 重写一遍 PHP 的字符串拼接，而是让 php 解释器执行
// 与 problemset.php 结构一致的代码。否则 PHP 源码一旦改动，这份"权威实现"
// 会悄悄过期，等价性测试就退化成自说自话。
//
// 对应 problemset.php：66-93 行（分页/搜索/list 分支）、120-133 行（三种模式）。
// 未覆盖 `?list=a,b,c` 分支：它只在 SA 模式下使用，不在 Go 首批路由范围内。

const phpEquivTemplate = `<?php
// 探针写在 phpweb/include/ 下，__DIR__ 正是 include 目录，
// 可以直接用 db_info.inc.php 拿到与线上一致的连接与编码设置。
define('OJ_NAME', 'hustoj');
require __DIR__ . '/db_info.inc.php';

$mode  = %MODE%;
$page  = %PAGE%;
$_GET['search'] = %SEARCH%;

$page_cnt = 50;            // problemset.php:66
$postfix  = "";
$filter_sql = "";          // problemset.php:69
$limit_sql  = "";          // problemset.php:70
$order_by   = " order by problem_id ";   // problemset.php:71
$args = array();

if (isset($_GET['search']) && trim($_GET['search']) != "") {   // problemset.php:72
    $search = "%" . ($_GET['search']) . "%";                    // problemset.php:73
    $filter_sql = " ( title like ? or source like ?)";           // problemset.php:74
    $limit_sql = " LIMIT " . ($page - 1) * $page_cnt . "," . $page_cnt;
    $postfix = "&search=" . urlencode($_GET['search']);
} else {                                                        // problemset.php:91
    $filter_sql = " problem_id > 0";                            // problemset.php:92
    $limit_sql = " LIMIT " . ($page - 1) * $page_cnt . "," . $page_cnt;
}

// 三种模式：problemset.php:120-133
if ($mode === 'admin') {
    // 管理员不加任何过滤
} else if ($mode === 'free') {
    $filter_sql .= " and defunct='N' ";
} else {
    $now = date('Y-m-d H:i', time());
    $filter_sql .= " and ` + "`defunct`" + `='N' AND ` + "`problem_id`" + ` NOT IN (
                    SELECT  ` + "`problem_id`" + `
                        FROM contest c
                        INNER JOIN  ` + "`contest_problem`" + ` cp ON c.` + "`contest_id`" + ` = cp.` + "`contest_id`" + ` " .
        " AND (c.` + "`defunct`" + ` = 'N' AND '$now'<c.` + "`end_time`" + `)" .
    ") ";
}

if (isset($_GET['search']) && trim($_GET['search']) != "") {
    $args = array($search, $search);
}

$sql = "select ` + "`problem_id`" + `,` + "`title`" + `,` + "`source`" + `,` + "`submit`" + `,` + "`accepted`" + `,defunct,coin FROM problem A WHERE $filter_sql $order_by $limit_sql ";

pdo_query("SELECT 1");        // 建立全局 $dbh（pdo.php 是惰性连接）
$st = $dbh->prepare($sql);
$st->execute($args);
$rows = $st->fetchAll(PDO::FETCH_ASSOC);
$out = array();
foreach ($rows as $r) { $out[] = (int)$r['problem_id']; }
echo json_encode($out);
`

// runPHP 在 phpweb/include/ 下落一个临时探针并执行，返回 problem_id 顺序。
func runPHP(t *testing.T, mode string, page int, search string) []int {
	t.Helper()
	phpweb := filepath.Join("..", "..", "phpweb")
	if _, err := os.Stat(filepath.Join(phpweb, "include", "db_info.inc.php")); err != nil {
		t.Skipf("找不到 phpweb/include/db_info.inc.php（需在仓库内运行）")
	}
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("PATH 中没有 php")
	}

	script := strings.ReplaceAll(phpEquivTemplate, "%MODE%", phpQuote(mode))
	script = strings.ReplaceAll(script, "%PAGE%", strconv.Itoa(page))
	script = strings.ReplaceAll(script, "%SEARCH%", phpQuote(search))

	// 探针必须落在 include/ 内，db_info.inc.php 依赖 __DIR__ 定位同目录文件。
	// 文件名带 PID，避免并行测试互相覆盖。
	probe := filepath.Join(phpweb, "include", fmt.Sprintf("equiv_probe_%d.php", os.Getpid()))
	if err := os.WriteFile(probe, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(probe) })

	cmd := exec.Command("php", "-d", "display_errors=1", probe)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("php 执行失败: %v\nphp stderr: %s", err, stderr.String())
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var ids []int
	if err := json.Unmarshal([]byte(trimmed), &ids); err != nil {
		t.Fatalf("解析 php 输出 %q 失败: %v", trimmed, err)
	}
	return ids
}

func phpQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// goIDs 用 Go 侧查询取出 problem_id 顺序。
func goIDs(t *testing.T, q dbgen.Querier, v Viewer, page int, kw string, now time.Time) []int {
	t.Helper()
	got, err := ListProblems(context.Background(), q, v, "", page, kw, now)
	if err != nil {
		t.Fatalf("Go 侧 ListProblems 失败: %v", err)
	}
	ids := make([]int, 0, len(got.Problems))
	for _, p := range got.Problems {
		ids = append(ids, int(p.ProblemID))
	}
	return ids
}

// ── 等价性用例 ────────────────────────────────────────────────────────

// 三种模式 × 搜索/无搜索 × 首页/次页。
// 这些组合是首批路由（/problemset.php）会真实走到的全部路径。
func TestEquivProblemList(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_TEST") != "1" {
		t.Skip("未设置 HUSTOJ_EQUIV_TEST")
	}
	db := equivDB(t)
	q := dbgen.New(db)
	now := time.Now()

	modes := []struct {
		name   string
		mode   string
		viewer Viewer
	}{
		{"管理员", "admin", ViewerAdmin},
		{"自由练习", "free", ViewerFreePractice},
		{"普通用户", "member", ViewerMember},
	}
	searches := []struct {
		name string
		kw   string
	}{
		{"无搜索", ""},
		{"搜索a", "a"},
		{"搜索数字", "5"},
		{"搜索无结果", "zzz不存在zzz"},
	}

	for _, m := range modes {
		for _, s := range searches {
			for _, page := range []int{1, 2} {
				name := fmt.Sprintf("%s/%s/第%d页", m.name, s.name, page)
				t.Run(name, func(t *testing.T) {
					want := runPHP(t, m.mode, page, s.kw)
					got := goIDs(t, q, m.viewer, page, s.kw, now)
					if !reflect.DeepEqual(want, got) {
						t.Errorf("两端结果不一致（PHP %d 行 / Go %d 行）\n  PHP: %v\n  Go : %v",
							len(want), len(got), want, got)
					}
				})
			}
		}
	}
}

// 计数也必须一致：列表一致但总数不一致会让分页导航页数不同。
func TestEquivProblemCount(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_TEST") != "1" {
		t.Skip("未设置 HUSTOJ_EQUIV_TEST")
	}
	db := equivDB(t)
	now := time.Now()

	// PHP 的 count 用的是同一份 $filter_sql，直接在 Go 侧跑同样的过滤条件对拍。
	tests := []struct {
		name   string
		viewer Viewer
		kw     string
	}{
		{"管理员-无搜索", ViewerAdmin, ""},
		{"管理员-搜索", ViewerAdmin, "a"},
		{"自由练习-无搜索", ViewerFreePractice, ""},
		{"自由练习-搜索", ViewerFreePractice, "a"},
		{"普通用户-无搜索", ViewerMember, ""},
		{"普通用户-搜索", ViewerMember, "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := count(context.Background(), dbgen.New(db), tc.viewer, tc.kw, now)
			if err != nil {
				t.Fatal(err)
			}
			// 独立参考值：直接把同样的过滤条件写成一条 SQL 求数。
			var want int64
			var ref string
			switch tc.viewer {
			case ViewerAdmin:
				ref = "SELECT count(1) FROM problem"
			case ViewerFreePractice:
				ref = "SELECT count(1) FROM problem WHERE defunct='N'"
			default:
				ref = "SELECT count(1) FROM problem WHERE defunct='N' AND problem_id NOT IN (" +
					"SELECT cp.problem_id FROM contest c INNER JOIN contest_problem cp ON c.contest_id=cp.contest_id " +
					"WHERE c.defunct='N' AND ? < c.end_time)"
			}
			var err2 error
			if tc.viewer == ViewerMember {
				if tc.kw != "" {
					ref = "SELECT count(1) FROM problem WHERE (title LIKE ? OR source LIKE ?) AND defunct='N' AND problem_id NOT IN (" +
						"SELECT cp.problem_id FROM contest c INNER JOIN contest_problem cp ON c.contest_id=cp.contest_id " +
						"WHERE c.defunct='N' AND ? < c.end_time)"
					err2 = db.QueryRow(ref, "%"+tc.kw+"%", "%"+tc.kw+"%", now).Scan(&want)
				} else {
					ref = "SELECT count(1) FROM problem WHERE problem_id > 0 AND defunct='N' AND problem_id NOT IN (" +
						"SELECT cp.problem_id FROM contest c INNER JOIN contest_problem cp ON c.contest_id=cp.contest_id " +
						"WHERE c.defunct='N' AND ? < c.end_time)"
					err2 = db.QueryRow(ref, now).Scan(&want)
				}
			} else {
				switch {
				case tc.viewer == ViewerAdmin && tc.kw != "":
					err2 = db.QueryRow(ref+" WHERE (title LIKE ? OR source LIKE ?)", "%"+tc.kw+"%", "%"+tc.kw+"%").Scan(&want)
				case tc.viewer == ViewerFreePractice && tc.kw != "":
					err2 = db.QueryRow(ref+" AND (title LIKE ? OR source LIKE ?)", "%"+tc.kw+"%", "%"+tc.kw+"%").Scan(&want)
				case tc.viewer == ViewerFreePractice:
					err2 = db.QueryRow("SELECT count(1) FROM problem WHERE problem_id > 0 AND defunct='N'").Scan(&want)
				default:
					err2 = db.QueryRow("SELECT count(1) FROM problem WHERE problem_id > 0").Scan(&want)
				}
			}
			if err2 != nil {
				t.Fatalf("参考查询失败 (%s): %v", ref, err2)
			}
			if got != want {
				t.Errorf("计数 = %d，参考值 %d\n  参考 SQL: %s", got, want, ref)
			}
		})
	}
}

// 当前用户完成状态：Go 的 CAST(MIN(result)) 必须与 PHP 的 MIN(result) 一致，
// 且 result>=4 的筛选不能被改成 >4（那会把部分分用户误判为未提交）。
func TestEquivUserBestResults(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_TEST") != "1" {
		t.Skip("未设置 HUSTOJ_EQUIV_TEST")
	}
	db := equivDB(t)

	// 找一个有解法记录的用户
	var user string
	err := db.QueryRow("SELECT user_id FROM solution WHERE result>=4 GROUP BY user_id ORDER BY COUNT(*) DESC LIMIT 1").Scan(&user)
	if err != nil {
		t.Skip("solution 表里没有可用用户")
	}

	rows, err := dbgen.New(db).GetUserBestResults(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}

	// 参考：按 PHP 原样执行
	refRows, err := db.Query(
		"SELECT problem_id, MIN(result) FROM solution WHERE user_id=? AND result>=4 GROUP BY problem_id", user)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = refRows.Close() }()
	ref := map[int32]int64{}
	for refRows.Next() {
		var pid int32
		var res int64
		if err := refRows.Scan(&pid, &res); err != nil {
			t.Fatal(err)
		}
		ref[pid] = res
	}
	if len(ref) != len(rows) {
		t.Fatalf("题目数 = %d，参考值 %d", len(rows), len(ref))
	}
	for _, r := range rows {
		if ref[r.ProblemID] != r.Result {
			t.Errorf("题 %d: Go result=%d，参考值 %d", r.ProblemID, r.Result, ref[r.ProblemID])
		}
	}
	t.Logf("用户 %s 有 %d 题提交过", user, len(rows))
}

// ── 详情页等价性 ──────────────────────────────────────────────────────
//
// 详情页的可见性规则与列表页不同（多一条 private='1'），单独对拍。
// 当前题库没有任何进行中或私有比赛，因此成员与自由练习两条查询
// 在现有数据上结果必然相同 —— 这类"数据上看不出差别"的部分由
// pkg/dbgen/problem_predicates_test.go 的谓词断言兜底。

func TestEquivProblemDetail(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_TEST") != "1" {
		t.Skip("未设置 HUSTOJ_EQUIV_TEST")
	}
	db := equivDB(t)
	now := time.Now()
	q := dbgen.New(db)

	// 取全部题目：计数断言必须覆盖整表，只抽前 20 道会让期望值失真。
	ids, err := db.Query("SELECT problem_id FROM problem ORDER BY problem_id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ids.Close() }()
	var problemIDs []int32
	for ids.Next() {
		var id int32
		if err := ids.Scan(&id); err != nil {
			t.Fatal(err)
		}
		problemIDs = append(problemIDs, id)
	}

	// 管理员能看到全部题目（含已删除），另两种模式只看 defunct='N'。
	var adminCount, liveCount int64
	if err := db.QueryRow("SELECT count(1) FROM problem").Scan(&adminCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(1) FROM problem WHERE defunct='N'").Scan(&liveCount); err != nil {
		t.Fatal(err)
	}

	for _, v := range []Viewer{ViewerAdmin, ViewerFreePractice, ViewerMember} {
		t.Run(v.String(), func(t *testing.T) {
			var got int64
			for _, id := range problemIDs {
				if _, err := GetProblem(context.Background(), q, v, id, now); err == nil {
					got++
				} else if !strings.Contains(err.Error(), "no rows") {
					t.Fatalf("题 %d 返回了非预期的错误: %v", id, err)
				}
			}
			want := map[Viewer]int64{
				ViewerAdmin: adminCount, ViewerFreePractice: liveCount, ViewerMember: liveCount,
			}[v]
			if got != want {
				t.Errorf("可见题目数 = %d，期望 %d（管理员可见全部，其余只看未删除）", got, want)
			}
		})
	}
}

// ── 比赛模式等价性 ────────────────────────────────────────────────────
//
// 比赛模式的可见性由**比赛本身**的状态决定（时间窗、private、defunct、
// subnet），与练习模式完全不同。生产库只有 1 个比赛（cid=1000，公开、
// 已结束、defunct=N、subnet 空），因此这里对拍覆盖：管理员/匿名两种
// 身份下能取到哪些题、取题键是否按 num、以及已结束公开比赛的可见性。
// private/未开始/锁榜这些"数据上看不出差别"的分支由单元测试与
// pkg/dbgen 的谓词断言兜底。

func TestEquivProblemContest(t *testing.T) {
	if os.Getenv("HUSTOJ_EQUIV_TEST") != "1" {
		t.Skip("未设置 HUSTOJ_EQUIV_TEST")
	}
	db := equivDB(t)
	now := time.Now()
	q := dbgen.New(db)

	// PHP 侧权威实现：problem.php:95-97 的取题逻辑
	phpContestProblems := func(t *testing.T, cid int32) map[int32]int32 {
		t.Helper()
		rows, err := db.Query(
			"SELECT num, problem_id FROM contest_problem WHERE contest_id = ? ORDER BY num", cid)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		m := map[int32]int32{}
		for rows.Next() {
			var num, pid int32
			if err := rows.Scan(&num, &pid); err != nil {
				t.Fatal(err)
			}
			m[num] = pid
		}
		return m
	}

	// 线上唯一比赛：公开、已结束、defunct=N → 匿名也应可访问
	var cnt int
	if err := db.QueryRow("SELECT count(*) FROM contest WHERE defunct='N'").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt == 0 {
		t.Skip("库里没有未关闭的比赛，无法对拍")
	}

	var cid int32
	if err := db.QueryRow("SELECT contest_id FROM contest WHERE defunct='N' LIMIT 1").Scan(&cid); err != nil {
		t.Fatal(err)
	}
	want := phpContestProblems(t, cid)

	// 匿名：已结束的公开比赛可访问，且每个 num 取到的题必须一致
	for num, wantPID := range want {
		gotPID, err := q.GetContestProblemID(context.Background(), dbgen.GetContestProblemIDParams{
			ContestID: sql.NullInt32{Int32: cid, Valid: true}, Num: num,
		})
		if err != nil {
			t.Errorf("cid=%d num=%d 取题失败: %v", cid, num, err)
			continue
		}
		if gotPID != wantPID {
			t.Errorf("cid=%d num=%d → problem_id=%d，PHP 侧期望 %d", cid, num, gotPID, wantPID)
		}
	}

	// 比赛可见性：匿名访问已结束的公开比赛应放行
	contest, err := q.GetContestByID(context.Background(), cid)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := EvaluateContestAccess(context.Background(), q, contest, Anonymous(), now, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !acc.OK() {
		t.Errorf("匿名访问已结束的公开比赛应放行，实际 Deny=%v", acc.Deny)
	}
	if !acc.IsOver {
		t.Errorf("已结束的比赛 IsOver 应为真")
	}

	// 比赛里的题必须能按无过滤语义取回（defunct 题也要能打开）
	for _, wantPID := range want {
		if _, err := q.GetProblemByID(context.Background(), wantPID); err != nil {
			t.Errorf("比赛题目 %d 应可按无过滤语义取回: %v", wantPID, err)
		}
	}
}
