package dbgen

// 本文件补上等价性测试的一个盲区。
//
// internal/web 的等价性测试对着真实库比对 Go 与 PHP 的返回行，但真实库
// 当前没有任何 defunct='Y' 的题目，problem_id 也全为正数，于是
// `defunct='N'` 和 `problem_id > 0` 这两个谓词对结果没有任何影响 ——
// 就算写错、写漏，实测比对依然是绿的。
//
// 已实测确认：给成员搜索查询多加一个 problem_id > 0，等价性测试毫无反应。
//
// 这里的做法是直接钉住生成出来的 SQL 文本里"应含/不应含"哪些谓词。
// 它是白盒断言，但正好覆盖数据比对看不见的那一类漂移，且不受题库内容影响。

import (
	"strings"
	"testing"
)

// want / unwanted：谓词在 SQL 中必须出现 / 必须不出现。
// 只断言关键子串，不断言完整 SQL —— 那样每次微调格式都要改测试，没意义。
type predicateCase struct {
	constant string
	// must 全部必须出现（大小写不敏感）。
	must []string
	// mustNot 全部不能出现。
	mustNot []string
	// noDefunctFilter 表示不得出现 defunct 的**过滤条件**。
	// 单独设字段而不是塞进 mustNot：defunct 同时是被 select 的列，
	// 用 mustNot 断言 "defunct" 会把正常查询也判为违规。
	noDefunctFilter bool
	// why 说明这条约束对应 PHP 的哪行行为，改 SQL 时先读这里。
	why string
}

func TestListQueryPredicates(t *testing.T) {
	cases := []predicateCase{
		// ── 管理员：不看 defunct，也不排除比赛题 ──
		{constant: listProblemsForAdmin,
			must: []string{"problem_id > 0"},
			why:  "problemset.php:92 的无搜索分支统一加了 problem_id > 0，管理员分支（120 行）不追加任何过滤"},
		{constant: countProblemsForAdmin,
			must: []string{"problem_id > 0"}, why: "同上，计数查询共用同一份 filter_sql"},
		{constant: searchProblemsForAdmin,
			mustNot:         []string{"problem_id > 0"},
			noDefunctFilter: true,
			why:             "带 search 时 problemset.php:74 把 filter_sql 整体替换掉，不再含 problem_id > 0；管理员分支不加 defunct 过滤"},
		{constant: countSearchProblemsForAdmin,
			mustNot: []string{"problem_id > 0"}, noDefunctFilter: true, why: "同上"},

		// ── 自由练习：只看 defunct='N'，不排除比赛题 ──
		{constant: listProblemsFreePractice,
			must:    []string{"problem_id > 0", "defunct = 'N'"},
			mustNot: []string{"contest_problem"},
			why:     "problemset.php:122-123 只追加 defunct='N'；自由练习**不**排除比赛进行中的题目"},
		{constant: countProblemsFreePractice,
			must: []string{"problem_id > 0", "defunct = 'N'"}, mustNot: []string{"contest_problem"}, why: "同上"},
		{constant: searchProblemsFreePractice,
			must: []string{"defunct = 'N'"}, mustNot: []string{"problem_id > 0", "contest_problem"}, why: "同上，搜索分支不含 problem_id > 0"},
		{constant: countSearchProblemsFreePractice,
			must: []string{"defunct = 'N'"}, mustNot: []string{"problem_id > 0", "contest_problem"}, why: "同上"},

		// ── 普通用户：defunct='N' 且排除比赛进行中的题目 ──
		{constant: listProblemsForMember,
			must: []string{"problem_id > 0", "defunct = 'N'", "NOT IN", "contest_problem", "end_time"},
			why: "problemset.php:124-132：defunct='N' 且 problem_id NOT IN (比赛进行中的题目)；" +
				"子查询的 JOIN 与 now < end_time 比较条件少一列就会多/少题"},
		{constant: countProblemsForMember,
			must: []string{"problem_id > 0", "defunct = 'N'", "NOT IN", "contest_problem", "end_time"}, why: "同上"},
		{constant: searchProblemsForMember,
			must:    []string{"defunct = 'N'", "NOT IN", "contest_problem", "end_time"},
			mustNot: []string{"problem_id > 0"},
			why:     "同上；搜索分支不含 problem_id > 0"},
		{constant: countSearchProblemsForMember,
			must:    []string{"defunct = 'N'", "NOT IN", "contest_problem", "end_time"},
			mustNot: []string{"problem_id > 0"},
			why:     "同上"},

		// ── 完成状态 ──
		{constant: getUserBestResults,
			must:    []string{"MIN(result)", "result >= 4", "GROUP BY problem_id"},
			mustNot: []string{"MAX(result)"},
			why: "problemset.php:106：MIN(result) 取最好成绩；result>=4 才是「提交过」（含部分分）。" +
				"改成 MAX 会把 AC 用户判成未通过，改成 >4 会把部分分用户判成未提交"},
	}

	for _, tc := range cases {
		name := firstLine(tc.constant)
		t.Run(name, func(t *testing.T) {
			lower := strings.ToLower(tc.constant)
			for _, want := range tc.must {
				if !strings.Contains(lower, strings.ToLower(want)) {
					t.Errorf("SQL 缺少 %q\n约束来源: %s\nSQL:\n%s", want, tc.why, tc.constant)
				}
			}
			if tc.noDefunctFilter && hasDefunctPredicate(tc.constant) {
				t.Errorf("SQL 不应按 defunct 过滤\n约束来源: %s\nSQL:\n%s", tc.why, tc.constant)
			}
			for _, bad := range tc.mustNot {
				if strings.Contains(lower, strings.ToLower(bad)) {
					t.Errorf("SQL 不应包含 %q\n约束来源: %s\nSQL:\n%s", bad, tc.why, tc.constant)
				}
			}
		})
	}
}

