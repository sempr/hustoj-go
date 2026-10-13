// 档案页（userinfo.php）与比赛列表页（contest.php 无 cid 分支）。
//
// 本文件承载两个页面，因为它们共享"NOIP 比赛排除"这一套判定：
// 档案页的 AC/提交统计、比赛列表的可见范围，都要用同一个
// ListActiveNOIPContests 结果，否则会出现"列表里看不到这场比赛、
// 但统计里又算了它的题"的错位。

package web

import (
	"context"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// ── 名字校验（对应 my_func.inc.php:589 的 is_valid_user_name）────────────

// isValidUserName 复刻 PHP 的 is_valid_user_name。
//
// PHP 那份是逐**字节**判断的（strlen + $s[$i]），不是按 Unicode 码点，
// 所以任何多字节字符都会因为高位字节不在允许集合里而被判为非法。
// 这里刻意用 range over string 拿到的是字节下标，因此行为与 PHP 一致。
//
// 两个容易被忽略的细节：
//   - 空串通过校验。PHP 的循环体不执行直接 return true，于是
//     `?user=`（缺参数）会走到查库那一步，最终以 "No such User!" 收场，
//     而不是在校验处就返回。Go 侧必须保持这个顺序，否则匿名探活
//     访问空 user 参数时看到的错误形态会变。
//   - '*' 只允许出现在下标 0。这是榜单里"昵称以 * 开头"的账号，
//     它们的名字本身就是 *开头的形式。
func isValidUserName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-', c == '_':
		case i == 0 && c == '*':
		default:
			return false
		}
	}
	return true
}

// specialGroupMap 对应 getMappedSpecial() 里的映射表
// （my_func.inc.php:281-287）：学号第 3-6 位 → 专业简称。
var specialGroupMap = map[string]string{
	"0701": "人智",
	"0708": "网工",
	"5702": "电科",
	"5701": "软工",
	"0207": "数技",
}

// getMappedSpecial 把学号映射成"班级组名"，对应 my_func.inc.php:280。
//
// 取 user_id 的第 3-6 位（PHP substr 下标从 0 起，即字节 2..5），
// 命中映射表则返回"前两位 + 专业简称"，例如 "02370123" → "02数技"。
// 没命中返回空串；user_id 太短也返回空串（PHP 的 substr 会返回
// 到串尾的短串，映射表里不存在这种键，结果同样是空串）。
func getMappedSpecial(userID string) string {
	if len(userID) < 6 {
		return ""
	}
	name, ok := specialGroupMap[userID[2:6]]
	if !ok {
		return ""
	}
	return userID[0:2] + name
}

// ── 等级（对应模板 userinfo.php:12-17）──────────────────────────────────

// levelThresholds / levelNames 是"通过题数 → 称号"的阶梯。
//
// 数值必须与 PHP 模板里的 $acneed / $accall 一致。
var levelThresholds = []int{10, 20, 30, 50, 80, 100, 200, 300, 500, 800, 1000}
var levelNames = []string{"萌新", "小小牛", "小牛", "小犇", "中牛", "中犇", "大牛", "大犇", "神牛", "神犇"}

// userLevel 返回称号、下一个称号名与还差几题。
//
// PHP 的写法是从大到小遍历阶梯，命中即覆盖，所以最终留下的是
// **最小**那个满足 AC < 门槛的档位。AC >= 1000 时一次都不命中，
// 停在初始值"锦鲤"且 calledid = -1 —— 此时"距离 <下一称号> 还需 N 题"
// 里的 N 会算成负数（模板 userinfo.php:62 直接输出 $acneed[0]-$AC）。
// 这是 PHP 的既有表现，属于锦鲤档的显示瑕疵，不是可修的语义，
// 这里原样保留，避免改出与 PHP 不一致的文案。
func userLevel(ac int) (name, nextName string, remain int) {
	name = "锦鲤"
	idx := -1
	for i := len(levelNames); i > 0; i-- {
		if ac < levelThresholds[i] {
			name = levelNames[i-1]
			idx = i - 1
		}
	}
	nextName = levelNames[idx+1]
	remain = levelThresholds[idx+1] - ac
	return name, nextName, remain
}

// ── NOIP 排除 ─────────────────────────────────────────────────────────

