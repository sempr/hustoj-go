package web

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// fakeQuerier 实现 dbgen.FullQuerier，用于在不连库的前提下验证
// "选了哪条查询、传了什么参数、结果怎么组装"。
//
// 之所以不用 sqlmock：那类库只能断言 SQL 字符串，而本层真正要守的是
// 访问者模式 → 查询的映射关系，以及分页/AC 标记的组装逻辑。
type fakeQuerier struct {
	// called 记录被调用的方法名，按顺序。
	called []string
	// total 是计数查询返回值。
	total int64
	// rows 是列表查询返回值（成员/自由练习/管理员共用同一份，够测组装逻辑）。
	rows []fakeRow
	// best 是用户完成状态。
	best []dbgen.GetUserBestResultsRow
	// detail 是 GetProblemByID 的返回值。
	detail dbgen.Problem
	// contest 是 GetContestByID 的返回值。
	contest dbgen.GetContestByIDRow
	// contestForRank 是 GetContestForRank 的返回值。
	contestForRank dbgen.GetContestForRankRow
	// contestProblems 是 GetContestProblems 的返回值。
	contestProblems []dbgen.GetContestProblemsRow
	// contestProblemsForRank 是 GetContestProblemsForRank 的返回值。
	contestProblemsForRank []dbgen.GetContestProblemsForRankRow
	// rankList 是 GetRankList 的返回值。
	rankList []dbgen.GetRankListRow
	// statusCount 是 GetStatusCount 的返回值。
	statusCount int64
	// statusCountContest 是 GetStatusCountContest 的返回值。
	statusCountContest int64
	// statusList 是 GetStatusList 的返回值。
	statusList []dbgen.GetStatusListRow
	// statusListContest 是 GetStatusListContest 的返回值。
	statusListContest []dbgen.GetStatusListContestRow
	// contestList 是 ListContests 的返回值。
	contestList []dbgen.ListContestsRow
	// privileged / noipLocked 是比赛授权与锁榜判定的返回值。
	privileged bool
	noipLocked bool

	// ── 提交链路的可注入返回值 ────────────────────────────────────────
	// pendingJudges 是待判队列长度，超过阈值会触发提交验证码。
	pendingJudges int64
	// submitProblem / contestSubmitPID 是两种取题路径的结果。
	submitProblem    dbgen.GetSubmitProblemRow
	contestSubmitPID int32
	contestForSubmit dbgen.GetContestForSubmitRow
	// contestInvites 是私有比赛受邀人数。
	contestInvites int64
	// spj / nick / remoteOJ 分别对应 GetProblemSPJ / GetUserNick /
	// GetProblemRemoteOJ。默认值由各测试按需覆盖。
	spj      string
	nick     string
	remoteOJ sql.NullString
	// lastRecentSubmit 非 0 表示冷却期内已有提交。
	lastRecentSubmit uint32
	// contestLocked 对应 IsContestLocked（128 延时 / 16 NOIP）。
	contestLocked bool
	// userAccepted 是投毒判定用的 AC 计数。
	userAccepted int64
	// problemAccepted / problemSubmits 供降级后重算计数。
	problemAccepted int64
	problemSubmits  int64
	// err 注入到所有查询，用于验证错误处理路径。
	err error
	// bestUser 记录 GetUserBestResults 收到的 user_id。
	bestUser string
	// args 记录最近一次列表查询的参数，用于断言分页偏移。
	lastLimit, lastOffset int32
	lastNow               sql.NullTime
	lastTitleLike         string
	lastSourceLike        string
	// ── 档案页 / 比赛列表相关 ──
	// noipContests 是被排除的进行中 NOIP 比赛。
	noipContests []int32
	// userProfile 是 GetUserProfile 的返回值。
	userProfile    dbgen.User
	userAC         int64
	userSubmit     int64
	usersAhead     int64
	loginLog       []dbgen.UserLoginLogRow
	resultStats    []dbgen.UserResultStatRow
	contestStats   []dbgen.UserContestStatRow
	acProblems     []dbgen.UserProblemCountRow
	badProblems    []dbgen.UserProblemCountRow
	subChart       []dbgen.UserSubChartRow
	viewerAC       []int32
	contestTotal   int64
	contestSet     []dbgen.ContestSetRow
	myContests     []int32
	vipContests    []int32
	problemAC      dbgen.ContestProblemACCounts
	usedInUpcoming bool

	// ── 比赛统计页（conteststatistics.php）─────────────────────────
	contestProblemIDs    []dbgen.ContestProblemID
	contestSolutionStats []dbgen.ContestSolutionStat
	dailySubmits         []dbgen.ContestDailyCount
	dailyAC              []dbgen.ContestDailyCount
	usedInOngoing        bool

	// ── 全局排名页（ranklist.php）─────────────────────────────────
	runningNOIP       int64
	ranklistTotal     int64
	ranklistRows      []dbgen.RanklistUserRow
	ranklistScoped    []dbgen.RanklistUserRow
	ranklistScopedTot int64
	// lastSolutionID 按日期给出 LastSolutionIDBefore 的返回值；
	// 未命中的日期回落到 lastSolutionDefault。
	lastSolutionID      map[string]int64
	lastSolutionDefault int64

	// ── 单题提交状态页（problemstatus.php）─────────────────────────
	psNOIP       int64
	psUsers      int64
	psACUsers    int64
	psResults    []dbgen.GetProblemResultStatsRow
	psInRunning  bool
	psSolutions  []dbgen.ProblemBestACSolRow
	psSource     sql.NullString
	psProblemIDs []int32

	// ── 源代码查看页（showsource.php）────────────────────────────
	ssSolution      dbgen.GetSolutionByIDRow
	ssContestWindow dbgen.GetContestWindowRow
	ssLockedCID     int32
	ssSource        string

	// ── AJAX（status-ajax.php / problem-ajax.php）─────────────────
	saSolution         dbgen.Solution
	saContestTitle     dbgen.GetContestTitleTypeRow
	saCompileError     sql.NullString
	saCompileErr       error
	saRuntimeError     sql.NullString
	saRuntimeErr       error
	paCreator          string
	paRightstr         string
	newsList           []dbgen.GetNewsListRow
	monthStars         []dbgen.GetMonthStarRankRow
	recentProblems     []dbgen.GetRecentProblemsRow
	recentContests     []dbgen.GetRecentContestsRow
	chartDataAll       []dbgen.GetChartDataAllRow
	chartDataAC        []dbgen.GetChartDataACRow
	submittedProblems  []dbgen.GetUserSubmittedProblemsRow
	unsolvedProblems   []dbgen.GetUserUnsolvedProblemsRow
	activeContests     []dbgen.GetActiveContestsRow
	onlineUsers        []dbgen.GetOnlineUsersRow
	recentContestsList []dbgen.GetRecentContestsListRow
	acCodeByUser       []dbgen.GetACCodeByUserRow
	contestCode        []dbgen.GetContestCodeRow
	contestInfo        dbgen.GetContestInfoRow
	contestSubmissions []dbgen.GetContestSubmissionsRow
	firstBlood         []dbgen.GetFirstBloodRow
	contestTeams       []dbgen.GetContestTeamsRow
	contestProblemMap  []dbgen.GetContestProblemMapRow
	newsByID           []dbgen.GetNewsByIDRow
	userByID           dbgen.GetUserByIDRow
	userEmail          sql.NullString
}

