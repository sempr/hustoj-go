// 比赛模式的可见性判定，对应 problem.php:53-110 与 contest-check.php。
//
// /problem.php 同时是练习（?id=）与比赛（?cid=&pid=）两种模式的入口。
// 比赛模式不能简单复用练习模式的查询：它的取题键是 contest_problem.num，
// 而且题目可见性取决于**比赛本身**的状态（时间窗、是否私有、是否关闭、
// 是否限制访问 IP 段），而不是题目表。
//
// 这里把 PHP 分散在 contest-check.php 与 problem.php 里的判断收拢成一处，
// 按 PHP 的原始顺序逐条判定，方便逐条写测试。

package web

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// 比赛准入的判定结果。
type ContestDeny int

const (
	// ContestAllowed 准入通过。
	ContestAllowed ContestDeny = iota
	// ContestNotFound 比赛不存在。
	ContestNotFound
	// ContestClosed 比赛已关闭（defunct='Y'），非管理员不可见。
	ContestClosed
	// ContestNotStarted 比赛尚未开始。
	ContestNotStarted
	// ContestPrivate 私有比赛且当前访问者未被邀请。
	ContestPrivate
	// ContestSubnet 访问者 IP 不在比赛允许的网段内。
	ContestSubnet
	// ContestLimited 个人限时赛：PHP 依赖 session + loginlog 计时，
	// Go 侧尚无 session 桥，无法可靠计时，按拒绝处理。
	ContestLimited
)

// ContestAccess 是一次比赛准入判定的结果。
type ContestAccess struct {
	Contest dbgen.GetContestByIDRow
	// IsOver 比赛已结束（contest-check.php:81）。
	//
	// 结束后 PHP 允许"按练习方式提交"，模板据此切换提交按钮的链接
	// （syzoj/problem.php:92-95），因此这个布尔量必须保留。
	IsOver bool
	Deny   ContestDeny
}

// OK 报告比赛是否准入。
func (a ContestAccess) OK() bool { return a.Deny == ContestAllowed }

// contestLimitKeyword 是"个人限时赛"的描述关键字。
//
// contest-check.php:100 用 isset 兜底为 "限时"：管理员可以在描述里写
// "限时90分钟"来给每个用户单独计时。我们没有对应的配置项，
// 固定按 PHP 的默认值处理。
const contestLimitKeyword = "限时"

