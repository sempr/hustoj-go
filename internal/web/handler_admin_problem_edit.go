// 后台题目新增/编辑（admin/problem_add_page.php、problem_add.php、problem_edit.php）。
//
// 权限：administrator / contest_creator / problem_editor（add_page、add）；
//       administrator / problem_editor（edit）。
// 数据文件写到 <OJHome>/data/<pid>/，与 PHP 的 $OJ_DATA/<pid> 对应。
// 新题以 defunct='Y' 落库（addproblem 行为），补完数据后由管理员启用。

package web

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

var (
	unicodeSpaceRe = regexp.MustCompile(`[\x{00A0}\x{1680}\x{2000}-\x{200A}\x{202F}\x{205F}\x{3000}]+`)
	zeroWidthRe    = regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{2060}\x{FEFF}]`)
)

// normalizeSpaces 复刻 my_func.inc.php 的 normalizeSpaces()：Unicode 空格归一成普通空格，
// 删除零宽字符与 BOM。
func normalizeSpaces(s string) string {
	s = unicodeSpaceRe.ReplaceAllString(s, " ")
	return zeroWidthRe.ReplaceAllString(s, "")
}

// commaEscape 复刻 PHP 的 str_replace(",", "&#44;")。
func commaEscape(s string) string { return strings.ReplaceAll(s, ",", "&#44;") }

// writeDataFile 把内容写进 <OJHome>/data/<pid>/<name>，并把 CRLF 归一成 LF。
func (s *Server) writeDataFile(pid int32, name, content string) {
	dir := s.problemDataDir(pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

type adminProblemAddVM struct {
	Source   string
	Contests []dbgen.ListFutureContestsRow
	ShowCoin bool
	Token    *http.Request
}

type adminProblemEditVM struct {
	ProblemID   int32
	Title       string
	TimeLimit   float64
	MemoryLimit int32
	Coin        uint32
	Description string
	Input       string
	Output      string
	SampleInput string
	SampleOut   string
	Hint        string
	Source      string
	RemoteOj    string
	RemoteID    string
	Spj         string
	ShowCoin    bool
	Token       *http.Request
}

type adminProblemAddResultVM struct {
	Title string
	PID   int32
}

// ── admin/problem_add_page.php ───────────────────────────────────────

func (s *Server) handleAdminProblemAddPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator || identity.ProblemEditor) {
		s.adminLoginRequired(w)
		return
	}

	src, err := s.queries().GetLastProblemSource(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.log.Error("取默认来源失败", "err", err)
	}
	contests, err := s.queries().ListFutureContests(ctx)
	if err != nil {
		s.log.Error("查询未来比赛失败", "err", err)
	}

	renderPage(w, http.StatusOK, "adminProblemAddPage", "添加题目", adminProblemAddVM{
		Source:   src.String,
		Contests: contests,
		ShowCoin: identity.Admin || identity.ProblemEditor,
		Token:    r,
	})
}

// ── admin/problem_add.php ────────────────────────────────────────────

func (s *Server) handleAdminProblemAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator || identity.ProblemEditor) {
		s.adminLoginRequired(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	title := commaEscape(r.FormValue("title"))
	description := commaEscape(r.FormValue("description"))
	input := commaEscape(r.FormValue("input"))
	output := commaEscape(r.FormValue("output"))
	hint := commaEscape(r.FormValue("hint"))
	sampleInput := normalizeSpaces(r.FormValue("sample_input"))
	sampleOutput := normalizeSpaces(r.FormValue("sample_output"))
	testInput := normalizeSpaces(r.FormValue("test_input"))
	testOutput := normalizeSpaces(r.FormValue("test_output"))
	source := r.FormValue("source")
	spj := r.FormValue("spj")
	if spj == "" {
		spj = "0"
	}

	res, err := s.queries().InsertProblem(ctx, dbgen.InsertProblemParams{
		Title:        title,
		TimeLimit:    ajaxFloat(r.FormValue("time_limit")),
		MemoryLimit:  int32(parseInt(r.FormValue("memory_limit"))),
		Description:  description,
		Input:        input,
		Output:       output,
		SampleInput:  sql.NullString{String: sampleInput, Valid: true},
		SampleOutput: sql.NullString{String: sampleOutput, Valid: true},
		Hint:         hint,
		Source:       sql.NullString{String: source, Valid: true},
		Spj:          spj,
	})
	if err != nil {
		s.log.Error("新增题目失败", "err", err)
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	pid64, _ := res.LastInsertId()
	pid := int32(pid64)

	coin := parseInt(r.FormValue("coin"))
	if coin < 0 {
		coin = 1
	}
	if r.FormValue("coin") == "" {
		coin = 1
	}
	_ = s.queries().SetProblemCoin(ctx, dbgen.SetProblemCoinParams{Coin: uint32(coin), ProblemID: pid})

	_ = os.MkdirAll(s.problemDataDir(pid), 0o755)
	if sampleOutput != "" && sampleInput == "" {
		sampleInput = "0"
	}
	if sampleInput != "" {
		s.writeDataFile(pid, "sample.in", sampleInput)
	}
	if sampleOutput != "" {
		s.writeDataFile(pid, "sample.out", sampleOutput)
	}
	if testOutput != "" && testInput == "" {
		testInput = "0"
	}
	if testInput != "" {
		s.writeDataFile(pid, "test.in", testInput)
	}
	if testOutput != "" {
		s.writeDataFile(pid, "test.out", testOutput)
	}

	if cid := int32(parseInt(r.FormValue("contest_id"))); cid > 0 {
		num, err := s.queries().GetContestProblemCount(ctx, sql.NullInt32{Int32: cid, Valid: true})
		if err == nil {
			_ = s.queries().InsertContestProblem(ctx, dbgen.InsertContestProblemParams{
				ProblemID: pid, ContestID: sql.NullInt32{Int32: cid, Valid: true}, Num: int32(num),
			})
		}
	}

	_ = s.queries().InsertPrivilege(ctx, dbgen.InsertPrivilegeParams{
		UserID: identity.UserID, Rightstr: "p" + strconv.Itoa(int(pid)), Valuestr: "true",
	})

	renderPage(w, http.StatusOK, "adminProblemAddResultPage", "题目已添加", adminProblemAddResultVM{Title: title, PID: pid})
}

// ── admin/problem_edit.php ───────────────────────────────────────────

func (s *Server) handleAdminProblemEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ProblemEditor) {
		s.adminLoginRequired(w)
		return
	}
	showCoin := identity.Admin || identity.ProblemEditor

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		id := int32(parseInt(r.FormValue("problem_id")))
		sampleInput := normalizeSpaces(r.FormValue("sample_input"))
		sampleOutput := normalizeSpaces(r.FormValue("sample_output"))
		err := s.queries().UpdateProblemFull(ctx, dbgen.UpdateProblemFullParams{
			Title:        commaEscape(r.FormValue("title")),
			TimeLimit:    ajaxFloat(r.FormValue("time_limit")),
			MemoryLimit:  int32(parseInt(r.FormValue("memory_limit"))),
			Description:  r.FormValue("description"),
			Input:        r.FormValue("input"),
			Output:       r.FormValue("output"),
			SampleInput:  sql.NullString{String: sampleInput, Valid: true},
			SampleOutput: sql.NullString{String: sampleOutput, Valid: true},
			Hint:         r.FormValue("hint"),
			Source:       sql.NullString{String: r.FormValue("source"), Valid: true},
			Spj:          defaultStr(r.FormValue("spj"), "0"),
			RemoteOj:     sql.NullString{String: r.FormValue("remote_oj"), Valid: true},
			RemoteID:     sql.NullString{String: r.FormValue("remote_id"), Valid: true},
			Coin:         uint32(parseInt(r.FormValue("coin"))),
			ProblemID:    id,
		})
		if err != nil {
			s.log.Error("更新题目失败", "err", err)
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		// PHP：仅当 sample.in 已存在且新样例非空时才重写样例文件。
		if sampleInput != "" {
			if _, err := os.Stat(filepath.Join(s.problemDataDir(id), "sample.in")); err == nil {
				s.writeDataFile(id, "sample.in", sampleInput)
				s.writeDataFile(id, "sample.out", sampleOutput)
			}
		}
		renderPage(w, http.StatusOK, "errorPage", "题目已更新", map[string]string{
			"Message": "Problem Updated! Edit OK!",
		})
		return
	}

	id := int32(parseInt(r.URL.Query().Get("id")))
	p, err := s.queries().GetProblemByID(ctx, id)
	if err != nil {
		renderPage(w, http.StatusOK, "errorPage", "无此题目", map[string]string{"Message": "No such problem."})
		return
	}
	renderPage(w, http.StatusOK, "adminProblemEditPage", "编辑题目", adminProblemEditVM{
		ProblemID:   p.ProblemID,
		Title:       p.Title,
		TimeLimit:   p.TimeLimit,
		MemoryLimit: p.MemoryLimit,
		Coin:        p.Coin,
		Description: p.Description,
		Input:       p.Input,
		Output:      p.Output,
		SampleInput: p.SampleInput.String,
		SampleOut:   p.SampleOutput.String,
		Hint:        p.Hint,
		Source:      p.Source.String,
		RemoteOj:    p.RemoteOj.String,
		RemoteID:    p.RemoteID.String,
		Spj:         p.Spj,
		ShowCoin:    showCoin,
		Token:       r,
	})
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (s *Server) registerAdminProblemEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/problem_add_page.php", s.handleAdminProblemAddPage)
	mux.HandleFunc("POST /admin/problem_add.php", s.handleAdminProblemAdd)
	mux.HandleFunc("GET /admin/problem_edit.php", s.handleAdminProblemEdit)
	mux.HandleFunc("POST /admin/problem_edit.php", s.handleAdminProblemEdit)
}