type fakeRow struct {
	ID       int32
	Title    string
	Source   sql.NullString
	Submit   sql.NullInt32
	Accepted sql.NullInt32
	Defunct  string
	Coin     uint32
}

var _ dbgen.FullQuerier = (*fakeQuerier)(nil)

func (f *fakeQuerier) note(name string) { f.called = append(f.called, name) }

// fail 返回注入的错误（若有），供各方法统一短路。
func (f *fakeQuerier) fail() error { return f.err }

func (f *fakeQuerier) CountProblemsForAdmin(context.Context) (int64, error) {
	f.note("CountProblemsForAdmin")
	return f.total, nil
}
func (f *fakeQuerier) CountProblemsForMember(_ context.Context, now sql.NullTime) (int64, error) {
	f.note("CountProblemsForMember")
	f.lastNow = now
	return f.total, f.fail()
}
func (f *fakeQuerier) CountProblemsFreePractice(context.Context) (int64, error) {
	f.note("CountProblemsFreePractice")
	return f.total, nil
}
func (f *fakeQuerier) CountSearchProblemsForAdmin(_ context.Context, a dbgen.CountSearchProblemsForAdminParams) (int64, error) {
	f.note("CountSearchProblemsForAdmin")
	f.lastTitleLike, f.lastSourceLike = a.TitleLike, a.SourceLike.String
	return f.total, nil
}
func (f *fakeQuerier) CountSearchProblemsForMember(_ context.Context, a dbgen.CountSearchProblemsForMemberParams) (int64, error) {
	f.note("CountSearchProblemsForMember")
	f.lastTitleLike, f.lastSourceLike, f.lastNow = a.TitleLike, a.SourceLike.String, a.Now
	return f.total, nil
}
func (f *fakeQuerier) CountSearchProblemsFreePractice(_ context.Context, a dbgen.CountSearchProblemsFreePracticeParams) (int64, error) {
	f.note("CountSearchProblemsFreePractice")
	f.lastTitleLike, f.lastSourceLike = a.TitleLike, a.SourceLike.String
	return f.total, nil
}

func (f *fakeQuerier) GetProblemByID(_ context.Context, id int32) (dbgen.Problem, error) {
	f.note("GetProblemByID")
	if err := f.fail(); err != nil {
		return dbgen.Problem{}, err
	}
	d := f.detail
	// 未设置 detail 时给一份可用的默认值，避免各用例都要重复填
	if d.ProblemID == 0 {
		d.ProblemID, d.Title, d.Defunct, d.Coin = id, "T", "N", 1
	}
	return d, nil
}

// ── 比赛模式 ──────────────────────────────────────────────────────────

// contest 是 GetContestByID 的返回值；未设置时给一份默认值。
func (f *fakeQuerier) defaultContest(cid int32) dbgen.GetContestByIDRow {
	c := f.contest
	if c.ContestID == 0 {
		c.ContestID = cid
		c.Title = sql.NullString{String: "T", Valid: true}
		c.Defunct = "N"
		c.Langmask = 16580496
		// 未设置时间窗时给一个已开始且未结束的窗口，避免每个用例都要重复填
		now := time.Now()
		c.StartTime = sql.NullTime{Time: now.Add(-time.Hour), Valid: true}
		c.EndTime = sql.NullTime{Time: now.Add(time.Hour), Valid: true}
	}
	return c
}

func (f *fakeQuerier) GetContestByID(_ context.Context, cid int32) (dbgen.GetContestByIDRow, error) {
	f.note("GetContestByID")
	if err := f.fail(); err != nil {
		return dbgen.GetContestByIDRow{}, err
	}
	return f.defaultContest(cid), nil
}

func (f *fakeQuerier) GetContestForRank(_ context.Context, cid int32) (dbgen.GetContestForRankRow, error) {
	f.note("GetContestForRank")
	if err := f.fail(); err != nil {
		return dbgen.GetContestForRankRow{}, err
	}
	return f.contestForRank, nil
}

func (f *fakeQuerier) GetContestProblems(_ context.Context, _ sql.NullInt32) ([]dbgen.GetContestProblemsRow, error) {
	f.note("GetContestProblems")
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.contestProblems, nil
}

func (f *fakeQuerier) GetContestProblemsForRank(_ context.Context, cid sql.NullInt32) ([]dbgen.GetContestProblemsForRankRow, error) {
	f.note("GetContestProblemsForRank")
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.contestProblemsForRank, nil
}

func (f *fakeQuerier) GetRankList(_ context.Context, cid sql.NullInt32) ([]dbgen.GetRankListRow, error) {
	f.note("GetRankList")
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.rankList, nil
}

func (f *fakeQuerier) ListContests(_ context.Context) ([]dbgen.ListContestsRow, error) {
	f.note("ListContests")
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.contestList, nil
}

func (f *fakeQuerier) GetStatusCount(_ context.Context) (int64, error) {
	f.note("GetStatusCount")
	if err := f.fail(); err != nil {
		return 0, err
	}
	return f.statusCount, nil
}

func (f *fakeQuerier) GetStatusCountContest(_ context.Context, cid sql.NullInt32) (int64, error) {
	f.note("GetStatusCountContest")
	if err := f.fail(); err != nil {
		return 0, err
	}
	return f.statusCountContest, nil
}

func (f *fakeQuerier) GetStatusList(_ context.Context, arg dbgen.GetStatusListParams) ([]dbgen.GetStatusListRow, error) {
	f.note("GetStatusList")
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.statusList, nil
}

func (f *fakeQuerier) GetStatusListContest(_ context.Context, arg dbgen.GetStatusListContestParams) ([]dbgen.GetStatusListContestRow, error) {
	f.note("GetStatusListContest")
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.statusListContest, nil
}

func (f *fakeQuerier) GetContestProblemID(_ context.Context, a dbgen.GetContestProblemIDParams) (int32, error) {
	f.note("GetContestProblemID")
	if err := f.fail(); err != nil {
		return 0, err
	}
	// 未设置时按 num 造一个稳定映射，够测"取题键是 num 而非 problem_id"
	return a.Num + 1000, nil
}

func (f *fakeQuerier) HasContestPrivilege(_ context.Context, _ dbgen.HasContestPrivilegeParams) (bool, error) {
	f.note("HasContestPrivilege")
	return f.privileged, f.fail()
}

func (f *fakeQuerier) IsProblemLocked(_ context.Context, _ dbgen.IsProblemLockedParams) (bool, error) {
	f.note("IsProblemLocked")
	return f.noipLocked, f.fail()
}

func (f *fakeQuerier) GetUserBestResults(_ context.Context, userID string) ([]dbgen.GetUserBestResultsRow, error) {
	f.note("GetUserBestResults")
	f.bestUser = userID
	return f.best, f.fail()
}