// EvaluateContestAccess 判定访问者能否查看比赛，判定顺序复刻 PHP。
//
// 顺序很关键：PHP 先查 subnet（contest-check.php:43），再查 defunct（:49），
// 管理员/比赛创建者在 private 之后才把 contest_ok 强行置真（:77-78），
// 未开始比赛在最后才报错（:85-96）。任何一次置 false 都会被后面
// 的管理员覆盖救回，所以管理员实际上绕过除 subnet 外的全部检查。
//
// hasSession 表示请求带着 PHP 会话 cookie。Go 侧没有 session 桥，
// 因此无法区分"匿名"与"已登录但无权"：对需要 session 才能判定的分支
// （私有比赛的受邀标记、限时赛计时），有会话时一律按无法判定 → 拒绝，
// 宁可少放行也不能错放行（错放行等于泄露赛题或放过超时作答）。
//
// 注意自由练习开关**不参与**比赛准入判定：contest-check.php 的
// subnet/defunct/private/未开始检查都不看 OJ_FREE_PRACTICE，该开关在
// problem.php:65 只影响空结果时错误页的文案，且两种情况最终都是错误页。
// 若在这里放行会绕过私有比赛校验，属于越权。
func EvaluateContestAccess(ctx context.Context, q dbgen.FullQuerier, c dbgen.GetContestByIDRow, id Identity, now time.Time, remoteIP string, hasSession bool) (ContestAccess, error) {
	res := ContestAccess{Contest: c}

	// subnet 为空表示不限制（in_subnet_of_contest:265-276）。
	// PHP 对不存在的比赛让 in_subnet_of_contest 返回 null（falsy）→ 拒绝，
	// 所以这里把"比赛查不到"也归入 subnet 分支，与 PHP 一致。
	if !inContestSubnet(remoteIP, c.Subnet) {
		res.Deny = ContestSubnet
		if res.Deny == ContestSubnet && c.ContestID == 0 {
			res.Deny = ContestNotFound
		}
		return res, nil
	}

	// 管理员 / 比赛创建者 / 题目编辑者：contest-check.php:77 与 problem.php:80。
	canManage := id.CanManageContest()

	if c.Defunct == "Y" && !canManage {
		res.Deny = ContestClosed
		return res, nil
	}

	if !canManage {
		// 私有比赛需要 session 里的 _c<cid>（密码验证时写入）或 privilege 表授权。
		// 优先用 session 桥拿到的受邀标记；没有 session 或标记缺失时回退查库。
		if c.Private != 0 {
			invited := false
			if id.HasContestInvite(c.ContestID) {
				invited = true
			} else if id.UserID != "" {
				var err error
				invited, err = HasContestPrivilege(ctx, q, id.UserID, c.ContestID)
				if err != nil {
					return res, fmt.Errorf("查询比赛 %d 的授权失败: %w", c.ContestID, err)
				}
			}
			if !invited {
				res.Deny = ContestPrivate
				return res, nil
			}
		}

		// 个人限时赛：PHP 在 contest-check.php:101 记 loginlog 并按分钟数计时，
		// 未登录用户跳过（isset($_SESSION[_user_id]) 为假）。
		// Go 侧：若 session 里已有 m<cid> 标记说明计时已用完，拒绝；否则如有会话但无法计时则拒绝（fail-safe）。
		if hasContestTimeLimit(c.Description.String) && id.LoggedIn {
			if id.HasContestTimeLimitUsed(c.ContestID) {
				res.Deny = ContestLimited
				return res, nil
			}
			res.Deny = ContestLimited
			return res, nil
		}
	}

	// 未开始：contest-check.php:85-96，管理员不受限。
	if !canManage && c.StartTime.Valid && now.Before(c.StartTime.Time) {
		res.Deny = ContestNotStarted
		return res, nil
	}

	// 已结束：contest-check.php:81。管理员也不豁免这一条 —— 它只影响提交入口。
	res.IsOver = c.EndTime.Valid && now.After(c.EndTime.Time)
	return res, nil
}

// hasContestTimeLimit 判断比赛描述是否启用了个人限时赛。
//
// contest-check.php:101 用 str_contains 匹配关键字，因此只要描述里出现
// "限时"两个字就启用，包括 "不限时" 这种反向表述 —— 这是上游行为，
// 不做语义修正。
func hasContestTimeLimit(description string) bool {
	return strings.Contains(description, contestLimitKeyword)
}

// HasContestPrivilege 查询用户是否被邀请参加私有比赛。
func HasContestPrivilege(ctx context.Context, q dbgen.FullQuerier, userID string, contestID int32) (bool, error) {
	ok, err := q.HasContestPrivilege(ctx, dbgen.HasContestPrivilegeParams{
		UserID:   userID,
		Rightstr: "c" + strconv.Itoa(int(contestID)),
	})
	if err != nil {
		return false, err
	}
	return ok, nil
}

// inContestSubnet 判断 IP 是否落在比赛允许的网段里，对应 in_subnet_of_contest。
//
// subnet 为空 → 不限制。多个网段以逗号分隔，任一命中即通过。
// 网段格式为 a.b.c.d/n（is_ip_in_subnet:195 用 explode("/") 直接解析，
// 不写明位数时 mask 为空，PHP 会退化成字符串运算，这里按 0 位处理）。
func inContestSubnet(remoteIP, subnet string) bool {
	subnet = strings.TrimSpace(subnet)
	if subnet == "" {
		return true
	}
	if remoteIP == "" {
		return false
	}
	ip := net.ParseIP(remoteIP)
	if ip == nil {
		return false
	}
	for _, entry := range strings.Split(subnet, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ipInSubnet(ip, entry) {
			return true
		}
	}
	return false
}