// activeNOIPContests 返回需要从档案统计中剔除的比赛。
//
// 对应 userinfo.php:90-97：仅当配置了 NOIP 关键字、且访问者不是管理员时
// 才去查。管理员不受限 —— 否则管理员查看学生档案会看到被清零的数字，
// 无法排查问题。
func (s *Server) activeNOIPContests(r *http.Request, isAdmin bool) ([]int32, error) {
	kw := s.cfg.NoIPKeyword
	if isAdmin || kw == "" {
		return nil, nil
	}
	// PHP 用 date('Y-m-d H:i', time()) 拼串（userinfo.php:88）。
	// 换算到本地时区，与 MySQL 侧 NOW() 的语义保持一致。
	now := time.Now().Format("2006-01-02 15:04:05")
	return s.queries().ListActiveNOIPContests(r.Context(), now, kw)
}

// ── 档案页视图模型 ─────────────────────────────────────────────────────

// userProblemChip 是"通过/未通过"色块列表里的一颗。
type userProblemChip struct {
	ProblemID int32
	// Count 是该题的提交次数。
	Count int64
	// ViewerPassed 表示**访客**通过过此题，决定色块是绿是红。
	//
	// 这里刻意区分"被查看者通过"与"访客通过"：PHP 的 $acc_arr 查的是
	// 访客的提交记录（userinfo.php:194），所以别人看你的档案时，
	// 你已通过的题全部显示红色，只有你自己看才是绿色。
	ViewerPassed bool
}

// userContestRow 是"比赛战绩"表格的一行。
type userContestRow struct {
	ContestID int32
	Title     string
	StartTime string
	Tried     int64
	AC        int64
}

// resultStatSlice 是饼图的一项。
type resultStatSlice struct {
	Result int32
	Count  int64
	Label  string
}

// subChartPoint 是日历热力图上的一天。
type subChartPoint struct {
	Date  string
	Count int64
}

// userInfoView 是档案页模板的数据。
type userInfoView struct {
	User      string
	Nick      string
	School    string
	GroupName string
	Email     string
	AvatarURL template.URL

	Level      string
	NextLevel  string
	LevelNeed  int
	AC         int
	Rank       int
	Starred    bool
	IsSelf     bool
	CoinEarned int
	CoinBonus  int
	CoinSpent  int
	CoinTotal  int

	Contests    []userContestRow
	TotalTry    int64
	TotalContAC int64

	ResultStats []resultStatSlice
	ACProblems  []userProblemChip
	BadProblems []userProblemChip
	SubChart    []subChartPoint
	MaxSubCount int64

	// LoginLog 仅管理员非空。
	LoginLog []dbgen.UserLoginLogRow
	IsAdmin  bool
}

// handleUserInfo 是 userinfo.php 的 Go 实现。
//
// 行为顺序刻意与 PHP 一致，校验与"查不到人"的出口不能互换：
//
//  1. is_valid_user_name 失败 → 纯文本 "No such User!"，不经模板
//  2. 查不到行，或命中 $OJ_RANK_HIDDEN → 错误页（标题是 $MSG_ERROR_INFO）
//
// 第 1 步在 PHP 里是 echo + exit(0)（userinfo.php:13-16），发生在任何
// 模板与错误页之前；第 2 步走 template/syzoj/error.php（:110-114）。
// 两者返回的都是 200，视觉上是"错误提示"，但一个是纯文本一个是带页面
// 框架的提示卡 —— 对拍时按 body 前缀区分，不要混为一谈。
func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !isValidUserName(user) {
		// PHP: echo "No such User!"; exit(0); —— 不套模板，也不带 header。
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("No such User!"))
		return
	}

	id := s.resolveIdentity(w, r)
	q := s.queries()
	ctx := r.Context()

	exclude, err := s.activeNOIPContests(r, id.Admin)
	if err != nil {
		s.log.Error("查询 NOIP 排除比赛失败", "err", err, "user", user)
		http.Error(w, "档案暂时不可用", http.StatusInternalServerError)
		return
	}

	profile, err := q.GetUserProfile(ctx, user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.renderUserInfoError(w, "No such User!")
			return
		}
		s.log.Error("查询用户档案失败", "err", err, "user", user)
		http.Error(w, "档案暂时不可用", http.StatusInternalServerError)
		return
	}

	// 非管理员看不到 $OJ_RANK_HIDDEN 里的账号。
	// PHP 是把它写进 SQL 的（userinfo.php:107），效果等同于"查无此人"。
	if !id.Admin && s.isRankHidden(profile.UserID) {
		s.renderUserInfoError(w, "No such User!")
		return
	}

	view, err := s.buildUserInfo(ctx, q, profile, exclude, id)
	if err != nil {
		s.log.Error("组装档案数据失败", "err", err, "user", user)
		http.Error(w, "档案暂时不可用", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, r, http.StatusOK, "userInfo", view.Nick+"@"+s.cfg.OJName, view)
}

