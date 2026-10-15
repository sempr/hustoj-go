// 提交页与提交处理，对应 /submitpage.php 与 /submit.php。
//
// 迁移期这两个路由必须**同批**切到 Go，原因只有一个：CSRF。
// PHP 的 submitpage.php 把 token 写进 PHPSESSID 的 csrf_keys，
// 而 Go 用 nosurf 的双提交 Cookie 校验。两套 token 互不相认，
// 只切 POST /submit.php 的话，用户从 PHP 渲染的表单提交会被 nosurf
// 无条件拒绝 —— 表现为"点提交就提示页面已过期"。
// 这与当初 /vcode.php 必须和登录页一起切是同一个道理。
//
// 写库顺序的约束见 pkg/repository/submit.go：先插 result=14，
// 源码落库后才推进到 result=0，两步同事务。

package web

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
	"github.com/sempr/hustoj-go/pkg/repository"
)

// 语言后缀（prepend/append 模板与判题文件名的依据）由注册表提供，见
// languages.go 的 languageExtAt；索引必须与 all.toml 的 id 逐项对齐 ——
// 错位会让 A 语言的提交被拼上 B 语言的模板，编译错误极难定位。

// submitInput 是从 POST 解析出的提交参数。
type submitInput struct {
	// ProblemID 提交时携带的题目 id。
	//
	// PHP 用 `id`（练习）或 `pid`（比赛内序号）两种字段名，统一在这里
	// 归一。测试运行的负号在 TestRun 里体现，ProblemID 一律存绝对值。
	ProblemID int32

	// ContestID / Num 只在比赛模式有意义。
	ContestID int32
	Num       int32

	// IsContest 区分两种模式。
	IsContest bool

	// TestRun 标记这是提交页的"测试运行"按钮发来的请求。
	//
	// 判定规则（submit.php:58/66）：客户端把 id/cid 取负后再 POST，
	// 所以负号就是"测试运行"的唯一信号。练习模式看 id<=0，
	// 比赛模式看 cid<0。
	//
	// 必须显式带着走完整个判定流程：比赛模式下 ProblemID 恒为 0
	// （走 contest_problem 查真实 id），任何"从 ProblemID 符号推断
	// 是否测试运行"的写法都会把每一场正常比赛提交误判成测试运行，
	// 后果是全站比赛提交都不进判题队列。
	TestRun bool

	// Language 是语言下标。
	Language int32

	// Source 是用户提交的原文。
	Source string

	// InputText 是测试运行的自定义输入。
	InputText string

	// Encoded 表示 source 是 base64 编码的（encoded_submit 字段）。
	Encoded bool

	// VCode 是图形验证码答案。
	VCode string

	// Ajax 报告请求是否来自 AJAX 提交。
	//
	// PHP 用 $_GET['ajax'] 判断，决定是回显提交 ID 还是重定向到 status。
	Ajax bool
}

// submitResult 是一次提交判定后的结果。
type submitResult struct {
	// SolutionID 是新提交 ID，Ajax 模式下回显给前端。
	SolutionID int64

	// RedirectTo 是非 Ajax 模式的重定向目标。
	RedirectTo string
}

// submitDeny 表示一次提交被拒绝的原因。
type submitDeny int

const (
	submitOK submitDeny = iota
	// submitNeedLogin 未登录。PHP 渲染"请先登录"错误页。
	submitNeedLogin
	// submitVCodeWrong 图形验证码错误。
	submitVCodeWrong
	// submitProblemMissing 题目不存在。
	submitProblemMissing
	// submitProblemDefunct 题目已删除且提交者无管理权限。
	submitProblemDefunct
	// submitNotInContest 比赛未在进行中。
	submitNotInContest
	// submitNotInvited 私有比赛未受邀。
	submitNotInvited
	// submitNoProblemInContest 比赛内该序号没有题目。
	submitNoProblemInContest
	// submitLanguageForbidden 语言不在该比赛/站点的允许范围内。
	submitLanguageForbidden
	// submitTooShort 源码过短。
	submitTooShort
	// submitTooLong 源码超长。
	submitTooLong
	// submitCooldown 冷却期内已有提交。
	submitCooldown
	// submitEmpty 缺少必要参数。
	submitEmpty
)

