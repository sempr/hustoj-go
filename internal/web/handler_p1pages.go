// P1 用户页（online.php、recent-contest.php、export_ac_code.php、export_contest_code.php、comparesource.php、getsource.php、showsource2.php、contestrank2.php、contestrank3.php）

package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// ── online.php ───────────────────────────────────────────────────────

// OnlineUser 是在线用户。
type OnlineUser struct {
	UserID   string
	Password string
	IP       string
	Time     string
}

// OnlinePageVM 是在线用户页的视图模型。
type OnlinePageVM struct {
	Title  string
	OJName string
	Users  []OnlineUser
}

func (s *Server) handleOnline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	var users []OnlineUser
	var err error

	if identity.Admin {
		search := r.URL.Query().Get("search")
		if search != "" {
			users, err = s.getOnlineUsersByIP(ctx, "%"+search+"%")
		} else {
			users, err = s.getOnlineUsersExcludeSelf(ctx, identity.UserID)
		}
	} else {
		users, err = s.getOnlineUsers(ctx)
	}

	if err != nil {
		s.log.Error("查询在线用户失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	renderPage(w, http.StatusOK, "onlinePage", "在线用户", OnlinePageVM{
		Title:  "在线用户",
		OJName: s.cfg.OJName,
		Users:  users,
	})
}

func (s *Server) getOnlineUsers(ctx context.Context) ([]OnlineUser, error) {
	rows, err := s.queries().GetOnlineUsers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]OnlineUser, 0, len(rows))
	for _, r := range rows {
		ip := ""
		if r.Ip.Valid {
			ip = r.Ip.String
		}
		t := ""
		if r.Time.Valid {
			t = r.Time.Time.Format("2006-01-02 15:04:05")
		}
		result = append(result, OnlineUser{UserID: r.UserID, Password: r.Password.String, IP: ip, Time: t})
	}
	return result, nil
}

func (s *Server) getOnlineUsersByIP(ctx context.Context, search string) ([]OnlineUser, error) {
	rows, err := s.queries().GetOnlineUsersByIP(ctx, sql.NullString{String: search, Valid: true})
	if err != nil {
		return nil, err
	}
	result := make([]OnlineUser, 0, len(rows))
	for _, r := range rows {
		ip := ""
		if r.Ip.Valid {
			ip = r.Ip.String
		}
		t := ""
		if r.Time.Valid {
			t = r.Time.Time.Format("2006-01-02 15:04:05")
		}
		result = append(result, OnlineUser{UserID: r.UserID, Password: r.Password.String, IP: ip, Time: t})
	}
	return result, nil
}

func (s *Server) getOnlineUsersExcludeSelf(ctx context.Context, userID string) ([]OnlineUser, error) {
	rows, err := s.queries().GetOnlineUsersExcludeSelf(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]OnlineUser, 0, len(rows))
	for _, r := range rows {
		ip := ""
		if r.Ip.Valid {
			ip = r.Ip.String
		}
		t := ""
		if r.Time.Valid {
			t = r.Time.Time.Format("2006-01-02 15:04:05")
		}
		result = append(result, OnlineUser{UserID: r.UserID, Password: r.Password.String, IP: ip, Time: t})
	}
	return result, nil
}

// ── recent-contest.php ───────────────────────────────────────────────

// RecentContestItem 是最近比赛项。
type RecentContestItem struct {
	ContestID int32
	Title     string
	StartTime string
	EndTime   string
	Private   int8
	UserID    string
}

// RecentContestPageVM 是最近比赛页的视图模型。
type RecentContestPageVM struct {
	Title    string
	OJName   string
	Contests []RecentContestItem
}

