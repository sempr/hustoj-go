// 题目列表与详情的 HTTP 处理。
//
// 路径刻意与 PHP 完全一致（/problemset.php、/problem.php）：
// 绞杀期由 nginx 按路径把流量切给本服务，URL 不变才能只改路由不改链接，
// 也不需要处理旧链接的重定向。

package web

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// Identity 是当前访问者的身份。
//
// 目前只有匿名与管理员两种：迁移期还没有 PHP session 桥，
// 无法可靠取到登录用户，因此 userID 恒为空、AC 标记恒不显示。
// 接入 session 桥时在这里补 userID 字段即可，不需要改任何查询或模板 ——
// 这就是保留该类型而不是直接传 string 的原因。
type Identity struct {
	UserID               string
	Nick                 string
	GroupName            string
	Admin                bool
	ContestCreator       bool
	ProblemEditor        bool
	ProblemVerifier      bool
	SourceBrowser        bool
	LoggedIn             bool
	ContestInvites       map[int32]bool // c<cid> 受邀标记
	ContestTimeLimitUsed map[int32]bool // m<cid> 限时赛已用标记
	CSRFKeys             []string       // PHP session 中的 csrf_keys 池

	// SessionID 是本次请求携带的会话 ID。
	// 写回会话（登录、签发 CSRF）时需要它，空串表示没有可用会话。
	SessionID string

	anonymous bool
}

// phpSessionCookieName 是 PHP-FPM 的会话 cookie 名。
//
// Go 侧读不出会话内容，只需要知道"有没有"——有会话时依赖会话的判定
// （私有比赛受邀、限时赛计时）就无法复现，必须按拒绝处理。
// 取值来自 php.ini 的 session.name，线上实测 Set-Cookie 头也是它。
const phpSessionCookieName = "PHPSESSID"

// Anonymous 是未登录访客。
func Anonymous() Identity { return Identity{anonymous: true} }

// CanManageContest 报告该身份能否管理比赛。
//
// 对应 problem.php:80 与 contest-check.php:77 的三处判断：
// administrator、contest_creator、problem_editor。
// 注意 problem.php:33 的练习模式判定里还多一个 problem_verifiter（上游拼写错误，
// 两个文件用的确实是不同的名字），它只影响练习模式，不参与比赛管理。
func (i Identity) CanManageContest() bool {
	return i.Admin || i.ContestCreator || i.ProblemEditor
}

// HasContestInvite 判断是否受邀参加某私有比赛。
func (i Identity) HasContestInvite(cid int32) bool {
	return i.ContestInvites != nil && i.ContestInvites[cid]
}

// HasContestTimeLimitUsed 判断限时赛是否已计时（用于拒绝超时）。
func (i Identity) HasContestTimeLimitUsed(cid int32) bool {
	return i.ContestTimeLimitUsed != nil && i.ContestTimeLimitUsed[cid]
}

// HasCSRFToken 判断给定的 token 是否在 session 的 csrf_keys 池中。
func (i Identity) HasCSRFToken(token string) bool {
	for _, t := range i.CSRFKeys {
		if t == token {
			return true
		}
	}
	return false
}

// viewerFor 把身份映射到题目列表的可见范围，对应 problemset.php:120-133。
//
// 注意 PHP 的判断顺序是「管理员 → 自由练习 → 普通用户」，
// 管理员优先于自由练习开关；顺序反了会让自由练习站点的管理员
// 看不到已删除题目。
func (s *Server) viewerFor(id Identity) Viewer {
	switch {
	case id.Admin:
		return ViewerAdmin
	case s.cfg.FreePractice:
		return ViewerFreePractice
	default:
		return ViewerMember
	}
}

// parsePage 解析 ?page=，语义对齐 PHP 的 intval()。
//
// intval 的两个关键性质不能想当然地用 strconv.Atoi 替代：
//   - 容忍首尾空白与前导正负号：Atoi(" 2 ") 直接报错
//   - 解析到第一个非数字即停止：Atoi("2abc") 直接报错，而 intval 得 2
//
// 这些输入在真实环境里并不少见（手改 URL、从别处复制查询串），
// 静默当成第 1 页会让人以为搜索/分页"失灵"。
//
// 另外 PHP 不夹取下界：?page=0 会算出 " LIMIT -50,50" 直接触发 SQL 语法错误。
// 这里夹到 1，属刻意的行为修正。
func parsePage(raw string) int {
	return clampPage(intval(raw))
}

// intval 复刻 PHP 的 (int) 转换：跳过前导空白与符号，解析连续数字，忽略其余部分。
func intval(s string) int {
	s = strings.TrimSpace(s)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	n, digits := 0, 0
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
		digits++
		// 防溢出：超出 int 范围后 PHP 得到的是 PHP_INT_MAX，
		// 这里饱和到最大值而不是绕回负数。
		if n > math.MaxInt32 {
			n = math.MaxInt32
		}
	}
	if digits == 0 {
		return 0
	}
	if neg {
		return -n
	}
	return n
}