func (d submitDeny) message() string {
	switch d {
	case submitNeedLogin:
		return "请先登录"
	case submitVCodeWrong:
		return "图形验证码不正确"
	case submitProblemMissing:
		return "题目不存在"
	case submitProblemDefunct:
		return "题目已删除"
	case submitNotInContest:
		return "比赛未在进行中"
	case submitNotInvited:
		return "您不在此比赛的邀请名单中"
	case submitNoProblemInContest:
		return "比赛中没有这道题"
	case submitLanguageForbidden:
		return "您提交的语言在该比赛中不可用"
	case submitTooShort:
		return "源码太短"
	case submitTooLong:
		return "源码太长"
	case submitCooldown:
		return "提交过于频繁，请稍后再试"
	default:
		return "提交失败"
	}
}

// handleSubmitPage 渲染提交页 GET /submitpage.php。
//
// 页面本身只做两件事：给出题目上下文（练习的题号或比赛的序号+题号）
// 与可选语言列表。真正的准入判定在 POST 时重做一遍 —— 不能信任
// GET 阶段的结果，用户可以绕过页面直接 POST。
func (s *Server) handleSubmitPage(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !identity.LoggedIn {
		s.renderPage(w, r, http.StatusOK, "alertAndBack", "请先登录", alertView{
			Message: "请先登录后再提交代码",
		})
		return
	}

	q := r.URL.Query()
	view := submitPageView{
		Token:     r,
		Languages: s.allowedLanguages(s.cfg.LangMask),
		Ajax:      q.Get("ajax") != "",
	}

	if rawCid := q.Get("cid"); rawCid != "" {
		cid := int32(abs(intval(rawCid)))
		num := int32(intval(q.Get("pid")))
		pid, err := s.queries().GetContestProblemID(r.Context(), dbgen.GetContestProblemIDParams{
			ContestID: sql.NullInt32{Int32: cid, Valid: true},
			Num:       num,
		})
		if err != nil {
			s.renderPage(w, r, http.StatusOK, "alertAndBack", "提交", alertView{
				Message: "比赛中没有这道题",
			})
			return
		}
		view.IsContest = true
		view.ContestID = cid
		view.Num = num
		view.ProblemID = pid
	} else {
		id := int32(intval(q.Get("id")))
		if id <= 0 {
			s.renderPage(w, r, http.StatusOK, "alertAndBack", "提交", alertView{
				Message: "缺少题目编号",
			})
			return
		}
		view.ProblemID = id
	}

	s.renderPage(w, r, http.StatusOK, "submitPage", "提交代码", view)
}

// allowedLanguages 列出掩码允许的语言。
//
// PHP 的掩码约定是"命中即拒绝"（$langmask & (1 << $language)），
// 所以可用语言是掩码里为 0 的那些位。
func (s *Server) allowedLanguages(mask int32) []submitLanguage {
	var out []submitLanguage
	for i := 0; i < languageCount(); i++ {
		if mask&(1<<uint(i)) != 0 {
			continue
		}
		out = append(out, submitLanguage{
			ID:   int32(i),
			Name: languageName(int32(i)),
		})
	}
	return out
}

// handleSubmit 处理 POST /submit.php。
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !identity.LoggedIn {
		s.renderSubmitDeny(w, r, submitNeedLogin)
		return
	}

	in, err := parseSubmitInput(r)
	if err != nil {
		s.renderSubmitDeny(w, r, submitEmpty)
		return
	}

	// 提交冷却。PHP 在取题之后检查，这里放在最前面：冷却命中时 PHP
	// 不写任何数据，直接跳回 status，提前 return 与之一致。
	if s.inSubmitCooldown(r.Context(), identity.UserID) {
		s.finishSubmit(w, r, in, submitResult{RedirectTo: s.statusURL(identity.UserID, in.ContestID, 0)})
		return
	}

	// 队列堆积时对提交也启用图形验证码（submit.php:26-29）。
	if s.submitNeedsVCode(r.Context()) {
		sid := identity.SessionID
		if sid == "" {
			// 未登录态不会有 Go 会话，但验证码答案必须落在某个会话里，
			// 否则没人能答对。这里按匿名会话兜底建一个。
			// 正常路径下 identity.LoggedIn 已经保证有 sid，
			// 这段只是防御 Redis 被清空后的边界情况。
			newSID, err := s.startGoSession(w, r)
			if err != nil {
				s.log.Error("创建验证码会话失败", "err", err)
				s.renderSubmitDeny(w, r, submitVCodeWrong)
				return
			}
			sid = newSID
		}
		if !s.checkVCode(r.Context(), sid, in.VCode) {
			s.markVFail(r.Context(), sid)
			s.renderSubmitDeny(w, r, submitVCodeWrong)
			return
		}
		_ = s.markVFailPassed(r.Context(), sid)
	}

	res, deny := s.submitOnce(w, r, identity, in)
	if deny != submitOK {
		s.renderSubmitDeny(w, r, deny)
		return
	}
	s.finishSubmit(w, r, in, res)
}