func (s *Server) handleRecentContest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := s.queries().GetRecentContestsList(ctx)
	if err != nil {
		s.log.Error("查询最近比赛失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	contests := make([]RecentContestItem, 0, len(rows))
	for _, r := range rows {
		contests = append(contests, RecentContestItem{
			ContestID: r.ContestID,
			Title:     r.Title.String,
			StartTime: r.StartTime.Time.Format("2006-01-02 15:04:05"),
			EndTime:   r.EndTime.Time.Format("2006-01-02 15:04:05"),
			Private:   r.Private,
			UserID:    r.UserID,
		})
	}

	renderPage(w, http.StatusOK, "recentContestPage", "最近比赛", RecentContestPageVM{
		Title:    "最近比赛",
		OJName:   s.cfg.OJName,
		Contests: contests,
	})
}

// ── export_ac_code.php ───────────────────────────────────────────────

func (s *Server) handleExportACCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	if !identity.LoggedIn {
		renderPage(w, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">请先登录</a>",
		})
		return
	}

	rows, err := s.queries().GetACCodeByUser(ctx, identity.UserID)
	if err != nil {
		s.log.Error("查询 AC 代码失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/file")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ac-%s.txt\"", identity.UserID))
	fmt.Fprintf(w, "%s\r\n", identity.UserID)
	for _, row := range rows {
		fmt.Fprintf(w, "Problem%d:\r\n", row.ProblemID)
		fmt.Fprintf(w, "%s\r\n", strings.ReplaceAll(row.Source, "\n", "\r\n"))
		fmt.Fprintf(w, "------------------------------------------------------\r\n")
	}
}

// ── export_contest_code.php ──────────────────────────────────────────

func (s *Server) handleExportContestCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	if !identity.LoggedIn {
		renderPage(w, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">请先登录</a>",
		})
		return
	}

	cid := int32(0)
	if c := r.URL.Query().Get("cid"); c != "" {
		if n, err := strconv.ParseInt(c, 10, 32); err == nil {
			cid = int32(n)
		}
	}

	if !identity.Admin {
		hasPriv, err := HasContestPrivilege(ctx, s.queries(), identity.UserID, cid)
		if err != nil || !hasPriv {
			renderPage(w, http.StatusOK, "errorPage", "无权限", map[string]string{
				"Message": "<a href=\"/loginpage.php\">无权限</a>",
			})
			return
		}
	}

	rows, err := s.queries().GetContestCode(ctx, sql.NullInt32{Int32: cid, Valid: true})
	if err != nil {
		s.log.Error("查询比赛代码失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/file")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"logs-%d.txt\"", cid))
	for _, row := range rows {
		fmt.Fprintf(w, "%s:Problem%d:%s\r\n", row.UserID, row.ProblemID, judgeResultLabel(int32(row.Result)))
		fmt.Fprintf(w, "%s\r\n", row.Source)
		fmt.Fprintf(w, "------------------------------------------------------\r\n")
	}
}

// ── comparesource.php ────────────────────────────────────────────────

// CompareSourceVM 是代码对比页的视图模型。
type CompareSourceVM struct {
	Title       string
	OJName      string
	LeftSource  string
	RightSource string
	LeftInfo    SourceInfo
	RightInfo   SourceInfo
}

// SourceInfo 是源码信息。
type SourceInfo struct {
	UserID    string
	ProblemID int32
	Result    int16
	Time      int32
	Memory    int32
	Language  int32
}

func (s *Server) handleCompareSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	leftID := int32(0)
	if l := r.URL.Query().Get("left"); l != "" {
		if n, err := strconv.ParseInt(l, 10, 32); err == nil {
			leftID = int32(n)
		}
	}

	rightID := int32(0)
	if ri := r.URL.Query().Get("right"); ri != "" {
		if n, err := strconv.ParseInt(ri, 10, 32); err == nil {
			rightID = int32(n)
		}
	}

	if leftID == 0 {
		renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{
			"Message": "No such code!",
		})
		return
	}

	leftInfo, leftSource, err := s.getSourceInfo(ctx, leftID, identity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{"Message": "No such code!"})
			return
		}
		s.log.Error("查询左侧源码失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	rightInfo, rightSource, err := s.getSourceInfo(ctx, rightID, identity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{"Message": "No such code!"})
			return
		}
		s.log.Error("查询右侧源码失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	renderPage(w, http.StatusOK, "compareSourcePage", "代码对比", CompareSourceVM{
		Title:       "代码对比",
		OJName:      s.cfg.OJName,
		LeftSource:  leftSource,
		RightSource: rightSource,
		LeftInfo:    leftInfo,
		RightInfo:   rightInfo,
	})
}

func (s *Server) getSourceInfo(ctx context.Context, solutionID int32, identity Identity) (SourceInfo, string, error) {
	sol, err := s.queries().GetSolutionByID(ctx, uint32(solutionID))
	if err != nil {
		return SourceInfo{}, "", err
	}

	canView := sol.UserID == identity.UserID || identity.SourceBrowser || identity.Admin
	if !canView {
		return SourceInfo{}, "I am sorry, You could not view this code!", nil
	}

	source, err := s.queries().GetSourceCodeForSolution(ctx, solutionID)
	if err != nil {
		return SourceInfo{}, "No source code available!", nil
	}

	info := SourceInfo{
		UserID:    sol.UserID,
		ProblemID: sol.ProblemID,
		Result:    sol.Result,
		Time:      sol.Time,
		Memory:    sol.Memory,
		Language:  int32(sol.Language),
	}
	return info, source, nil
}