func (f *fakeQuerier) ListProblemsForAdmin(_ context.Context, a dbgen.ListProblemsForAdminParams) ([]dbgen.ListProblemsForAdminRow, error) {
	f.note("ListProblemsForAdmin")
	f.lastLimit, f.lastOffset = a.Limit, a.Offset
	return f.adminRows(), f.fail()
}
func (f *fakeQuerier) ListProblemsForMember(_ context.Context, a dbgen.ListProblemsForMemberParams) ([]dbgen.ListProblemsForMemberRow, error) {
	f.note("ListProblemsForMember")
	f.lastLimit, f.lastOffset, f.lastNow = a.Limit, a.Offset, a.Now
	return f.memberRows(), f.fail()
}
func (f *fakeQuerier) ListProblemsFreePractice(_ context.Context, a dbgen.ListProblemsFreePracticeParams) ([]dbgen.ListProblemsFreePracticeRow, error) {
	f.note("ListProblemsFreePractice")
	f.lastLimit, f.lastOffset = a.Limit, a.Offset
	return f.freeRows(), f.fail()
}
func (f *fakeQuerier) SearchProblemsForAdmin(_ context.Context, a dbgen.SearchProblemsForAdminParams) ([]dbgen.SearchProblemsForAdminRow, error) {
	f.note("SearchProblemsForAdmin")
	f.lastLimit, f.lastOffset = a.Limit, a.Offset
	f.lastTitleLike, f.lastSourceLike = a.TitleLike, a.SourceLike.String
	return f.searchAdminRows(), f.fail()
}
func (f *fakeQuerier) SearchProblemsForMember(_ context.Context, a dbgen.SearchProblemsForMemberParams) ([]dbgen.SearchProblemsForMemberRow, error) {
	f.note("SearchProblemsForMember")
	f.lastLimit, f.lastOffset, f.lastNow = a.Limit, a.Offset, a.Now
	f.lastTitleLike, f.lastSourceLike = a.TitleLike, a.SourceLike.String
	return f.searchMemberRows(), f.fail()
}
func (f *fakeQuerier) SearchProblemsFreePractice(_ context.Context, a dbgen.SearchProblemsFreePracticeParams) ([]dbgen.SearchProblemsFreePracticeRow, error) {
	f.note("SearchProblemsFreePractice")
	f.lastLimit, f.lastOffset = a.Limit, a.Offset
	f.lastTitleLike, f.lastSourceLike = a.TitleLike, a.SourceLike.String
	return f.searchFreeRows(), f.fail()
}

// convertRows 把 fakeRow 转成各查询各自的 sqlc Row 类型。
// 6 条列表查询各有独立 Row 类型，无法互相赋值，只能逐类型转；
// 用泛型把样板压到"每类型一行字面量"。
func convertRows[O any](rs []fakeRow, lit func(fakeRow) O) []O {
	out := make([]O, 0, len(rs))
	for _, r := range rs {
		out = append(out, lit(r))
	}
	return out
}

func (f *fakeQuerier) adminRows() []dbgen.ListProblemsForAdminRow {
	return convertRows(f.rows, func(r fakeRow) dbgen.ListProblemsForAdminRow {
		return dbgen.ListProblemsForAdminRow{ProblemID: r.ID, Title: r.Title, Source: r.Source,
			Submit: r.Submit, Accepted: r.Accepted, Defunct: r.Defunct, Coin: r.Coin}
	})
}
func (f *fakeQuerier) memberRows() []dbgen.ListProblemsForMemberRow {
	return convertRows(f.rows, func(r fakeRow) dbgen.ListProblemsForMemberRow {
		return dbgen.ListProblemsForMemberRow{ProblemID: r.ID, Title: r.Title, Source: r.Source,
			Submit: r.Submit, Accepted: r.Accepted, Defunct: r.Defunct, Coin: r.Coin}
	})
}
func (f *fakeQuerier) freeRows() []dbgen.ListProblemsFreePracticeRow {
	return convertRows(f.rows, func(r fakeRow) dbgen.ListProblemsFreePracticeRow {
		return dbgen.ListProblemsFreePracticeRow{ProblemID: r.ID, Title: r.Title, Source: r.Source,
			Submit: r.Submit, Accepted: r.Accepted, Defunct: r.Defunct, Coin: r.Coin}
	})
}
func (f *fakeQuerier) searchAdminRows() []dbgen.SearchProblemsForAdminRow {
	return convertRows(f.rows, func(r fakeRow) dbgen.SearchProblemsForAdminRow {
		return dbgen.SearchProblemsForAdminRow{ProblemID: r.ID, Title: r.Title, Source: r.Source,
			Submit: r.Submit, Accepted: r.Accepted, Defunct: r.Defunct, Coin: r.Coin}
	})
}
func (f *fakeQuerier) searchMemberRows() []dbgen.SearchProblemsForMemberRow {
	return convertRows(f.rows, func(r fakeRow) dbgen.SearchProblemsForMemberRow {
		return dbgen.SearchProblemsForMemberRow{ProblemID: r.ID, Title: r.Title, Source: r.Source,
			Submit: r.Submit, Accepted: r.Accepted, Defunct: r.Defunct, Coin: r.Coin}
	})
}
func (f *fakeQuerier) searchFreeRows() []dbgen.SearchProblemsFreePracticeRow {
	return convertRows(f.rows, func(r fakeRow) dbgen.SearchProblemsFreePracticeRow {
		return dbgen.SearchProblemsFreePracticeRow{ProblemID: r.ID, Title: r.Title, Source: r.Source,
			Submit: r.Submit, Accepted: r.Accepted, Defunct: r.Defunct, Coin: r.Coin}
	})
}

// ── 访问者模式 → 查询映射 ──────────────────────────────────────────────
// 这组用例是本文件的核心：problemset.php 的可见范围完全由这三条分支决定，
// 映射错了用户在切换到 Go 的瞬间就会看到题目列表变化。

func TestViewerSelectsMatchingQueries(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		viewer  Viewer
		kw      string
		wantSum string
		wantCnt string
	}{
		{"管理员-无搜索", ViewerAdmin, "",
			"ListProblemsForAdmin", "CountProblemsForAdmin"},
		{"管理员-有搜索", ViewerAdmin, "abc",
			"SearchProblemsForAdmin", "CountSearchProblemsForAdmin"},
		{"自由练习-无搜索", ViewerFreePractice, "",
			"ListProblemsFreePractice", "CountProblemsFreePractice"},
		{"自由练习-有搜索", ViewerFreePractice, "abc",
			"SearchProblemsFreePractice", "CountSearchProblemsFreePractice"},
		{"普通用户-无搜索", ViewerMember, "",
			"ListProblemsForMember", "CountProblemsForMember"},
		{"普通用户-有搜索", ViewerMember, "abc",
			"SearchProblemsForMember", "CountSearchProblemsForMember"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeQuerier{total: 100}
			if _, err := ListProblems(context.Background(), f, tc.viewer, "", 1, tc.kw, now); err != nil {
				t.Fatalf("ListProblems: %v", err)
			}
			// 计数先于列表发出（总数决定分页页数），顺序也要对。
			want := []string{tc.wantCnt, tc.wantSum}
			if len(f.called) != 2 || f.called[0] != want[0] || f.called[1] != want[1] {
				t.Errorf("查询序列 = %v，期望 %v", f.called, want)
			}
		})
	}
}