// clampPage 把页码夹到 [1, +inf)。
func clampPage(p int) int {
	if p < 1 {
		return 1
	}
	return p
}

// parseSearch 取 ?search=，并去掉首尾空白。
// 对应 problemset.php:72 的 `trim($_GET['search']) != ""`：
// 只由全空白组成的搜索词等同于没搜索，必须同样处理，
// 否则会走搜索分支但匹配不到任何东西。
func parseSearch(raw string) string { return strings.TrimSpace(raw) }

// handleProblemSet 实现 /problemset.php。
func (s *Server) handleProblemSet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := parsePage(q.Get("page"))
	search := parseSearch(q.Get("search"))
	id := s.resolveIdentity(w, r)

	// now 由处理函数传入而非在仓储层内部取：便于测试注入固定时间，
	// 也让"排除比赛进行中题目"这一依赖当前时刻的行为在测试里可复现。
	now := time.Now()

	data, err := ListProblems(r.Context(), s.queries(), s.viewerFor(id), id.UserID, page, search, now)
	if err != nil {
		s.log.Error("题目列表查询失败", "err", err, "viewer", s.viewerFor(id), "search", search, "page", page)
		http.Error(w, "题目列表暂时不可用", http.StatusInternalServerError)
		return
	}

	s.renderPage(w, r, http.StatusOK, "problemset", "题目列表", buildPageVM(data))
}

// handleProblem 实现 /problem.php。
//
// 这个路径同时承载两种模式（problem.php:25 与 :53）：
//
//	?id=N              练习模式，按 problem_id 取题
//	?cid=X&pid=Y       比赛模式，按 contest_problem.num 取题
//
// 判别顺序与 PHP 的 if/else if 链一致：先看 id，再看 cid+pid，
// 两者都没有才落到 PHP:111 的"题目不存在"分支。
func (s *Server) handleProblem(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	identity := s.resolveIdentity(w, r)

	if raw := q.Get("id"); raw != "" {
		s.handleProblemPractice(w, r, identity, raw, now)
		return
	}
	if q.Get("cid") != "" && q.Get("pid") != "" {
		s.handleProblemContest(w, r, identity, now)
		return
	}

	// PHP 走 intval("") 得 0，查不到行后渲染"题目当前不可用"错误页。
	// 这里给 400：请求本身就不合法，与"题目不存在"是不同的诊断。
	http.Error(w, "缺少题目 id 或比赛参数 cid/pid", http.StatusBadRequest)
}

// handleProblemPractice 处理 /problem.php?id=N。
func (s *Server) handleProblemPractice(w http.ResponseWriter, r *http.Request, identity Identity, raw string, now time.Time) {
	// id 非法时给 400。PHP 会 intval 后查不到行，渲染空白页；
	// 显式 400 更利于发现被爬虫或错误链接打进来的请求。
	id64, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil || id64 <= 0 {
		http.Error(w, "非法的题目 id", http.StatusBadRequest)
		return
	}

	viewer := s.viewerFor(identity)
	detail, err := GetProblem(r.Context(), s.queries(), viewer, int32(id64), now)
	if err != nil {
		s.writeProblemError(w, r, err, int32(id64))
		return
	}

	// NOIP 锁榜对两种模式都生效（problem.php:150-168 在分支之外）。
	detail.StatsHidden, detail.HintHidden = s.applyNoIPMask(r, identity, detail.ProblemID, now)

	// 需要 freopen 的题目在题面顶部给出文件名提示。
	detail.InputFileName, detail.OutputFileName = problemFileIONames(s.cfg.OJHome, detail.ProblemID)

	s.renderPage(w, r, http.StatusOK, "problemDetail", detail.Title, detail)
}

// handleProblemContest 处理 /problem.php?cid=X&pid=Y。
func (s *Server) handleProblemContest(w http.ResponseWriter, r *http.Request, identity Identity, now time.Time) {
	q := r.URL.Query()
	// PHP 用 abs(intval())，负数 cid 会被折叠成正数（contest-check.php:4,9）。
	cid := int32(abs(intval(q.Get("cid"))))
	pid := int32(intval(q.Get("pid")))

	contest, err := s.queries().GetContestByID(r.Context(), cid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.log.Error("查询比赛失败", "err", err, "contest_id", cid)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}

	access, err := EvaluateContestAccess(r.Context(), s.queries(), contest, identity, now,
		s.remoteIP(r), hasPHPSession(r))
	if err != nil {
		s.log.Error("比赛准入判定失败", "err", err, "contest_id", cid)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}
	if !access.OK() {
		// PHP 一律 exit(0) 渲染错误页，状态码仍是 200。
		// 这里保留 200：比赛未开始是正常状态，不是资源不存在，
		// 返回 404 会让浏览器与监控都误判。
		s.renderPage(w, r, http.StatusOK, "contestError",
			"比赛准入失败", newContestErrorView(cid, &contest, access.Deny, access.Deny == ContestSubnet))
		return
	}

	row, err := ContestProblem(r.Context(), s.queries(), cid, pid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.renderPage(w, r, http.StatusOK, "contestError", "题目不存在",
				ContestAccess{Deny: ContestNotFound})
			return
		}
		s.log.Error("查询比赛题目失败", "err", err, "contest_id", cid, "pid", pid)
		http.Error(w, "题目暂时不可用", http.StatusInternalServerError)
		return
	}

	detail := problemFromRow(row)
	detail.Contest = &ContestDetail{
		CID:      cid,
		Title:    contest.Title.String,
		Langmask: contest.Langmask,
		PID:      pid,
		IsOver:   access.IsOver,
	}
	detail.ContestIsOver = access.IsOver
	detail.StatsHidden, detail.HintHidden = s.applyNoIPMask(r, identity, detail.ProblemID, now)

	// 需要 freopen 的题目在题面顶部给出文件名提示。
	detail.InputFileName, detail.OutputFileName = problemFileIONames(s.cfg.OJHome, detail.ProblemID)

	s.renderPage(w, r, http.StatusOK, "problemDetail", detail.Title, detail)
}