// isRankHidden 判断账号是否在 $OJ_RANK_HIDDEN 名单里。
func (s *Server) isRankHidden(userID string) bool {
	for _, h := range s.cfg.RankHidden {
		if h == userID {
			return true
		}
	}
	return false
}

// renderUserInfoError 渲染"查无此人"的错误卡。
func (s *Server) renderUserInfoError(w http.ResponseWriter, msg string) {
	s.renderPage(w, nil, http.StatusOK, "userInfoError", "错误信息 - "+s.cfg.OJName,
		userInfoErrorView{Message: msg})
}

// buildUserInfo 组装档案页数据。
//
// 这里有三条 GET 请求里的写操作，与 PHP 保持一致（详见
// userinfo.sql.go 各方法的注释）：写 users.solved/submit、重算 coin_earned。
// 前者让榜单口径自愈，后者让金币与题面配置一致；漏掉任何一条都会让
// "档案页显示的 AC 数"和"榜单里的 AC 数"长期对不上。
func (s *Server) buildUserInfo(ctx context.Context, q dbgen.FullQuerier, u dbgen.User,
	exclude []int32, id Identity) (*userInfoView, error) {

	userID := u.UserID

	// ── AC / 提交数，并写回 users ──
	ac64, err := q.CountUserACProblems(ctx, userID, exclude)
	if err != nil {
		return nil, err
	}
	sub64, err := q.CountUserSubmittedProblems(ctx, userID, exclude)
	if err != nil {
		return nil, err
	}
	ac, submits := int32(ac64), int32(sub64)
	if err := q.UpdateUserSolvedSubmit(ctx, ac, submits, userID); err != nil {
		return nil, err
	}

	// ── 名次 ──
	// PHP 只在非管理员路径上加 $OJ_RANK_HIDDEN 之外的默认口径，
	// 但这一条本身对所有人生效（userinfo.php:144）。
	ahead, err := q.CountUsersAhead(ctx, ac, s.cfg.RankHidden)
	if err != nil {
		return nil, err
	}
	rank := int(ahead) + 1

	// ── 金币：先按首杀重算，再读 ──
	if err := q.RecalcUserCoinEarned(ctx, userID); err != nil {
		return nil, err
	}
	u, err = q.GetUserProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	v := &userInfoView{
		User:      userID,
		Nick:      u.Nick,
		School:    u.School,
		GroupName: u.GroupName,
		Email:     u.Email.String,
		AvatarURL: gravatarURL(u.Email.String),
		AC:        int(ac),
		Rank:      rank,
		// starred 只读，不调用 GitHub API。
		//
		// PHP 的 starred()（my_func.inc.php:354）在 starred=0 时会向
		// api.github.com 发一次带 10s 超时的同步请求。本部署 8 个用户
		// 全部 starred=0，也就是每次打开任何人的档案都会触发一次出站
		// HTTP，GitHub 不可达时页面要挂满 10 秒。
		//
		// 经确认改为只读 DB：放弃"首次点星自动发放 5 金币"这一条
		// 附带行为（coin_bonus+5），换取页面永远不会被外部服务拖住。
		Starred:    u.Starred > 0,
		IsSelf:     id.LoggedIn && id.UserID == userID,
		IsAdmin:    id.Admin,
		CoinEarned: int(u.CoinEarned),
		CoinBonus:  int(u.CoinBonus),
		CoinSpent:  int(u.CoinSpent),
		CoinTotal:  int(u.CoinEarned) + int(u.CoinBonus) - int(u.CoinSpent),
	}
	v.Level, v.NextLevel, v.LevelNeed = userLevel(int(ac))

	// group_name 为空时回落到学号映射出的班级（userinfo.php:119）。
	if v.GroupName == "" {
		v.GroupName = "[" + getMappedSpecial(userID) + "]"
	}

	// ── 登录日志（仅管理员）──
	// 表头含 Password 明文，是 PHP 的既有行为；
	// 这里的门禁必须与 PHP 的 isset($_SESSION[...administrator]) 等价。
	if id.Admin {
		ll, err := q.ListUserLoginLog(ctx, userID)
		if err != nil {
			return nil, err
		}
		v.LoginLog = ll
	}

	// ── 结果分布（饼图）──
	stats, err := q.ListUserResultStats(ctx, userID, exclude)
	if err != nil {
		return nil, err
	}
	v.ResultStats = make([]resultStatSlice, 0, len(stats))
	for _, st := range stats {
		v.ResultStats = append(v.ResultStats, resultStatSlice{
			Result: st.Result,
			Count:  st.Count,
			Label:  judgeResultLabel(st.Result),
		})
	}

	// ── 比赛战绩 ──
	contests, err := q.ListUserContestStats(ctx, userID, s.cfg.NoIPKeyword)
	if err != nil {
		return nil, err
	}
	v.Contests = make([]userContestRow, 0, len(contests))
	for _, c := range contests {
		row := userContestRow{
			ContestID: c.ContestID,
			Title:     c.Title.String,
			Tried:     c.Tried,
			AC:        c.AC,
		}
		if c.StartTime.Valid {
			row.StartTime = c.StartTime.Time.Format("2006-01-02 15:04:05")
		}
		v.Contests = append(v.Contests, row)
		v.TotalTry += c.Tried
		v.TotalContAC += c.AC
	}

	// ── 访客已通过的题（决定色块颜色）──
	viewerAC := map[int32]bool{}
	if id.LoggedIn {
		ids, err := q.ListViewerACProblems(ctx, id.UserID, exclude)
		if err != nil {
			return nil, err
		}
		for _, pid := range ids {
			viewerAC[pid] = true
		}
	}

	// ── 已通过 / 未通过的题 ──
	acList, err := q.ListUserACProblems(ctx, userID, exclude)
	if err != nil {
		return nil, err
	}
	acSet := make(map[int32]bool, len(acList))
	v.ACProblems = make([]userProblemChip, 0, len(acList))
	for _, p := range acList {
		acSet[p.ProblemID] = true
		v.ACProblems = append(v.ACProblems, userProblemChip{
			ProblemID:    p.ProblemID,
			Count:        p.Count,
			ViewerPassed: viewerAC[p.ProblemID],
		})
	}

	bad, err := q.ListUserNonACProblems(ctx, userID, exclude)
	if err != nil {
		return nil, err
	}
	// PHP 模板用 $ac（已通过题号数组）做差集，
	// 所以既 AC 过又有 WA 的题只出现在"通过"区。
	for _, p := range bad {
		if acSet[p.ProblemID] {
			continue
		}
		v.BadProblems = append(v.BadProblems, userProblemChip{
			ProblemID:    p.ProblemID,
			Count:        p.Count,
			ViewerPassed: viewerAC[p.ProblemID],
		})
	}

	// ── 日历热力图 ──
	chart, err := q.ListUserSubChart(ctx, userID, exclude)
	if err != nil {
		return nil, err
	}
	for _, pt := range chart {
		v.SubChart = append(v.SubChart, subChartPoint{Date: pt.Date, Count: pt.Count})
		if pt.Count > v.MaxSubCount {
			v.MaxSubCount = pt.Count
		}
	}

	return v, nil
}