// submitOnce 执行一次提交的判定与写入。
func (s *Server) submitOnce(w http.ResponseWriter, r *http.Request, identity Identity, in submitInput) (submitResult, submitDeny) {
	ctx := r.Context()
	now := time.Now()

	// 语言下标越界时 PHP 归零（submit.php:164-166），
	// 随后会被掩码检查拦下，除非 0 号语言恰好可用。
	if in.Language < 0 || int(in.Language) >= languageCount() {
		in.Language = 0
	}

	langmask := s.cfg.LangMask
	var problemID int32
	var defunct string

	if in.IsContest {
		// 比赛必须正在进行。GetContestForSubmit 内含
		// start_time<=now AND end_time>now，查不到即"不在比赛中"。
		contest, err := s.queries().GetContestForSubmit(ctx, dbgen.GetContestForSubmitParams{
			ContestID: in.ContestID,
			Now:       sql.NullTime{Time: now, Valid: true},
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return submitResult{}, submitNotInContest
			}
			s.log.Error("查询比赛失败", "err", err, "contest_id", in.ContestID)
			return submitResult{}, submitNotInContest
		}

		// 私有比赛需要受邀或管理员身份。
		//
		// PHP 判的是 session 里的 _c<cid>，这里落到 privilege 表：
		// "c<cid>"（参赛）与 "m<cid>"（管理）两者之一即放行。
		if contest.Private != 0 && !identity.Admin {
			n, err := s.queries().CountContestInvites(ctx, dbgen.CountContestInvitesParams{
				UserID:          identity.UserID,
				RightstrContest: "c" + strconv.Itoa(int(in.ContestID)),
				RightstrManage:  "m" + strconv.Itoa(int(in.ContestID)),
			})
			if err != nil {
				s.log.Error("查询比赛邀请失败", "err", err, "contest_id", in.ContestID)
				return submitResult{}, submitNotInvited
			}
			if n == 0 {
				return submitResult{}, submitNotInvited
			}
		}

		// 比赛自带的语言掩码收紧可用范围。
		if contest.Langmask != 0 {
			langmask |= int32(contest.Langmask)
		}

		pid, err := s.queries().GetContestSubmitProblem(ctx, dbgen.GetContestSubmitProblemParams{
			ContestID: sql.NullInt32{Int32: in.ContestID, Valid: true},
			Num:       in.Num,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return submitResult{}, submitNoProblemInContest
			}
			s.log.Error("查询比赛题目失败", "err", err, "contest_id", in.ContestID, "num", in.Num)
			return submitResult{}, submitNoProblemInContest
		}
		problemID = pid
		defunct = "N" // submit.php:65 的 SQL 把 defunct 写死为 'N'
	} else {
		// 练习模式。测试运行时 ProblemID 是绝对值，仍要查库：
		// 送判源码要读 data/<id>/ 下的 prepend/append，SPJ 判定也要用。
		// PHP 在这里不校验（它拿负 id 去查，必然查不到，然后跳过所有
		// 错误分支），于是留下一条 problem_id=0 的记录让判题机去读
		// 不存在的 data/0。校验存在性是把这条死路堵住。
		row, err := s.queries().GetSubmitProblem(ctx, in.ProblemID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return submitResult{}, submitProblemMissing
			}
			s.log.Error("查询题目失败", "err", err, "problem_id", in.ProblemID)
			return submitResult{}, submitProblemMissing
		}
		problemID = row.ProblemID
		defunct = row.Defunct
	}

	isTestRun := in.TestRun
	// 已删除题目（defunct!='N'）只对管理员/题目编辑/题目审核开放。
	// 比赛模式的 defunct 被 SQL 写死为 'N'，恒能通过。
	// PHP 的第 79 行对测试运行整体跳过这段检查，这里照做。
	if !isTestRun && defunct != "N" && !identity.canSubmitDefunct() {
		return submitResult{}, submitProblemDefunct
	}

	// 语言掩码：命中即拒绝。
	if langmask&(1<<uint(in.Language)) != 0 {
		return submitResult{}, submitLanguageForbidden
	}

	source := in.Source
	if in.Encoded {
		if decoded, derr := decodeBase64(source); derr == nil {
			source = decoded
		}
	}

	// 长度检查。PHP 用 strlen（字节）比 65536，下界是 2。
	//
	// 注意顺序：PHP 是先判 <2 再判 >65536，两者都渲染同一个错误模板，
	// 但消息不同（太短/太长），所以这里分开。
	if len(source) < 2 {
		return submitResult{}, submitTooShort
	}
	if s.cfg.MaxSourceLength > 0 && len(source) > s.cfg.MaxSourceLength {
		return submitResult{}, submitTooLong
	}

	// 送判源码：拼接 prepend/append，并可能补 PHP 的 coding 头。
	sendSource, err := s.buildJudgeSource(ctx, problemID, in.Language, source)
	if err != nil {
		s.log.Error("拼接题目模板代码失败", "err", err, "problem_id", problemID)
		return submitResult{}, submitEmpty
	}

	// 测试运行存真实 problem_id（不取负），由 repository 停在 result=14。
	// 取负会让判题机去找不存在的 data/<负数>，详见 SubmitParams.ProblemID。

	nick, err := s.submitterNick(ctx, identity)
	if err != nil {
		s.log.Error("查询用户昵称失败", "err", err, "user_id", identity.UserID)
		return submitResult{}, submitEmpty
	}

	// OI/NOIP 只保留最后一次提交。
	//
	// PHP 条件（submit.php:335）：
	//   contest_locked($cid,128) || (标题含 noip || contest_locked($cid,16))
	//                            && $OJ_OI_1_SOLUTION_ONLY && !$test_run
	// 注意 && 优先级高于 ||，且 contest_locked(128) 命中时
	// **不受** oi_one_solution_only 开关约束 —— 延时比赛总是只留最后一次。
	if in.IsContest && !isTestRun {
		if s.shouldDemoteContestSubmissions(ctx, in.ContestID) {
			affected, derr := repository.DemoteContestSubmissions(ctx, s.db, in.ContestID, identity.UserID, in.Num)
			if derr != nil {
				s.log.Error("降级历史提交失败", "err", derr)
				return submitResult{}, submitEmpty
			}
			if affected > 0 {
				s.recomputeProblemCounts(ctx, absInt32(problemID))
			}
		}
	}

	out, err := repository.InsertSubmission(ctx, s.db, repository.SubmitParams{
		ProblemID:  problemID,
		TestRun:    isTestRun,
		UserID:     identity.UserID,
		Nick:       nick,
		Language:   in.Language,
		IP:         remoteIP(r),
		CodeLength: int32(len(sendSource)),
		Source:     sendSource,
		SourceUser: source,
		InputText:  in.InputText,
		ContestID:  in.ContestID,
		Num:        in.Num,
		IsContest:  in.IsContest,
	})
	if err != nil {
		s.log.Error("提交入库失败", "err", err, "user_id", identity.UserID, "problem_id", problemID)
		return submitResult{}, submitEmpty
	}

	// 远程评测题目不进本地队列。
	//
	// 测试运行一律不走远程：PHP 第 372 行查 remote_oj 时 $id 已被置 0，
	// 必然查不到，所以测试运行在 PHP 下也永远是本地题。Go 这里存的是
	// 真实 id，若不跳过就会把一次测试运行转给远程评测站。
	if !isTestRun {
		remote, rerr := s.queries().GetProblemRemoteOJ(ctx, problemID)
		if rerr == nil && remote.Valid && remote.String != "" {
			if err := repository.MarkSolutionRemote(ctx, s.db, out.SolutionID, remote.String); err != nil {
				s.log.Error("标记远程评测失败", "err", err, "solution_id", out.SolutionID)
			}
		}
	}

	return submitResult{
		SolutionID: out.SolutionID,
		RedirectTo: s.statusURL(identity.UserID, in.ContestID, out.SolutionID),
	}, submitOK
}