// ── getsource.php ────────────────────────────────────────────────────

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	id := int32(0)
	if i := r.URL.Query().Get("id"); i != "" {
		if n, err := strconv.ParseInt(i, 10, 32); err == nil {
			id = int32(n)
		}
	}

	if id == 0 {
		renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{
			"Message": "No such code!",
		})
		return
	}

	sol, err := s.queries().GetSolutionByID(ctx, uint32(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{"Message": "No such code!"})
			return
		}
		s.log.Error("查询源码失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	canView := sol.UserID == identity.UserID || identity.SourceBrowser || identity.Admin
	if !canView {
		fmt.Fprint(w, "I am sorry, You could not view this code!")
		return
	}

	source, err := s.queries().GetSourceCodeForSolution(ctx, id)
	if err != nil {
		fmt.Fprint(w, "No source code available!")
		return
	}

	brush := languageName(int32(sol.Language))
	if brush == "Pascal" {
		brush = "delphi"
	} else if brush == "Obj-C" {
		brush = "c"
	} else if brush == "FreeBasic" {
		brush = "vb"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "/**************************************************************\n")
	fmt.Fprintf(w, "\tProblem: %d\n\tUser: %s\n\tNick: %s\n", sol.ProblemID, sol.UserID, sol.Nick)
	fmt.Fprintf(w, "\tLanguage: %s\n\tResult: %s\n", languageName(int32(sol.Language)), judgeResultLabel(int32(sol.Result)))
	if sol.Result == 4 {
		fmt.Fprintf(w, "\tTime: %d ms\n", sol.Time)
		fmt.Fprintf(w, "\tMemory: %d kb\n", sol.Memory)
	}
	fmt.Fprintf(w, "****************************************************************/\n\n")
	fmt.Fprintf(w, "%s\n", strings.ReplaceAll(source, "\n\r", "\n"))
}

// ── showsource2.php ──────────────────────────────────────────────────

func (s *Server) handleShowSource2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	id := int32(0)
	if i := r.URL.Query().Get("id"); i != "" {
		if n, err := strconv.ParseInt(i, 10, 32); err == nil {
			id = int32(n)
		}
	}

	if id == 0 {
		renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{
			"Message": "No such code!",
		})
		return
	}

	sol, err := s.queries().GetSolutionByID(ctx, uint32(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			renderPage(w, http.StatusOK, "errorPage", "代码不存在", map[string]string{"Message": "No such code!"})
			return
		}
		s.log.Error("查询源码失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	if sol.ContestID.Valid && sol.ContestID.Int32 > 0 && !identity.SourceBrowser && !identity.Admin {
		contestID := sol.ContestID.Int32
		contest, err := s.queries().GetContestByID(ctx, contestID)
		if err == nil {
			now := time.Now()
			if contest.EndTime.Time.After(now) {
				renderPage(w, http.StatusOK, "errorPage", "比赛进行中", map[string]string{
					"Message": fmt.Sprintf("<h3>比赛编号: %d</h3><p>比赛进行中，无法查看源码</p>", contestID),
				})
				return
			}
		}
	}

	canView := sol.UserID == identity.UserID || identity.SourceBrowser || identity.Admin
	if !canView {
		fmt.Fprint(w, "I am sorry, You could not view this code!")
		return
	}

	source, err := s.queries().GetSourceCodeForSolution(ctx, id)
	if err != nil {
		fmt.Fprint(w, "No source code available!")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "%s\n", strings.ReplaceAll(source, "\n\r", "\n"))
}

// ── contestrank2.php / contestrank3.php ───────────────────────────────

// ContestRankVM 是滚榜页的视图模型。
type ContestRankVM struct {
	Title       string
	OJName      string
	CID         int32
	ContestName string
	StartTime   string
	EndTime     string
	LockTime    string
	Rankings    []ContestRankRow
	FirstBlood  map[int32]string
}