// 详情页不过滤 defunct：problem.php 对已删除题目渲染提示页，由路由/模板层决定，
// 查询层一旦过滤，两层行为就分叉了。
func TestGetProblemDoesNotFilterDefunct(t *testing.T) {
	if hasDefunctPredicate(getProblemByID) {
		t.Errorf("详情查询不应按 defunct 过滤：\n%s", getProblemByID)
	}
}

// 详情必须取齐 problem.php 展示所需的列，漏列会让页面显示空值而不报错。
func TestGetProblemSelectsNeededColumns(t *testing.T) {
	for _, col := range []string{
		"description", "input", "output", "sample_input", "sample_output",
		"spj", "hint", "source", "in_date", "time_limit", "memory_limit",
		"defunct", "coin", "submit", "accepted", "solved", "remote_oj", "remote_id",
	} {
		if !strings.Contains(strings.ToLower(getProblemByID), col) {
			t.Errorf("详情查询缺少列 %s", col)
		}
	}
}

// MIN(result) 之前被 codegen 成 interface{}（smallint 的 MIN 返回类型不定），
// CAST 成 SIGNED 才有 int64 可比较。这条防的是 codegen 结果退化。
func TestBestResultIsTypedNotInterface(t *testing.T) {
	var _ int64 = GetUserBestResultsRow{}.Result
}

