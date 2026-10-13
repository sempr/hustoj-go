// 后台零散页：help.php（落地页/系统状态）与 suspect_list.php（查重）。
//
// help.php：显示平均判题延迟、负载、内存/磁盘、题目/用户/提交计数，以及各功能入口；
// 用原生读 /proc 与 unix.Statfs 替代 PHP 的 system("free/df/top")。
// suspect_list.php：列出同一 IP 多用户 / 同一用户多 IP（比赛时段内）的疑点。

package web

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

type adminHelpVM struct {
	Delay      string
	Load       string
	FreeMem    string
	FreeDisk   string
	ProblemNum int64
	UserNum    int64
	SubmitNum  int64
	IsAdmin    bool
}

func (s *Server) handleAdminHelp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		s.adminLoginRequired(w)
		return
	}

	vm := adminHelpVM{IsAdmin: identity.Admin}
	if v, err := s.queries().AvgJudgeDelay(ctx); err == nil {
		vm.Delay = fmt.Sprintf("%.0f", toFloat(v))
	} else {
		vm.Delay = "0"
	}
	vm.Load = readLoadAvg()
	vm.FreeMem = readFreeMem()
	vm.FreeDisk = readFreeDisk()
	vm.ProblemNum, _ = s.queries().CountProblemsAll(ctx)
	vm.UserNum, _ = s.queries().CountUsers(ctx)
	vm.SubmitNum, _ = s.queries().CountSolutions(ctx)

	renderPage(w, http.StatusOK, "adminHelpPage", "后台管理", vm)
}

// toFloat 把 avg() 返回的 interface{}（float64/[]byte/string）转成 float64。
func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case []byte:
		f, _ := strconv.ParseFloat(string(x), 64)
		return f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func readLoadAvg() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "?"
	}
	if f := strings.Fields(string(data)); len(f) > 0 {
		return f[0]
	}
	return "?"
}

// readFreeMem 读 /proc/meminfo，返回 "可用/总量 MB"。
func readFreeMem() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "?"
	}
	var total, avail int
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, _ = strconv.Atoi(f[1])
		case "MemAvailable:":
			avail, _ = strconv.Atoi(f[1])
		}
	}
	return fmt.Sprintf("%d/%d MB", avail/1024, total/1024)
}

func readFreeDisk() string {
	var st unix.Statfs_t
	if err := unix.Statfs("/", &st); err != nil {
		return "?"
	}
	free := st.Bavail * uint64(st.Bsize)
	total := st.Blocks * uint64(st.Bsize)
	gib := uint64(1) << 30
	return fmt.Sprintf("%d/%d GB", free/gib, total/gib)
}

// ── admin/suspect_list.php ───────────────────────────────────────────

type suspectRowVM struct {
	IP     string
	UserID string
	Time   string
	Count  int64
}

type adminSuspectVM struct {
	CID    int32
	ByIP   []suspectRowVM
	ByUser []suspectRowVM
}

func (s *Server) handleAdminSuspectList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		s.adminLoginRequired(w)
		return
	}

	cid := int32(parseInt(r.URL.Query().Get("cid")))
	vm := adminSuspectVM{CID: cid}

	if byIP, err := s.queries().SuspectByIP(ctx, dbgen.SuspectByIPParams{ContestID: nullCID(cid)}); err == nil {
		for _, row := range byIP {
			vm.ByIP = append(vm.ByIP, suspectRowVM{
				IP: row.Ip, UserID: row.UserID, Time: row.InDate.Format("2006-01-02 15:04:05"), Count: row.C,
			})
		}
	}

	if c, err := s.queries().GetContestByID(ctx, cid); err == nil {
		byUser, err := s.queries().SuspectUsersByIP(ctx, dbgen.SuspectUsersByIPParams{
			StartTime: c.StartTime, EndTime: c.EndTime, ContestID: nullCID(cid),
		})
		if err == nil {
			for _, row := range byUser {
				t := ""
				if row.Time.Valid {
					t = row.Time.Time.Format("2006-01-02 15:04:05")
				}
				vm.ByUser = append(vm.ByUser, suspectRowVM{IP: row.Ip.String, UserID: row.UserID, Time: t, Count: row.C})
			}
		}
	}

	renderPage(w, http.StatusOK, "adminSuspectPage", "查重", vm)
}

func (s *Server) registerAdminMiscRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/help.php", s.handleAdminHelp)
	mux.HandleFunc("GET /admin/suspect_list.php", s.handleAdminSuspectList)
}