// shouldDemoteContestSubmissions 复刻 submit.php:335 的判定。
//
// PHP 写的是：
//
//	contest_locked($cid, 128) ||
//	  ((标题含 noip || contest_locked($cid, 16)) && $OJ_OI_1_SOLUTION_ONLY && !$test_run)
//
// 三个细节都不能省：
//   - 128（延时）命中时**绕过** oi_one_solution_only 开关；
//   - 标题含关键字也要算，标题不在比赛模式的查询结果里，另查一次；
//   - 测试运行不降级。
func (s *Server) shouldDemoteContestSubmissions(ctx context.Context, cid int32) bool {
	now := time.Now()
	parsed := now
	keyword := "%" + s.cfg.NoIPKeyword + "%"

	locked128, err := s.queries().IsContestLocked(ctx, dbgen.IsContestLockedParams{
		ContestID: cid,
		Level:     sql.NullInt16{Int16: 128, Valid: true},
		Keyword:   sql.NullString{String: keyword, Valid: true},
		Now:       sql.NullTime{Time: parsed, Valid: true},
	})
	if err != nil {
		s.log.Error("查询比赛锁榜状态失败", "err", err, "contest_id", cid)
	}
	if locked128 {
		return true
	}
	if !s.cfg.OIOneSolutionOnly {
		return false
	}

	// 标题含关键字（NOIP 模式）。
	contest, err := s.queries().GetContestByID(ctx, cid)
	if err == nil && strings.Contains(
		strings.ToLower(contest.Title.String), strings.ToLower(s.cfg.NoIPKeyword)) {
		return true
	}

	locked16, err := s.queries().IsContestLocked(ctx, dbgen.IsContestLockedParams{
		ContestID: cid,
		Level:     sql.NullInt16{Int16: 16, Valid: true},
		Keyword:   sql.NullString{String: keyword, Valid: true},
		Now:       sql.NullTime{Time: parsed, Valid: true},
	})
	if err != nil {
		s.log.Error("查询比赛 NOIP 状态失败", "err", err, "contest_id", cid)
	}
	return locked16
}