// ipInSubnet 判断 IP 是否属于 CIDR 网段。
//
// 只处理 IPv4：PHP 的 ip2long/ip_to_integer 是 32 位实现，IPv6 会被压成
// 错误的整数。站点是 IPv4 站点（nginx 只 listen 80/443），不引入 IPv6 分支。
func ipInSubnet(ip net.IP, cidr string) bool {
	slash := strings.Index(cidr, "/")
	if slash < 0 {
		return false
	}
	base := net.ParseIP(strings.TrimSpace(cidr[:slash]))
	if base == nil {
		return false
	}
	bits, err := strconv.Atoi(strings.TrimSpace(cidr[slash+1:]))
	if err != nil || bits < 0 || bits > 32 {
		return false
	}
	base4, ip4 := base.To4(), ip.To4()
	if base4 == nil || ip4 == nil {
		return false
	}
	baseMask := net.CIDRMask(bits, 32)
	return base4.Mask(baseMask).Equal(ip4.Mask(baseMask))
}

// ContestProblem 取比赛里的某道题。
//
// 按 contest_problem.num 取题（problem.php:95-97），不按 problem_id。
// 注意这里不做任何 defunct 过滤：比赛模式下已删除的题目仍要能打开，
// 否则赛中改题会导致选手点不开题面。
func ContestProblem(ctx context.Context, q dbgen.FullQuerier, contestID, pid int32) (dbgen.Problem, error) {
	problemID, err := q.GetContestProblemID(ctx, dbgen.GetContestProblemIDParams{
		ContestID: sql.NullInt32{Int32: contestID, Valid: true},
		Num:       pid,
	})
	if err != nil {
		return dbgen.Problem{}, fmt.Errorf("比赛 %d 第 %d 题不存在: %w", contestID, pid, err)
	}
	row, err := q.GetProblemByID(ctx, problemID)
	if err != nil {
		return dbgen.Problem{}, fmt.Errorf("查询题目 %d 失败: %w", problemID, err)
	}
	return row, nil
}

// IsNoIPLocked 判断题目是否处于 NOIP 锁榜状态，对应 problem_locked($id, 28)。
//
// problem.php:151-157 的 $flag 分支按比赛标题关键字判断，
// 这里合并了两者：只要命中关键字或 contest_type 位即可。
func IsNoIPLocked(ctx context.Context, q dbgen.FullQuerier, problemID int32, keyword string, now time.Time) (bool, error) {
	locked, err := q.IsProblemLocked(ctx, dbgen.IsProblemLockedParams{
		ProblemID: problemID,
		Level:     sql.NullInt16{Int16: 28, Valid: true},
		Keyword:   LikeArg("%" + keyword + "%"),
		Now:       nowArg(now),
	})
	if err != nil {
		return false, fmt.Errorf("查询题目 %d 的锁榜状态失败: %w", problemID, err)
	}
	return locked, nil
}

// remoteIP 取访客 IP，用于比赛网段校验。
//
// 优先取 X-Real-IP / X-Forwarded-For 的首段：nginx 已配置
// X-Real-IP $remote_addr。伪造头只在绕过 nginx 直连回环端口时才可能，
// 而回环端口不对外监听；contest-check.php:110-118 用的是同样的信任模型。
func remoteIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return firstAddr(v)
	}
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		return firstAddr(v)
	}
	host, _, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// firstAddr 取逗号分隔列表的第一项。
func firstAddr(list string) string {
	if i := strings.Index(list, ","); i >= 0 {
		return strings.TrimSpace(list[:i])
	}
	return strings.TrimSpace(list)
}

// abs 取绝对值，对应 contest-check.php:4,9 的 abs(intval($cid))。
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// splitHostPort 从 host:port 里取 host，避免直接依赖 net.SplitHostPort 的错误分支。
func splitHostPort(addr string) (host, port string, err error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, "", fmt.Errorf("缺少端口")
	}
	return strings.Trim(addr[:i], "[]"), addr[i+1:], nil
}