// gravatarURL 生成头像地址，对应模板 userinfo.php:37-44。
//
// 默认走 gravatar；邮箱是 @qq.com 时换成 QQ 头像接口。
// 这里不做任何出站请求 —— 头像由浏览器自己去取，
// 与 starred() 不同，那个是服务端同步调用，必须去掉。
//
// 返回值声明为 template.URL：它要落进 <img src>，经过 html/template
// 的属性转义与 URL 过滤后仍可能被重写成 #ZgotmplZ，
// 而这里拼出来的地址是我们自己造的常量域名加已转义的查询串。
func gravatarURL(email string) template.URL {
	if email == "" {
		return template.URL("https://www.gravatar.com/avatar/?d=&s=500")
	}
	if i := strings.Index(strings.ToLower(email), "@qq.com"); i > 0 {
		// stripos 返回位置后取 [0,$i) 作为 QQ 号。
		return template.URL("https://q1.qlogo.cn/g?b=qq&nk=" + urlEncode(email[:i]) + "&s=5")
	}
	return template.URL("https://www.gravatar.com/avatar/" +
		md5Hex(strings.ToLower(strings.TrimSpace(email))) + "?d=&s=500")
}

// userInfoErrorView 是"查无此人"的错误卡。
type userInfoErrorView struct {
	Message string
}

// registerUserInfoRoutes 挂载档案页路由。
func (s *Server) registerUserInfoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /userinfo.php", s.handleUserInfo)
}