// recomputeProblemCounts 在降级历史提交后重算题目的 AC 数与提交数。
//
// 对应 submit.php:337-341 的两条 UPDATE（PHP 写成多表 UPDATE + 派生表，
// 这里拆成"先 SELECT 计数再 UPDATE"，理由见 schema/queries/solution.sql）。
func (s *Server) recomputeProblemCounts(ctx context.Context, problemID int32) {
	accepted, err := s.queries().CountAcceptedForProblem(ctx, problemID)
	if err != nil {
		s.log.Error("重算 AC 数失败", "err", err, "problem_id", problemID)
		return
	}
	if err := s.queries().SetProblemAccepted(ctx, dbgen.SetProblemAcceptedParams{
		Accepted:  sql.NullInt32{Int32: int32(accepted), Valid: true},
		ProblemID: problemID,
	}); err != nil {
		s.log.Error("写回 AC 数失败", "err", err, "problem_id", problemID)
	}

	submits, err := s.queries().CountSubmitsForProblem(ctx, problemID)
	if err != nil {
		s.log.Error("重算提交数失败", "err", err, "problem_id", problemID)
		return
	}
	if err := s.queries().SetProblemSubmit(ctx, dbgen.SetProblemSubmitParams{
		Submit:    sql.NullInt32{Int32: int32(submits), Valid: true},
		ProblemID: problemID,
	}); err != nil {
		s.log.Error("写回提交数失败", "err", err, "problem_id", problemID)
	}
}