// ContestRankRow 是比赛排名行。
type ContestRankRow struct {
	Rank           int
	UserID         string
	Solved         int
	Time           int
	ProblemResults []ProblemResult
}

// ProblemResult 是题目结果。
type ProblemResult struct {
	Num      int32
	Attempts int
	ACTime   int
}

func (s *Server) handleContestRank2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	cid := int32(0)
	if c := r.URL.Query().Get("cid"); c != "" {
		if n, err := strconv.ParseInt(c, 10, 32); err == nil {
			cid = int32(n)
		}
	}

	if cid == 0 {
		renderPage(w, http.StatusOK, "errorPage", "比赛不存在", map[string]string{"Message": "No Such Contest!"})
		return
	}

	contest, err := s.queries().GetContestInfo(ctx, cid)
	if err != nil {
		s.log.Error("查询比赛信息失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	startTime := contest.StartTime.Time
	endTime := contest.EndTime.Time
	now := time.Now()

	if startTime.After(now) {
		renderPage(w, http.StatusOK, "errorPage", "比赛未开始", map[string]string{"Message": "比赛尚未开始!"})
		return
	}

	title := ""
	if contest.Title.Valid {
		title = contest.Title.String
	}
	noip := endTime.After(now) && strings.Contains(strings.ToLower(title), strings.ToLower(s.cfg.NoIPKeyword))
	if noip && !identity.Admin && !identity.SourceBrowser {
		renderPage(w, http.StatusOK, "errorPage", "比赛进行中", map[string]string{"Message": s.noipWarning()})
		return
	}

	lockPercent := s.cfg.RankLockPercent
	if lp := r.URL.Query().Get("lock_percent"); lp != "" {
		if f, err := strconv.ParseFloat(lp, 64); err == nil {
			lockPercent = f
		}
	}
	lockTime := endTime.Add(-time.Duration(float64(endTime.Sub(startTime)) * lockPercent))

	submissions, err := s.queries().GetContestSubmissions(ctx, sql.NullInt32{Int32: cid, Valid: true})
	if err != nil {
		s.log.Error("查询比赛提交失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	firstBloodRows, err := s.queries().GetFirstBlood(ctx, sql.NullInt32{Int32: cid, Valid: true})
	if err != nil {
		s.log.Error("查询首杀失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	firstBlood := make(map[int32]string)
	for _, fb := range firstBloodRows {
		firstBlood[int32(fb.Num)] = fb.UserID
	}

	rankings := calculateContestRank(submissions, startTime, lockTime, now, s.cfg.CEPenalty)

	renderPage(w, http.StatusOK, "contestRankPage", "滚榜", ContestRankVM{
		Title:       "滚榜",
		OJName:      s.cfg.OJName,
		CID:         cid,
		ContestName: title,
		StartTime:   startTime.Format("2006-01-02 15:04:05"),
		EndTime:     endTime.Format("2006-01-02 15:04:05"),
		LockTime:    lockTime.Format("2006-01-02 15:04:05"),
		Rankings:    rankings,
		FirstBlood:  firstBlood,
	})
}

func (s *Server) handleContestRank3(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	cid := int32(0)
	if c := r.URL.Query().Get("cid"); c != "" {
		if n, err := strconv.ParseInt(c, 10, 32); err == nil {
			cid = int32(n)
		}
	}

	if cid == 0 {
		renderPage(w, http.StatusOK, "errorPage", "比赛不存在", map[string]string{"Message": "No Such Contest"})
		return
	}

	if !identity.Admin {
		hasPriv, err := HasContestPrivilege(ctx, s.queries(), identity.UserID, cid)
		if err != nil || !hasPriv {
			fmt.Fprint(w, "<a href='../loginpage.php'>Please Login First!</a>")
			return
		}
	}

	contest, err := s.queries().GetContestInfo(ctx, cid)
	if err != nil {
		s.log.Error("查询比赛信息失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	startTime := contest.StartTime.Time
	endTime := contest.EndTime.Time
	now := time.Now()

	if startTime.After(now) {
		renderPage(w, http.StatusOK, "errorPage", "比赛未开始", map[string]string{"Message": "比赛尚未开始!"})
		return
	}

	if endTime.After(now) {
		renderPage(w, http.StatusOK, "errorPage", "比赛未结束", map[string]string{"Message": "Contest Not Finished!"})
		return
	}

	list := r.URL.Query().Get("list")
	if list == "submit" {
		submissions, err := s.queries().GetContestSubmissions(ctx, sql.NullInt32{Int32: cid, Valid: true})
		if err != nil {
			s.log.Error("查询提交失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		problemMap, err := s.queries().GetContestProblemMap(ctx, sql.NullInt32{Int32: cid, Valid: true})
		if err != nil {
			s.log.Error("查询题目映射失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		pmap := make(map[int32]string)
		for _, pm := range problemMap {
			pmap[pm.ProblemID] = string(rune('A' + pm.Num))
		}

		w.Header().Set("Content-Type", "application/json")
		result := make([]map[string]interface{}, 0, len(submissions))
		for _, sub := range submissions {
			result = append(result, map[string]interface{}{
				"submitID":   sub.SolutionID,
				"userID":     sub.UserID,
				"alphabetID": pmap[sub.ProblemID],
				"subTime":    sub.InDate.Format("2006-01-02 15:04:05"),
				"resultID":   changeResult(sub.Result),
			})
		}
		json.NewEncoder(w).Encode(result)
		return
	}

	if list == "team" {
		teams, err := s.queries().GetContestTeams(ctx, sql.NullInt32{Int32: cid, Valid: true})
		if err != nil {
			s.log.Error("查询队伍失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		result := make([]map[string]interface{}, 0, len(teams))
		for _, t := range teams {
			result = append(result, map[string]interface{}{
				"userID":   t.UserID,
				"nickName": t.Nick,
				"realName": "",
				"official": 1,
			})
		}
		json.NewEncoder(w).Encode(result)
		return
	}

	lockPercent := s.cfg.RankLockPercent
	if lp := r.URL.Query().Get("lock_percent"); lp != "" {
		if f, err := strconv.ParseFloat(lp, 64); err == nil {
			lockPercent = f
		}
	}
	lockTime := endTime.Add(-time.Duration(float64(endTime.Sub(startTime)) * lockPercent))

	submissions, err := s.queries().GetContestSubmissions(ctx, sql.NullInt32{Int32: cid, Valid: true})
	if err != nil {
		s.log.Error("查询比赛提交失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	rankings := calculateContestRank(submissions, startTime, lockTime, now, s.cfg.CEPenalty)

	renderPage(w, http.StatusOK, "contestRank3Page", "滚榜", ContestRankVM{
		Title:       "滚榜",
		OJName:      s.cfg.OJName,
		CID:         cid,
		ContestName: contest.Title.String,
		StartTime:   startTime.Format("2006-01-02 15:04:05"),
		EndTime:     endTime.Format("2006-01-02 15:04:05"),
		LockTime:    lockTime.Format("2006-01-02 15:04:05"),
		Rankings:    rankings,
	})
}

func calculateContestRank(submissions []dbgen.GetContestSubmissionsRow, startTime, lockTime, now time.Time, cePenalty bool) []ContestRankRow {
	type problemStat struct {
		attempts int
		acTime   int
	}
	type userStat struct {
		userID   string
		solved   int
		time     int
		problems map[int32]*problemStat
	}

	users := make(map[string]*userStat)
	var order []string

	for _, sub := range submissions {
		if _, ok := users[sub.UserID]; !ok {
			users[sub.UserID] = &userStat{userID: sub.UserID, problems: make(map[int32]*problemStat)}
			order = append(order, sub.UserID)
		}
		u := users[sub.UserID]

		if _, ok := u.problems[int32(sub.Num)]; !ok {
			u.problems[int32(sub.Num)] = &problemStat{}
		}
		p := u.problems[int32(sub.Num)]

		if sub.InDate.After(lockTime) && now.Before(lockTime.Add(2*time.Hour)) {
			continue
		}

		if sub.Result == 4 {
			if p.acTime == 0 {
				p.acTime = int(sub.InDate.Sub(startTime).Seconds())
				u.solved++
				u.time += p.acTime + p.attempts*1200
			}
		} else {
			if sub.Result == 11 && !cePenalty {
				continue
			}
			p.attempts++
		}
	}

	result := make([]ContestRankRow, 0, len(order))
	for i, uid := range order {
		u := users[uid]
		row := ContestRankRow{Rank: i + 1, UserID: u.userID, Solved: u.solved, Time: u.time}
		for num, p := range u.problems {
			row.ProblemResults = append(row.ProblemResults, ProblemResult{Num: int32(num), Attempts: p.attempts, ACTime: p.acTime})
		}
		result = append(result, row)
	}

	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Solved > result[i].Solved ||
				(result[j].Solved == result[i].Solved && result[j].Time < result[i].Time) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	for i := range result {
		result[i].Rank = i + 1
	}
	return result
}

func changeResult(result int16) int {
	switch result {
	case 4:
		return 0
	case 5:
		return 1
	case 6:
		return 4
	case 7:
		return 2
	case 8:
		return 3
	case 9:
		return 6
	case 10:
		return 5
	case 11:
		return 7
	default:
		return -1
	}
}

// ── user_set_ip.php ─────────────────────────────────────────────────

// UserSetIPPageVM 是设置登录 IP 页的视图模型。
type UserSetIPPageVM struct {
	Title       string
	OJName      string
	CID         int32
	ContestName string
	Description string
	StartTime   string
	EndTime     string
	Private     bool
	IsOver      bool
	IsNotYet    bool
	IsRunning   bool
	Changed     bool
	UserID      string
	IP          string
}

func (s *Server) handleUserSetIP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid := int32(parseInt(r.URL.Query().Get("cid")))
	if cid <= 0 {
		http.Error(w, "缺少 cid", http.StatusBadRequest)
		return
	}

	identity := s.resolveIdentity(w, r)
	contest, err := s.queries().GetContestByID(ctx, cid)
	if err != nil {
		if err == sql.ErrNoRows {
			renderPage(w, http.StatusOK, "errorPage", "比赛不存在",
				map[string]string{"Message": "没有这场比赛"})
			return
		}
		s.log.Error("查询比赛失败", "err", err)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	start := contest.StartTime.Time
	end := contest.EndTime.Time
	isOver := now.After(end)
	isNotYet := now.Before(start)
	isRunning := !isOver && !isNotYet

	if r.Method == http.MethodPost && r.FormValue("do") != "" {
		if identity.Admin || identity.ContestCreator {
			userID := strings.TrimSpace(r.FormValue("user_id"))
			ip := strings.TrimSpace(r.FormValue("ip"))
			if userID != "" && ip != "" {
				err := s.queries().InsertLoginLog(ctx, dbgen.InsertLoginLogParams{
					UserID:   userID,
					Password: sql.NullString{String: "set ip by " + identity.UserID, Valid: true},
					Ip:       sql.NullString{String: ip, Valid: true},
				})
				if err != nil {
					s.log.Error("插入 loginlog 失败", "err", err)
				} else {
					renderPage(w, http.StatusOK, "userSetIPPage", "设置登录 IP", UserSetIPPageVM{
						Title: "设置登录 IP", OJName: s.cfg.OJName,
						CID: cid, ContestName: contest.Title.String,
						Description: contest.Description.String,
						StartTime:   start.Format("2006-01-02 15:04:05"),
						EndTime:     end.Format("2006-01-02 15:04:05"),
						Private:     contest.Private != 0,
						IsOver:      isOver, IsNotYet: isNotYet, IsRunning: isRunning,
						Changed: true, UserID: userID, IP: ip,
					})
					return
				}
			}
		}
	}

	renderPage(w, http.StatusOK, "userSetIPPage", "设置登录 IP", UserSetIPPageVM{
		Title: "设置登录 IP", OJName: s.cfg.OJName,
		CID: cid, ContestName: contest.Title.String,
		Description: contest.Description.String,
		StartTime:   start.Format("2006-01-02 15:04:05"),
		EndTime:     end.Format("2006-01-02 15:04:05"),
		Private:     contest.Private != 0,
		IsOver:      isOver, IsNotYet: isNotYet, IsRunning: isRunning,
	})
}

func (s *Server) registerP1Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /online.php", s.handleOnline)
	mux.HandleFunc("GET /recent-contest.php", s.handleRecentContest)
	mux.HandleFunc("GET /export_ac_code.php", s.handleExportACCode)
	mux.HandleFunc("GET /export_contest_code.php", s.handleExportContestCode)
	mux.HandleFunc("GET /comparesource.php", s.handleCompareSource)
	mux.HandleFunc("GET /getsource.php", s.handleGetSource)
	mux.HandleFunc("GET /showsource2.php", s.handleShowSource2)
	mux.HandleFunc("GET /contestrank2.php", s.handleContestRank2)
	mux.HandleFunc("GET /contestrank3.php", s.handleContestRank3)
	mux.HandleFunc("/user_set_ip.php", s.handleUserSetIP)
}