// 自由练习开关切错会导致题目列表可见范围变化，是最容易被忽略的回归。
func TestFreePracticeSwitchChangesVisibility(t *testing.T) {
	now := time.Now()
	member := &fakeQuerier{total: 10}
	if _, err := ListProblems(context.Background(), member, ViewerMember, "", 1, "", now); err != nil {
		t.Fatal(err)
	}
	free := &fakeQuerier{total: 10}
	if _, err := ListProblems(context.Background(), free, ViewerFreePractice, "", 1, "", now); err != nil {
		t.Fatal(err)
	}
	if member.called[0] == free.called[0] {
		t.Errorf("普通用户与自由练习选了同一条查询 %q，开关没起作用", member.called[0])
	}
}

// ── 分页 ──────────────────────────────────────────────────────────────

func TestPaginationMath(t *testing.T) {
	now := time.Now()
	tests := []struct {
		total      int64
		page       int
		wantOffset int32
		wantPages  int64
	}{
		{0, 1, 0, 1},    // 空库也应有 1 页，否则模板渲染出 0 页导航
		{1, 1, 0, 1},    //
		{50, 1, 0, 1},   //
		{51, 2, 50, 2},  //
		{100, 2, 50, 2}, //
		{101, 3, 100, 3},
	}
	for _, tc := range tests {
		f := &fakeQuerier{total: tc.total}
		got, err := ListProblems(context.Background(), f, ViewerMember, "", tc.page, "", now)
		if err != nil {
			t.Fatalf("total=%d page=%d: %v", tc.total, tc.page, err)
		}
		if f.lastOffset != tc.wantOffset {
			t.Errorf("total=%d page=%d: offset = %d，期望 %d", tc.total, tc.page, f.lastOffset, tc.wantOffset)
		}
		if got.TotalPage != tc.wantPages {
			t.Errorf("total=%d page=%d: TotalPage = %d，期望 %d", tc.total, tc.page, got.TotalPage, tc.wantPages)
		}
		if f.lastLimit != pageSize {
			t.Errorf("每页条数 = %d，期望 %d（须与 PHP 的 $page_cnt 一致）", f.lastLimit, pageSize)
		}
	}
}