// buildJudgeSource 拼出送进编译器的源码。
//
// 复刻 submit.php:219-252：
//  1. 若题目目录有 solution.name 而上传文件名不符，源码被替换成一句提示
//     （Scratch3 的 .sb3 上传场景）；
//  2. prepend.<ext> + "\n" + source（$OJ_APPENDCODE 开启时）；
//  3. append.<ext>；
//  4. 语言在注册表里配置了 source_header 且题目非 SPJ 时，补上该头
//     （注册表里 Python 配为 "# coding=utf-8"）。
//
// 注意顺序：header 在拼接 prepend/append **之前**加进源码。
func (s *Server) buildJudgeSource(ctx context.Context, problemID int32, language int32, source string) (string, error) {
	if hdr := languageSourceHeader(language); hdr != "" {
		if spj, err := s.queries().GetProblemSPJ(ctx, absInt32(problemID)); err == nil && spj != "2" {
			source = hdr + "\n" + source
		}
	}

	if !s.cfg.AppendCode {
		return source, nil
	}

	ext, ok := languageExtAt(language)
	if !ok {
		return source, nil
	}
	dir := filepath.Join(s.cfg.OJHome, "data", strconv.Itoa(int(absInt32(problemID))))
	if prepend, err := os.ReadFile(filepath.Join(dir, "prepend."+ext)); err == nil {
		source = string(prepend) + "\n" + source
	}
	if appendCode, err := os.ReadFile(filepath.Join(dir, "append."+ext)); err == nil {
		source = source + "\n" + string(appendCode)
	}
	return source, nil
}

// submitterNick 取写入 solution.nick 的昵称。
//
// PHP 查不到用户时用 "Guest"，昵称为空时退回 user_id。
func (s *Server) submitterNick(ctx context.Context, identity Identity) (string, error) {
	nick, err := s.queries().GetUserNick(ctx, identity.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "Guest", nil
		}
		return "", err
	}
	if nick == "" {
		return identity.UserID, nil
	}
	return nick, nil
}

// inSubmitCooldown 判断是否处于提交冷却期。
//
// 对应 submit.php:283-288：冷却期内已有提交时，PHP 不报错也不写库，
// 直接把用户送回自己的 status 页。
func (s *Server) inSubmitCooldown(ctx context.Context, userID string) bool {
	if s.cfg.SubmitCooldown <= 0 {
		return false
	}
	since := time.Now().Add(-time.Duration(s.cfg.SubmitCooldown) * time.Second)
	_, err := s.queries().GetLastRecentSubmit(ctx, dbgen.GetLastRecentSubmitParams{
		UserID: userID,
		Since:  since,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Error("查询最近提交失败", "err", err, "user_id", userID)
		}
		return false
	}
	return true
}

// submitNeedsVCode 判断这次提交是否需要图形验证码。
//
// 对应 submit.php:26-29：待判队列（result<4）超过阈值即开启。
// 生产上判题机与 Web 同机，这个阈值实际起自保护作用。
func (s *Server) submitNeedsVCode(ctx context.Context) bool {
	if s.cfg.VCodePendingThreshold <= 0 {
		return false
	}
	n, err := s.queries().CountPendingJudges(ctx)
	if err != nil {
		s.log.Error("查询待判队列长度失败", "err", err)
		return false
	}
	return n > s.cfg.VCodePendingThreshold
}

// statusURL 拼提交后的跳转目标。
//
// 对应 submit.php:472-483：
//
//	status.php?user_id=<uid>[&spa][&cid=<cid>&top=<solution_id>&fixed=]
//
// &fixed= 是空值参数，PHP 用来让 status 页把新提交固定到可见位置。
// 少了它用户会以为提交没生效。
func (s *Server) statusURL(userID string, cid int32, solutionID int64) string {
	u := "status.php?user_id=" + userID
	if cid > 0 {
		u += fmt.Sprintf("&cid=%d&top=%d&fixed=", cid, solutionID)
	}
	return u
}