// hasDefunctPredicate 判断是否存在 defunct 的过滤条件（而不是选中 defunct 列）。
func hasDefunctPredicate(sql string) bool {
	l := strings.ToLower(sql)
	for _, pat := range []string{"defunct =", "defunct='", "defunct ='", "`defunct`"} {
		if strings.Contains(l, pat) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// ── 详情页三模式的可见性 ──────────────────────────────────────────────
//
// 详情页的过滤比列表页多一条 private='1'（problem.php:44）。
// 漏掉它意味着学生改 URL 就能提前看到私有比赛/考试题，是安全问题而非显示问题。

func TestProblemDetailVisibilityPredicates(t *testing.T) {
	// 管理员：不过滤，能看到已删除题目（problem.php:35）
	if hasDefunctPredicate(getProblemByID) {
		t.Errorf("管理员详情查询不应按 defunct 过滤:\n%s", getProblemByID)
	}

	// 自由练习：只要求 defunct='N'，但不排除比赛题（problem.php:38）
	free := strings.ToLower(getProblemFreePractice)
	if !strings.Contains(free, "defunct = 'n'") {
		t.Errorf("自由练习详情查询缺少 defunct='N':\n%s", getProblemFreePractice)
	}
	for _, forbidden := range []string{"not exists", "contest_problem", "private"} {
		if strings.Contains(free, forbidden) {
			t.Errorf("自由练习详情查询不应含 %q（自由练习**不**排除比赛题）:\n%s",
				forbidden, getProblemFreePractice)
		}
	}

	// 成员：defunct='N' 且不被进行中/私有比赛引用
	member := strings.ToLower(getProblemForMember)
	for _, want := range []string{"defunct = 'n'", "not exists", "contest_problem", "end_time", "private"} {
		if !strings.Contains(member, want) {
			t.Errorf("成员详情查询缺少 %q:\n%s", want, getProblemForMember)
		}
	}
	// private 必须是数值比较：列是 tinyint，PHP 写 '1' 靠 MariaDB 隐式转换。
	// 改成 private='1' 虽然结果相同，但会让优化器选择不同索引。
	if strings.Contains(member, "private = '1'") {
		t.Errorf("private 应用于数值比较（= 1），保持与列类型一致:\n%s", getProblemForMember)
	}
}

// 详情三模式都取齐展示所需列，漏列不会报错只会显示空值。
func TestDetailQueriesSelectSameColumns(t *testing.T) {
	cols := []string{
		"problem_id", "title", "description", "input", "output",
		"sample_input", "sample_output", "spj", "hint", "source", "in_date",
		"time_limit", "memory_limit", "defunct", "coin",
		"accepted", "submit", "solved", "remote_oj", "remote_id",
	}
	for name, sql := range map[string]string{
		"GetProblemByID":         getProblemByID,
		"GetProblemFreePractice": getProblemFreePractice,
		"GetProblemForMember":    getProblemForMember,
	} {
		lower := strings.ToLower(sql)
		for _, c := range cols {
			if !strings.Contains(lower, c) {
				t.Errorf("%s 缺少列 %s", name, c)
			}
		}
	}
}

// ── 比赛模式查询的结构守卫 ────────────────────────────────────────────

// GetContestByID 必须不加过滤：PHP 的 contest-check.php:22 做的是
// 无条件 SELECT *，再在 PHP 里逐条判断。把条件塞进 SQL 会让
// "管理员绕过检查"的顺序失效。
func TestGetContestByIDUnfiltered(t *testing.T) {
	lower := strings.ToLower(getContestByID)
	// 只检查 WHERE 子句：private/defunct 作为 SELECT 的列出现是正常的，
	// 出现在过滤条件里才是把判定塞进了 SQL。
	if i := strings.Index(lower, "where"); i >= 0 {
		where := lower[i:]
		if strings.Contains(where, "defunct") {
			t.Errorf("比赛查询不应按 defunct 过滤（PHP 在应用层判定）:\n%s", getContestByID)
		}
		if strings.Contains(where, "private") || strings.Contains(where, "start_time") {
			t.Errorf("比赛查询不应按 private/start_time 过滤:\n%s", getContestByID)
		}
	}
	for _, want := range []string{"start_time", "end_time", "langmask", "subnet", "description"} {
		if !strings.Contains(lower, want) {
			t.Errorf("比赛查询缺少列 %s", want)
		}
	}
}

// GetContestProblemID 的取题键必须是 num，不是 problem_id。
// 比赛排名页展示的是 1-based 编号，写错键会让选手看到别的题。
func TestGetContestProblemKeyedOnNum(t *testing.T) {
	lower := strings.ToLower(getContestProblemID)
	if !strings.Contains(lower, "num =") && !strings.Contains(lower, "num =?") && !strings.Contains(lower, "num = ?") {
		t.Errorf("比赛取题必须按 num 过滤:\n%s", getContestProblemID)
	}
	if !strings.Contains(lower, "contest_problem") {
		t.Errorf("比赛取题必须查 contest_problem:\n%s", getContestProblemID)
	}
}

// HasContestPrivilege 必须查 privilege 表的 c<cid> 授权，
// 对应 contest-check.php:62-70。
func TestHasContestPrivilegeQuery(t *testing.T) {
	lower := strings.ToLower(hasContestPrivilege)
	for _, want := range []string{"privilege", "user_id", "rightstr"} {
		if !strings.Contains(lower, want) {
			t.Errorf("授权查询缺少 %s:\n%s", want, hasContestPrivilege)
		}
	}
}

// IsProblemLocked 必须限定比赛**正在进行**（start_time < now < end_time），
// 漏掉时间窗会把已结束的 NOIP 比赛也当成锁榜，永久隐藏统计数字。
func TestIsProblemLockedTimeWindow(t *testing.T) {
	lower := strings.ToLower(isProblemLocked)
	for _, want := range []string{"start_time <", "end_time >", "contest_type", "title like"} {
		if !strings.Contains(lower, want) {
			t.Errorf("锁榜查询缺少 %q:\n%s", want, isProblemLocked)
		}
	}
}