// page<=0 会被 PHP 的 $page-1 算成负偏移；Go 侧夹到 1，避免生成
// "LIMIT -50 OFFSET -50" 这种语法错误或全表扫描。
func TestPageClampedToOne(t *testing.T) {
	for _, p := range []int{-5, 0, 1} {
		f := &fakeQuerier{total: 10}
		got, err := ListProblems(context.Background(), f, ViewerMember, "", p, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got.Page != 1 {
			t.Errorf("输入 page=%d 时 Page = %d，期望夹到 1", p, got.Page)
		}
		if f.lastOffset != 0 {
			t.Errorf("输入 page=%d 时 offset = %d，期望 0", p, f.lastOffset)
		}
	}
}

// ── 当前用户完成状态 ──────────────────────────────────────────────────

func TestAcceptedMarks(t *testing.T) {
	f := &fakeQuerier{
		total: 3,
		rows: []fakeRow{
			{ID: 1, Title: "AC 题"},
			{ID: 2, Title: "只提交过"},
			{ID: 3, Title: "没提交"},
		},
		best: []dbgen.GetUserBestResultsRow{
			{ProblemID: 1, Result: resultAC}, // 4 = AC
			{ProblemID: 2, Result: 6},        // 部分分：提交过但没 AC
		},
	}
	got, err := ListProblems(context.Background(), f, ViewerMember, "u1", 1, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if f.bestUser != "u1" {
		t.Errorf("GetUserBestResults 收到 user_id=%q，期望 u1", f.bestUser)
	}
	wantSolved := []bool{true, true, false}
	wantAccepted := []bool{true, false, false}
	for i, p := range got.Problems {
		if p.SolvedByUser != wantSolved[i] {
			t.Errorf("题 %d SolvedByUser = %v，期望 %v", p.ProblemID, p.SolvedByUser, wantSolved[i])
		}
		if p.AcceptedByUser != wantAccepted[i] {
			t.Errorf("题 %d AcceptedByUser = %v，期望 %v", p.ProblemID, p.AcceptedByUser, wantAccepted[i])
		}
	}
}

// 未登录时 PHP 根本不发完成状态查询，Go 侧必须同样跳过。
func TestNoUserNoBestResultsQuery(t *testing.T) {
	f := &fakeQuerier{total: 1, rows: []fakeRow{{ID: 1, Title: "x"}}}
	if _, err := ListProblems(context.Background(), f, ViewerMember, "", 1, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.called {
		if c == "GetUserBestResults" {
			t.Error("未登录时不应查询用户完成状态")
		}
	}
}

// AcceptedStat（列）与 AcceptedByUser（当前用户）是两个含义，
// 同名必然互相覆盖 —— 用这个用例守住它们互不干扰。
func TestProblemStatAndUserAcceptAreSeparate(t *testing.T) {
	f := &fakeQuerier{
		total: 1,
		rows:  []fakeRow{{ID: 7, Title: "t", Accepted: sql.NullInt32{Int32: 123, Valid: true}}},
		best:  []dbgen.GetUserBestResultsRow{{ProblemID: 7, Result: resultAC}},
	}
	got, err := ListProblems(context.Background(), f, ViewerMember, "u", 1, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := got.Problems[0]
	if p.AcceptedStat.Int32 != 123 {
		t.Errorf("AcceptedStat = %d，期望 123（全站通过次数）", p.AcceptedStat.Int32)
	}
	if !p.AcceptedByUser {
		t.Error("AcceptedByUser = false，期望 true（当前用户 AC）")
	}
}

// ── 搜索参数 ──────────────────────────────────────────────────────────

// PHP 的 "%kw%" 是裸 LIKE：用户输入的 % 和 _ 就是通配符。
// Go 侧若擅自转义，两端结果会不同，所以这里把行为钉死。
func TestSearchPatternIsRawLike(t *testing.T) {
	if got := SearchPattern("abc"); got != "%abc%" {
		t.Errorf("SearchPattern = %q，期望 %%abc%%", got)
	}
	// 不转义：% 与 _ 原样透传
	if got := SearchPattern("a%b_c"); got != "%a%b_c%" {
		t.Errorf("SearchPattern = %q，期望保持未转义（与 PHP 的裸 like 一致）", got)
	}
}

// title 与 source 必须拿到同一个匹配串，否则 "(title like ? or source like ?)"
// 会出现两臂条件不同，语义与 PHP 不符。
func TestBothLikeArmsGetSamePattern(t *testing.T) {
	f := &fakeQuerier{total: 1}
	if _, err := ListProblems(context.Background(), f, ViewerMember, "", 1, "kw", time.Now()); err != nil {
		t.Fatal(err)
	}
	if f.lastTitleLike != f.lastSourceLike {
		t.Errorf("title 匹配串 %q 与 source 匹配串 %q 不一致", f.lastTitleLike, f.lastSourceLike)
	}
	if f.lastTitleLike != "%kw%" {
		t.Errorf("匹配串 = %q，期望 %%kw%%", f.lastTitleLike)
	}
}

// ── now 传参 ──────────────────────────────────────────────────────────

// 成员模式要把 now 传进去排除比赛题；忘传会让全站题目在比赛期间消失。
func TestMemberModePassesNow(t *testing.T) {
	want := time.Date(2026, 9, 30, 15, 4, 5, 0, time.Local)
	f := &fakeQuerier{total: 1}
	if _, err := ListProblems(context.Background(), f, ViewerMember, "", 1, "", want); err != nil {
		t.Fatal(err)
	}
	if !f.lastNow.Valid {
		t.Fatal("成员模式未传 now")
	}
	if !f.lastNow.Time.Equal(want) {
		t.Errorf("now = %v，期望 %v", f.lastNow.Time, want)
	}
}

// ── 来源标签拆分 ──────────────────────────────────────────────────────

func TestSplitSource(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Codeforces", []string{"Codeforces"}},
		{"A+B 洛谷", []string{"A+B", "洛谷"}},
		{"  前后有空格  ", []string{"前后有空格"}},
		{"多个   连续空格", []string{"多个", "连续空格"}},
	}
	for _, tc := range tests {
		got := SplitSource(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("SplitSource(%q) = %v，期望 %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("SplitSource(%q)[%d] = %q，期望 %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// ── 错误传播 ──────────────────────────────────────────────────────────

var errBoom = errors.New("boom")

type errQuerier struct{ fakeQuerier }

func (e *errQuerier) CountProblemsForMember(context.Context, sql.NullTime) (int64, error) {
	return 0, errBoom
}

func TestListProblemsWrapsCountError(t *testing.T) {
	_, err := ListProblems(context.Background(), &errQuerier{}, ViewerMember, "", 1, "", time.Now())
	if err == nil {
		t.Fatal("计数失败时应返回错误")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("错误未被包装透传: %v", err)
	}
	// 错误信息要带上模式名，否则线上出问题无法判断是哪条分支
	if got := err.Error(); !strings.Contains(got, "member") {
		t.Errorf("错误信息缺少访问者模式: %v", err)
	}
}

func (f *fakeQuerier) GetProblemForMember(_ context.Context, _ dbgen.GetProblemForMemberParams) (dbgen.Problem, error) {
	f.note("GetProblemForMember")
	if err := f.fail(); err != nil {
		return dbgen.Problem{}, err
	}
	return f.defaultProblem(1), nil
}

func (f *fakeQuerier) GetProblemFreePractice(_ context.Context, _ int32) (dbgen.Problem, error) {
	f.note("GetProblemFreePractice")
	if err := f.fail(); err != nil {
		return dbgen.Problem{}, err
	}
	return f.defaultProblem(1), nil
}

// defaultProblem 返回用例可用的详情：未设置时给一份默认值。
func (f *fakeQuerier) defaultProblem(id int32) dbgen.Problem {
	d := f.detail
	if d.ProblemID == 0 {
		d.ProblemID, d.Title, d.Defunct, d.Coin = id, "T", "N", 1
	}
	return d
}

// ── 详情 ──────────────────────────────────────────────────────────────

// 详情页的可见性按三种模式走不同查询。
// 管理员不过滤（能看到已删除题目），成员模式要排除进行中/私有比赛题。
func TestGetProblemSelectsModeQuery(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		viewer Viewer
		want   string
	}{
		{"管理员不过滤", ViewerAdmin, "GetProblemByID"},
		{"自由练习只查 defunct", ViewerFreePractice, "GetProblemFreePractice"},
		{"成员排除比赛题", ViewerMember, "GetProblemForMember"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeQuerier{}
			if _, err := GetProblem(context.Background(), f, tc.viewer, 1, now); err != nil {
				t.Fatal(err)
			}
			if len(f.called) != 1 || f.called[0] != tc.want {
				t.Errorf("调用序列 = %v，期望仅 %s", f.called, tc.want)
			}
		})
	}
}

func TestViewerString(t *testing.T) {
	for v, want := range map[Viewer]string{
		ViewerMember: "member", ViewerFreePractice: "free_practice", ViewerAdmin: "admin",
	} {
		if got := v.String(); got != want {
			t.Errorf("Viewer(%d).String() = %q，期望 %q", v, got, want)
		}
	}
}

// ── 提交相关查询的替身 ──────────────────────────────────────────────
//
// 提交链路的查询在 submit_test.go 里逐条断言，这里只提供最小实现：
// 记录调用名并返回可注入的结果。缺一个方法就会在
// `var _ dbgen.FullQuerier` 处编译失败，所以新增查询时必须回到这里补替身 ——
// 这是刻意的：它保证测试替身不会悄悄落后于真实查询集。

func (f *fakeQuerier) CountPendingJudges(context.Context) (int64, error) {
	f.note("CountPendingJudges")
	return f.pendingJudges, f.fail()
}

func (f *fakeQuerier) GetSubmitProblem(_ context.Context, _ int32) (dbgen.GetSubmitProblemRow, error) {
	f.note("GetSubmitProblem")
	if err := f.fail(); err != nil {
		return dbgen.GetSubmitProblemRow{}, err
	}
	return f.submitProblem, nil
}

func (f *fakeQuerier) GetContestSubmitProblem(_ context.Context, _ dbgen.GetContestSubmitProblemParams) (int32, error) {
	f.note("GetContestSubmitProblem")
	if err := f.fail(); err != nil {
		return 0, err
	}
	return f.contestSubmitPID, nil
}

func (f *fakeQuerier) GetContestForSubmit(_ context.Context, _ dbgen.GetContestForSubmitParams) (dbgen.GetContestForSubmitRow, error) {
	f.note("GetContestForSubmit")
	if err := f.fail(); err != nil {
		return dbgen.GetContestForSubmitRow{}, err
	}
	return f.contestForSubmit, nil
}

func (f *fakeQuerier) CountContestInvites(_ context.Context, _ dbgen.CountContestInvitesParams) (int64, error) {
	f.note("CountContestInvites")
	return f.contestInvites, f.fail()
}

func (f *fakeQuerier) GetProblemSPJ(_ context.Context, _ int32) (string, error) {
	f.note("GetProblemSPJ")
	return f.spj, f.fail()
}

func (f *fakeQuerier) GetUserNick(_ context.Context, _ string) (string, error) {
	f.note("GetUserNick")
	if err := f.fail(); err != nil {
		return "", err
	}
	return f.nick, nil
}

func (f *fakeQuerier) GetLastRecentSubmit(_ context.Context, _ dbgen.GetLastRecentSubmitParams) (uint32, error) {
	f.note("GetLastRecentSubmit")
	return f.lastRecentSubmit, f.fail()
}

func (f *fakeQuerier) IsContestLocked(_ context.Context, _ dbgen.IsContestLockedParams) (bool, error) {
	f.note("IsContestLocked")
	return f.contestLocked, f.fail()
}

func (f *fakeQuerier) GetProblemRemoteOJ(_ context.Context, _ int32) (sql.NullString, error) {
	f.note("GetProblemRemoteOJ")
	return f.remoteOJ, f.fail()
}

func (f *fakeQuerier) CountUserAccepted(_ context.Context, _ dbgen.CountUserAcceptedParams) (int64, error) {
	f.note("CountUserAccepted")
	return f.userAccepted, f.fail()
}

func (f *fakeQuerier) CountAcceptedForProblem(_ context.Context, _ int32) (int64, error) {
	f.note("CountAcceptedForProblem")
	return f.problemAccepted, f.fail()
}

func (f *fakeQuerier) CountSubmitsForProblem(_ context.Context, _ int32) (int64, error) {
	f.note("CountSubmitsForProblem")
	return f.problemSubmits, f.fail()
}

func (f *fakeQuerier) SetProblemAccepted(_ context.Context, _ dbgen.SetProblemAcceptedParams) error {
	f.note("SetProblemAccepted")
	return f.fail()
}

func (f *fakeQuerier) SetProblemSubmit(_ context.Context, _ dbgen.SetProblemSubmitParams) error {
	f.note("SetProblemSubmit")
	return f.fail()
}

func (f *fakeQuerier) IncrementProblemSubmit(_ context.Context, _ int32) error {
	f.note("IncrementProblemSubmit")
	return f.fail()
}

func (f *fakeQuerier) IncrementContestProblemSubmit(_ context.Context, _ dbgen.IncrementContestProblemSubmitParams) error {
	f.note("IncrementContestProblemSubmit")
	return f.fail()
}

// ── 档案页与比赛列表的替身 ────────────────────────────────────────────
//
// 同上：新增 dbgen.FullQuerier 方法时必须在这里补替身，
// 否则 `var _ dbgen.FullQuerier` 会编译失败。
//
// 档案页的组装逻辑（等级计算、学号映射、颜色判定、差集去重）
// 在 handler_userinfo_test.go 里用这些字段驱动验证；
// 这里只保证"能调通、能返回可注入的结果"。

func (f *fakeQuerier) ListActiveNOIPContests(context.Context, string, string) ([]int32, error) {
	f.note("ListActiveNOIPContests")
	return f.noipContests, f.fail()
}

func (f *fakeQuerier) GetUserProfile(_ context.Context, _ string) (dbgen.User, error) {
	f.note("GetUserProfile")
	if err := f.fail(); err != nil {
		return dbgen.User{}, err
	}
	return f.userProfile, nil
}

func (f *fakeQuerier) CountUserACProblems(_ context.Context, _ string, _ []int32) (int64, error) {
	f.note("CountUserACProblems")
	return f.userAC, f.fail()
}

func (f *fakeQuerier) CountUserSubmittedProblems(_ context.Context, _ string, _ []int32) (int64, error) {
	f.note("CountUserSubmittedProblems")
	return f.userSubmit, f.fail()
}

func (f *fakeQuerier) UpdateUserSolvedSubmit(context.Context, int32, int32, string) error {
	f.note("UpdateUserSolvedSubmit")
	return f.fail()
}

func (f *fakeQuerier) CountUsersAhead(context.Context, int32, []string) (int64, error) {
	f.note("CountUsersAhead")
	return f.usersAhead, f.fail()
}

func (f *fakeQuerier) RecalcUserCoinEarned(context.Context, string) error {
	f.note("RecalcUserCoinEarned")
	return f.fail()
}

func (f *fakeQuerier) ListUserLoginLog(_ context.Context, _ string) ([]dbgen.UserLoginLogRow, error) {
	f.note("ListUserLoginLog")
	return f.loginLog, f.fail()
}

func (f *fakeQuerier) ListUserResultStats(_ context.Context, _ string, _ []int32) ([]dbgen.UserResultStatRow, error) {
	f.note("ListUserResultStats")
	return f.resultStats, f.fail()
}

func (f *fakeQuerier) ListUserContestStats(_ context.Context, _ string, _ string) ([]dbgen.UserContestStatRow, error) {
	f.note("ListUserContestStats")
	return f.contestStats, f.fail()
}

func (f *fakeQuerier) ListUserACProblems(_ context.Context, _ string, _ []int32) ([]dbgen.UserProblemCountRow, error) {
	f.note("ListUserACProblems")
	return f.acProblems, f.fail()
}

func (f *fakeQuerier) ListUserNonACProblems(_ context.Context, _ string, _ []int32) ([]dbgen.UserProblemCountRow, error) {
	f.note("ListUserNonACProblems")
	return f.badProblems, f.fail()
}

func (f *fakeQuerier) ListUserSubChart(_ context.Context, _ string, _ []int32) ([]dbgen.UserSubChartRow, error) {
	f.note("ListUserSubChart")
	return f.subChart, f.fail()
}

func (f *fakeQuerier) ListViewerACProblems(_ context.Context, _ string, _ []int32) ([]int32, error) {
	f.note("ListViewerACProblems")
	return f.viewerAC, f.fail()
}

func (f *fakeQuerier) CountActiveContests(context.Context) (int64, error) {
	f.note("CountActiveContests")
	return f.contestTotal, f.fail()
}

func (f *fakeQuerier) ListContestSet(_ context.Context, _ dbgen.ContestSetFilter, _, _ int32) ([]dbgen.ContestSetRow, error) {
	f.note("ListContestSet")
	return f.contestSet, f.fail()
}

func (f *fakeQuerier) ListMyContestIDs(_ context.Context, _ string) ([]int32, error) {
	f.note("ListMyContestIDs")
	return f.myContests, f.fail()
}

func (f *fakeQuerier) ListVIPContestIDs(context.Context) ([]int32, error) {
	f.note("ListVIPContestIDs")
	return f.vipContests, f.fail()
}

func (f *fakeQuerier) CountUserContestProblemAC(_ context.Context, _ string, _, _ int32) (dbgen.ContestProblemACCounts, error) {
	f.note("CountUserContestProblemAC")
	return f.problemAC, f.fail()
}

func (f *fakeQuerier) ProblemUsedInUpcomingContest(context.Context, int32) (bool, error) {
	f.note("ProblemUsedInUpcomingContest")
	return f.usedInUpcoming, f.fail()
}

// ── 比赛统计页的替身 ──────────────────────────────────────────────────

func (f *fakeQuerier) ListContestProblemIDs(_ context.Context, _ int32) ([]dbgen.ContestProblemID, error) {
	f.note("ListContestProblemIDs")
	return f.contestProblemIDs, f.fail()
}

func (f *fakeQuerier) ListContestSolutionStats(_ context.Context, _ int32) ([]dbgen.ContestSolutionStat, error) {
	f.note("ListContestSolutionStats")
	return f.contestSolutionStats, f.fail()
}

func (f *fakeQuerier) ListContestDailySubmits(_ context.Context, _ int32) ([]dbgen.ContestDailyCount, error) {
	f.note("ListContestDailySubmits")
	return f.dailySubmits, f.fail()
}

func (f *fakeQuerier) ListContestDailyAC(_ context.Context, _ int32) ([]dbgen.ContestDailyCount, error) {
	f.note("ListContestDailyAC")
	return f.dailyAC, f.fail()
}

func (f *fakeQuerier) ProblemUsedInOngoingContest(context.Context, int32) (bool, error) {
	f.note("ProblemUsedInOngoingContest")
	return f.usedInOngoing, f.fail()
}

func (f *fakeQuerier) CountRunningNOIPContests(context.Context, string, string) (int64, error) {
	f.note("CountRunningNOIPContests")
	return f.runningNOIP, f.fail()
}

func (f *fakeQuerier) CountRanklistUsers(context.Context) (int64, error) {
	f.note("CountRanklistUsers")
	return f.ranklistTotal, f.fail()
}

func (f *fakeQuerier) ListRanklistUsers(_ context.Context, _ dbgen.RanklistFilter, _, _ int32) ([]dbgen.RanklistUserRow, error) {
	f.note("ListRanklistUsers")
	return f.ranklistRows, f.fail()
}

func (f *fakeQuerier) LastSolutionIDBefore(_ context.Context, date string) (int64, error) {
	f.note("LastSolutionIDBefore")
	if f.lastSolutionID != nil {
		if v, ok := f.lastSolutionID[date]; ok {
			return v, f.fail()
		}
	}
	return f.lastSolutionDefault, f.fail()
}

func (f *fakeQuerier) CountRanklistScopedUsers(context.Context, dbgen.RanklistScope) (int64, error) {
	f.note("CountRanklistScopedUsers")
	return f.ranklistScopedTot, f.fail()
}

func (f *fakeQuerier) ListRanklistScopedUsers(_ context.Context, _ dbgen.RanklistScope, _ []string, _, _ int32) ([]dbgen.RanklistUserRow, error) {
	f.note("ListRanklistScopedUsers")
	return f.ranklistScoped, f.fail()
}

// ── 单题提交状态页（problemstatus.php）的替身 ─────────────────────────

func (f *fakeQuerier) CountRunningNOIPProblemContests(_ context.Context, _ dbgen.CountRunningNOIPProblemContestsParams) (int64, error) {
	f.note("CountRunningNOIPProblemContests")
	return f.psNOIP, f.fail()
}

func (f *fakeQuerier) CountProblemUsers(_ context.Context, _ int32) (int64, error) {
	f.note("CountProblemUsers")
	return f.psUsers, f.fail()
}

func (f *fakeQuerier) CountProblemACUsers(_ context.Context, _ int32) (int64, error) {
	f.note("CountProblemACUsers")
	return f.psACUsers, f.fail()
}

func (f *fakeQuerier) GetProblemResultStats(_ context.Context, _ int32) ([]dbgen.GetProblemResultStatsRow, error) {
	f.note("GetProblemResultStats")
	return f.psResults, f.fail()
}

func (f *fakeQuerier) ProblemInRunningContest(_ context.Context, _ dbgen.ProblemInRunningContestParams) (bool, error) {
	f.note("ProblemInRunningContest")
	return f.psInRunning, f.fail()
}

func (f *fakeQuerier) UserSolvedProblem(_ context.Context, _ dbgen.UserSolvedProblemParams) (bool, error) {
	f.note("UserSolvedProblem")
	return false, f.fail()
}

func (f *fakeQuerier) GetProblemSource(_ context.Context, _ int32) (sql.NullString, error) {
	f.note("GetProblemSource")
	return f.psSource, f.fail()
}

func (f *fakeQuerier) ListProblemIDsBySource(_ context.Context, _ dbgen.ListProblemIDsBySourceParams) ([]int32, error) {
	f.note("ListProblemIDsBySource")
	return f.psProblemIDs, f.fail()
}

func (f *fakeQuerier) ListProblemBestACSolutions(_ context.Context, _ int32, _, _ int32) ([]dbgen.ProblemBestACSolRow, error) {
	f.note("ListProblemBestACSolutions")
	return f.psSolutions, f.fail()
}

// ── 源代码查看页（showsource.php）的替身 ─────────────────────────────

func (f *fakeQuerier) GetSolutionByID(_ context.Context, _ uint32) (dbgen.GetSolutionByIDRow, error) {
	f.note("GetSolutionByID")
	return f.ssSolution, f.fail()
}

func (f *fakeQuerier) GetContestWindow(_ context.Context, _ int32) (dbgen.GetContestWindowRow, error) {
	f.note("GetContestWindow")
	return f.ssContestWindow, f.fail()
}

func (f *fakeQuerier) ProblemLockedContestID(_ context.Context, _ dbgen.ProblemLockedContestIDParams) (int32, error) {
	f.note("ProblemLockedContestID")
	return f.ssLockedCID, f.fail()
}

func (f *fakeQuerier) GetSourceCodeForSolution(_ context.Context, _ int32) (string, error) {
	f.note("GetSourceCodeForSolution")
	return f.ssSource, f.fail()
}

// ── AJAX（status-ajax.php / problem-ajax.php）的替身 ──────────────────

func (f *fakeQuerier) GetSolutionStatusByID(_ context.Context, _ uint32) (dbgen.Solution, error) {
	f.note("GetSolutionStatusByID")
	return f.saSolution, f.fail()
}

func (f *fakeQuerier) GetProblemCreator(_ context.Context, rightstr string) (string, error) {
	f.note("GetProblemCreator")
	f.paRightstr = rightstr
	if err := f.fail(); err != nil {
		return "", err
	}
	return f.paCreator, nil
}

func (f *fakeQuerier) CountNews(_ context.Context, _ string) (int64, error) {
	f.note("CountNews")
	return 0, f.fail()
}

func (f *fakeQuerier) GetNoipContestIDs(_ context.Context, _ sql.NullString) ([]int32, error) {
	f.note("GetNoipContestIDs")
	return nil, f.fail()
}

func (f *fakeQuerier) GetMonthStarBoundary(_ context.Context) (uint32, error) {
	f.note("GetMonthStarBoundary")
	return 0, f.fail()
}

func (f *fakeQuerier) GetLastSolutionID(_ context.Context) (interface{}, error) {
	f.note("GetLastSolutionID")
	return uint32(0), f.fail()
}

func (f *fakeQuerier) GetSubmitSpeed(_ context.Context, _ uint32) (interface{}, error) {
	f.note("GetSubmitSpeed")
	return float64(0), f.fail()
}

func (f *fakeQuerier) GetUserContests(_ context.Context, _ string) ([]sql.NullInt32, error) {
	f.note("GetUserContests")
	return nil, f.fail()
}

func (f *fakeQuerier) GetNewsList(_ context.Context) ([]dbgen.GetNewsListRow, error) {
	f.note("GetNewsList")
	return f.newsList, f.fail()
}

func (f *fakeQuerier) GetMonthStarRank(_ context.Context, _ dbgen.GetMonthStarRankParams) ([]dbgen.GetMonthStarRankRow, error) {
	f.note("GetMonthStarRank")
	return f.monthStars, f.fail()
}

func (f *fakeQuerier) GetRecentProblems(_ context.Context, _ string) ([]dbgen.GetRecentProblemsRow, error) {
	f.note("GetRecentProblems")
	return f.recentProblems, f.fail()
}

func (f *fakeQuerier) GetRecentContests(_ context.Context) ([]dbgen.GetRecentContestsRow, error) {
	f.note("GetRecentContests")
	return f.recentContests, f.fail()
}

func (f *fakeQuerier) GetChartDataAll(_ context.Context, _ uint32) ([]dbgen.GetChartDataAllRow, error) {
	f.note("GetChartDataAll")
	return f.chartDataAll, f.fail()
}

func (f *fakeQuerier) GetChartDataAC(_ context.Context, _ uint32) ([]dbgen.GetChartDataACRow, error) {
	f.note("GetChartDataAC")
	return f.chartDataAC, f.fail()
}

func (f *fakeQuerier) GetUserSubmittedProblems(_ context.Context, _ string) ([]dbgen.GetUserSubmittedProblemsRow, error) {
	f.note("GetUserSubmittedProblems")
	return f.submittedProblems, f.fail()
}

func (f *fakeQuerier) GetUserUnsolvedProblems(_ context.Context, _ dbgen.GetUserUnsolvedProblemsParams) ([]dbgen.GetUserUnsolvedProblemsRow, error) {
	f.note("GetUserUnsolvedProblems")
	return f.unsolvedProblems, f.fail()
}

func (f *fakeQuerier) GetActiveContests(_ context.Context) ([]dbgen.GetActiveContestsRow, error) {
	f.note("GetActiveContests")
	return f.activeContests, f.fail()
}

func (f *fakeQuerier) GetOnlineUsers(_ context.Context) ([]dbgen.GetOnlineUsersRow, error) {
	f.note("GetOnlineUsers")
	return f.onlineUsers, f.fail()
}

func (f *fakeQuerier) GetOnlineUsersByIP(_ context.Context, _ sql.NullString) ([]dbgen.GetOnlineUsersByIPRow, error) {
	f.note("GetOnlineUsersByIP")
	return nil, f.fail()
}

func (f *fakeQuerier) GetOnlineUsersExcludeSelf(_ context.Context, _ string) ([]dbgen.GetOnlineUsersExcludeSelfRow, error) {
	f.note("GetOnlineUsersExcludeSelf")
	return nil, f.fail()
}

func (f *fakeQuerier) GetRecentContestsList(_ context.Context) ([]dbgen.GetRecentContestsListRow, error) {
	f.note("GetRecentContestsList")
	return f.recentContestsList, f.fail()
}

func (f *fakeQuerier) GetACCodeByUser(_ context.Context, _ string) ([]dbgen.GetACCodeByUserRow, error) {
	f.note("GetACCodeByUser")
	return f.acCodeByUser, f.fail()
}

func (f *fakeQuerier) GetContestCode(_ context.Context, _ sql.NullInt32) ([]dbgen.GetContestCodeRow, error) {
	f.note("GetContestCode")
	return f.contestCode, f.fail()
}

func (f *fakeQuerier) GetContestInfo(_ context.Context, _ int32) (dbgen.GetContestInfoRow, error) {
	f.note("GetContestInfo")
	return f.contestInfo, f.fail()
}

func (f *fakeQuerier) GetContestProblemCount(_ context.Context, _ sql.NullInt32) (int64, error) {
	f.note("GetContestProblemCount")
	return 0, f.fail()
}

func (f *fakeQuerier) GetContestSubmissions(_ context.Context, _ sql.NullInt32) ([]dbgen.GetContestSubmissionsRow, error) {
	f.note("GetContestSubmissions")
	return f.contestSubmissions, f.fail()
}

func (f *fakeQuerier) GetFirstBlood(_ context.Context, _ sql.NullInt32) ([]dbgen.GetFirstBloodRow, error) {
	f.note("GetFirstBlood")
	return f.firstBlood, f.fail()
}

func (f *fakeQuerier) GetContestTeams(_ context.Context, _ sql.NullInt32) ([]dbgen.GetContestTeamsRow, error) {
	f.note("GetContestTeams")
	return f.contestTeams, f.fail()
}

func (f *fakeQuerier) GetContestProblemMap(_ context.Context, _ sql.NullInt32) ([]dbgen.GetContestProblemMapRow, error) {
	f.note("GetContestProblemMap")
	return f.contestProblemMap, f.fail()
}

func (f *fakeQuerier) GetNewsByID(_ context.Context, _ int32) ([]dbgen.GetNewsByIDRow, error) {
	f.note("GetNewsByID")
	return f.newsByID, f.fail()
}

func (f *fakeQuerier) GetMailList(_ context.Context, _ dbgen.GetMailListParams) ([]dbgen.GetMailListRow, error) {
	f.note("GetMailList")
	return nil, f.fail()
}

func (f *fakeQuerier) GetMailByID(_ context.Context, _ dbgen.GetMailByIDParams) ([]dbgen.GetMailByIDRow, error) {
	f.note("GetMailByID")
	return nil, f.fail()
}

func (f *fakeQuerier) MarkMailRead(_ context.Context, _ int32) error {
	f.note("MarkMailRead")
	return f.fail()
}

func (f *fakeQuerier) CheckMailPrivilege(_ context.Context, _ dbgen.CheckMailPrivilegeParams) ([]int32, error) {
	f.note("CheckMailPrivilege")
	return []int32{1}, f.fail()
}

func (f *fakeQuerier) SendMail(_ context.Context, _ dbgen.SendMailParams) error {
	f.note("SendMail")
	return f.fail()
}

func (f *fakeQuerier) GetUserByID(_ context.Context, _ string) (dbgen.GetUserByIDRow, error) {
	f.note("GetUserByID")
	return f.userByID, f.fail()
}

func (f *fakeQuerier) UpdateUserProfile(_ context.Context, _ dbgen.UpdateUserProfileParams) error {
	f.note("UpdateUserProfile")
	return f.fail()
}

func (f *fakeQuerier) UpdateUserSolutionNick(_ context.Context, _ dbgen.UpdateUserSolutionNickParams) error {
	f.note("UpdateUserSolutionNick")
	return f.fail()
}

func (f *fakeQuerier) GetUserEmail(_ context.Context, _ string) (sql.NullString, error) {
	f.note("GetUserEmail")
	return f.userEmail, f.fail()
}

func (f *fakeQuerier) UpdateUserPassword(_ context.Context, _ dbgen.UpdateUserPasswordParams) error {
	f.note("UpdateUserPassword")
	return f.fail()
}

func (f *fakeQuerier) UpdateUserPasswordByKey(_ context.Context, _ dbgen.UpdateUserPasswordByKeyParams) error {
	f.note("UpdateUserPasswordByKey")
	return f.fail()
}

func (f *fakeQuerier) GetContestTitleType(_ context.Context, _ int32) (dbgen.GetContestTitleTypeRow, error) {
	f.note("GetContestTitleType")
	return f.saContestTitle, f.fail()
}

func (f *fakeQuerier) GetCompileError(_ context.Context, _ int32) (sql.NullString, error) {
	f.note("GetCompileError")
	if f.saCompileErr != nil {
		return f.saCompileError, f.saCompileErr
	}
	return f.saCompileError, f.fail()
}

func (f *fakeQuerier) GetRuntimeError(_ context.Context, _ int32) (sql.NullString, error) {
	f.note("GetRuntimeError")
	if f.saRuntimeErr != nil {
		return f.saRuntimeError, f.saRuntimeErr
	}
	return f.saRuntimeError, f.fail()
}

func (f *fakeQuerier) DeleteCustomInput(_ context.Context, _ int32) error {
	f.note("DeleteCustomInput")
	return f.fail()
}

func (f *fakeQuerier) InsertLoginLog(_ context.Context, _ dbgen.InsertLoginLogParams) error {
	f.note("InsertLoginLog")
	return f.fail()
}

func (f *fakeQuerier) ActivateUserByCode(_ context.Context, _ string) error {
	f.note("ActivateUserByCode")
	return f.fail()
}