// parseSubmitInput 从请求解析提交参数。
func parseSubmitInput(r *http.Request) (submitInput, error) {
	// 限制请求体。源码上限 65536 字节，base64 后约 1.34 倍，
	// 再留出表单其余字段与 multipart 边界的余量。
	r.Body = http.MaxBytesReader(nil, r.Body, 4<<20)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		// 非 multipart 时退回 urlencoded（PHP 同时支持两种）。
		if err := r.ParseForm(); err != nil {
			return submitInput{}, err
		}
	}

	in := submitInput{
		Language:  int32(intval(r.PostFormValue("language"))),
		Source:    r.PostFormValue("source"),
		InputText: r.PostFormValue("input_text"),
		Encoded:   r.PostFormValue("encoded_submit") != "",
		VCode:     r.PostFormValue("vcode"),
		Ajax:      r.URL.Query().Get("ajax") != "",
	}
	in.InputText = strings.ReplaceAll(in.InputText, "\r\n", "\n")

	// 比赛模式：cid 与 pid 成对出现。
	// PHP 是 `isset($_POST['cid'])`，cid=0 也算"传了 cid"，
	// 但第 99 行的条件要求 cid!=0，于是 cid=0 落进练习分支且 id=0
	// （等价于对任意题目做测试运行）。
	if _, ok := r.PostForm["cid"]; ok {
		cid := int32(intval(r.PostFormValue("cid")))
		// cid<0 表示测试运行（submit.php:58-59），取绝对值作为真实比赛。
		if cid < 0 {
			cid = -cid
			in.TestRun = true
		}
		if cid == 0 {
			// 走练习分支且 id=0：PHP 第 152/159 行把 $id 置 0 并强制
			// test_run=true。这里 ProblemID 保持 0，如实反映这一点。
			in.IsContest = false
			in.TestRun = true
			return in, nil
		}
		in.IsContest = true
		in.ContestID = cid
		in.Num = int32(intval(r.PostFormValue("pid")))
		return in, nil
	}

	// 练习模式。id<0 是测试运行（submit.php:66），取绝对值作为真实题目。
	id := int32(intval(r.PostFormValue("id")))
	if id < 0 {
		id = -id
		in.TestRun = true
	}
	in.ProblemID = id
	return in, nil
}

// decodeBase64 解 base64 源码。
//
// PHP 用自己的 include/base64.php 里的 decode64，容忍非严格填充。
// 这里用标准库并容忍解码失败：失败时保留原文而不是丢弃提交 ——
// 用户点"提交"就消失是最难排查的一类问题。
func decodeBase64(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

// renderSubmitDeny 渲染提交被拒的错误页。
//
// PHP 一律 exit(0) 渲染 error.php，状态码仍是 200 —— 提交失败是
// 正常业务结果，不是资源不存在。这里保持 200。
func (s *Server) renderSubmitDeny(w http.ResponseWriter, r *http.Request, d submitDeny) {
	s.renderPage(w, r, http.StatusOK, "alertAndBack", d.message(), alertView{
		Message: d.message(),
	})
}

// finishSubmit 结束一次提交：Ajax 回显 ID，普通请求重定向到 status。
func (s *Server) finishSubmit(w http.ResponseWriter, r *http.Request, in submitInput, res submitResult) {
	if in.Ajax {
		// PHP 的 ajax 分支 ob_clean() 后回显提交 ID（submit.php:492）。
		// 冷却命中时回 -1（submit.php:293）。
		if res.SolutionID == 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("-1"))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strconv.FormatInt(res.SolutionID, 10)))
		return
	}
	http.Redirect(w, r, res.RedirectTo, http.StatusFound)
}

// registerSubmitRoutes 挂载提交相关路由。
//
// 两个路由必须同时注册：见文件头关于 CSRF 的说明。
func (s *Server) registerSubmitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /submitpage.php", s.handleSubmitPage)
	mux.HandleFunc("POST /submit.php", s.handleSubmit)
}

// submitLanguage 是提交页语言下拉框的一项。
type submitLanguage struct {
	// ID 是 $language_ext 的下标，提交时原样回传。
	ID int32
	// Name 是展示名。
	Name string
}

// submitPageView 是提交页模板的数据。
type submitPageView struct {
	// Token 是 nosurf 的 CSRF 值，模板用 csrfToken 渲染成隐藏字段。
	Token *http.Request

	// ProblemID 是提交的目标题目。
	ProblemID int32

	// IsContest 区分练习提交与比赛提交。
	//
	// 两者 POST 出去的字段名不同（id vs cid+pid），模板必须照 PHP 的
	// 约定发字段名，Go 处理端才能正确归一。
	IsContest bool

	// ContestID / Num 只在比赛模式有意义。
	ContestID int32
	Num       int32

	// Languages 是掩码允许的语言列表。
	Languages []submitLanguage

	// Ajax 报告该页是否由 AJAX 提交端渲染。
	//
	// 比赛页的"测试运行"按钮会把 action 带上 ?ajax，submit.php 据此
	// 改用 inline JS 回显 solution_id 而不是 302 跳转（submit.php:498）。
	Ajax bool
}