// applyNoIPMask 查询 NOIP 锁榜状态并写回详情。
//
// problem.php:151-157 的 $flag 分支只对未命中关键字的情况补查
// problem_locked($id, 28)，两者等价，因此合并成一个查询。
// 管理员与比赛创建者不受影响 —— PHP 那边是继续执行而保留原值，
// 这里显式返回 false，避免把管理员的统计数字也抹掉。
func (s *Server) applyNoIPMask(r *http.Request, identity Identity, problemID int32, now time.Time) (statsHidden, hintHidden bool) {
	if identity.CanManageContest() {
		return false, false
	}
	locked, err := IsNoIPLocked(r.Context(), s.queries(), problemID, s.cfg.NoIPKeyword, now)
	if err != nil {
		s.log.Error("查询锁榜状态失败", "err", err, "problem_id", problemID)
		return false, false
	}
	return locked, locked && !s.cfg.NoIPHint
}

// writeProblemError 把题目查询错误映射成 HTTP 响应。
//
// sql.ErrNoRows 对应 PHP 的"题目不存在"（渲染 error.php，状态码 200），
// 这里返回 404：语义更准确，也不会让监控把不存在的题目算成服务端故障。
func (s *Server) writeProblemError(w http.ResponseWriter, r *http.Request, err error, problemID int32) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "题目不存在", http.StatusNotFound)
		return
	}
	s.log.Error("题目详情查询失败", "err", err, "problem_id", problemID)
	http.Error(w, "题目暂时不可用", http.StatusInternalServerError)
}

// hasPHPSession 报告请求是否带着 PHP 会话。
//
// Go 侧读不了会话内容，但**有没有**会话决定了一批判定能否可靠执行：
// 私有比赛的受邀标记、限时赛计时都存在会话里。有会话时这些判定
// 无法复现，一律按拒绝处理（见 EvaluateContestAccess）。
func hasPHPSession(r *http.Request) bool {
	_, err := r.Cookie(phpSessionCookieName)
	return err == nil
}

// maskStat 在 NOIP 锁榜时把统计数字替换成问号。
//
// PHP 写的是 '<font color="red"> ? </font>'（problem.php:159-160），
// 这里输出同样的问号占位，保持视觉一致。
func maskStat(hidden bool, v sql.NullInt32) string {
	if hidden {
		return "?"
	}
	return nullIntStr(v)
}

// nullIntStr 把可空计数渲染成字符串：NULL 显示为空，与 PHP 的
// 「NULL 拼接成空串」一致。
func nullIntStr(v sql.NullInt32) string {
	if !v.Valid {
		return ""
	}
	return strconv.FormatInt(int64(v.Int32), 10)
}

// trimFloat 去掉时限末尾的 .0，与 PHP 的 float 打印习惯对齐。
func trimFloat(f float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(f, 'f', -1, 64), ".0")
}

// registerProblemRoutes 挂载题目相关路由。
func (s *Server) registerProblemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /problemset.php", s.handleProblemSet)
	mux.HandleFunc("GET /problem.php", s.handleProblem)
}

// queries 返回数据库查询接口。
func (s *Server) queries() dbgen.FullQuerier {
	if s.q != nil {
		return s.q
	}
	return dbgen.New(s.db)
}

// absInt32 返回 int32 的绝对值。
//
// abs() 返回 int，直接用在 int32 上要写转换，混在提交逻辑里很碍眼。
// 刻意不做 MIN_INT32 的处理：题目 id 来自数据库主键，不可能是极小值。
func absInt32(n int32) int32 {
	if n < 0 {
		return -n
	}
	return n
}

// canSubmitDefunct 报告该身份能否向已删除题目提交。
//
// 对应 submit.php:71 的三个 session 键：administrator、problem_editor、
// problem_verifiter。最后一个是上游的拼写错误（正确拼法是 verifier），
// 但 PHP 读的就是这个错拼的键，两端必须一致。
//
// 比赛模式不走这条路径：那里的 defunct 由 SQL 写死为 'N'。
func (i Identity) canSubmitDefunct() bool {
	return i.Admin || i.ProblemEditor || i.ProblemVerifier
}
