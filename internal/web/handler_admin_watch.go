// 系统负荷监控（admin/watch.php）。
//
// 只读：从 /proc 读 CPU/内存/swap/TCP，用 unix.Statfs 读磁盘，历史点写入
// /dev/shm/watchlog（JSON，保留 1 周）。`?json` 返回给前端 flot 的四组曲线
// `[[[ms,v],...] x4]`（与 PHP json_encode 同形）。
//
// 用原生 /proc 采样替代 PHP 的 `top -bn1` / exec；前端图表沿用 PHP 的 flot +
// jQuery（静态资源仍在，混合模式可加载）。

package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	watchLogMax     = 10500 // 1 周 ≈ 10080 分钟
	watchLogEveryMS = 60000 // 每分钟记一个点
)

// watchLogFile 可被测试覆盖，避免写到真实的 /dev/shm。
var watchLogFile = "/dev/shm/watchlog"

var (
	watchMu   sync.Mutex
	watchLast cpuStatSample
	watchHave bool
)

type cpuStatSample struct{ idle, total uint64 }

// readCPUStat 解析 /proc/stat 第一行 cpu。
func readCPUStat() (cpuStatSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuStatSample{}, false
	}
	f := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(f) < 5 || f[0] != "cpu" {
		return cpuStatSample{}, false
	}
	var total uint64
	for _, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
	}
	idle, _ := strconv.ParseUint(f[4], 10, 64)
	iowait := uint64(0)
	if len(f) > 5 {
		iowait, _ = strconv.ParseUint(f[5], 10, 64)
	}
	return cpuStatSample{idle: idle + iowait, total: total}, true
}

// cpuUsagePercent 返回自上次采样以来的 CPU 使用率；首次调用做一次 150ms 采样。
func cpuUsagePercent() float64 {
	watchMu.Lock()
	defer watchMu.Unlock()

	cur, ok := readCPUStat()
	if !ok {
		return 0
	}
	if !watchHave {
		watchLast = cur
		watchHave = true
		time.Sleep(150 * time.Millisecond)
		cur, ok = readCPUStat()
		if !ok {
			return 0
		}
	}
	dTotal := cur.total - watchLast.total
	dIdle := cur.idle - watchLast.idle
	watchLast = cur
	if dTotal == 0 {
		return 0
	}
	return (1 - float64(dIdle)/float64(dTotal)) * 100
}

func readMemInfo() (totalMB, freeMB, totalSwapMB, usedSwapMB int) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, 0
	}
	var total, avail, swapTotal, swapFree int
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.Atoi(f[1])
		switch f[0] {
		case "MemTotal:":
			total = n
		case "MemAvailable:":
			avail = n
		case "SwapTotal:":
			swapTotal = n
		case "SwapFree:":
			swapFree = n
		}
	}
	return total / 1024, avail / 1024, swapTotal / 1024, (swapTotal - swapFree) / 1024
}

func readTCPInUse() int {
	data, err := os.ReadFile("/proc/net/sockstat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "TCP:") {
			continue
		}
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "inuse" {
				n, _ := strconv.Atoi(f[i+1])
				return n
			}
		}
	}
	return 0
}

func readDiskUsedTotalMB() (usedMB, totalGB int) {
	var st unix.Statfs_t
	if err := unix.Statfs("/", &st); err != nil {
		return 0, 0
	}
	free := st.Bavail * uint64(st.Bsize)
	total := st.Blocks * uint64(st.Bsize)
	return int((total - free) / (1 << 20)), int(total / (1 << 30))
}

// sample 是 [cpu, freeMemMB, usedSwapMB, tcp, nowMS, diskUsedMB, diskTotalGB]。
func collectSample() []float64 {
	_, freeMB, _, usedSwapMB := readMemInfo()
	diskUsed, diskTotal := readDiskUsedTotalMB()
	return []float64{
		cpuUsagePercent(), float64(freeMB), float64(usedSwapMB), float64(readTCPInUse()),
		float64(time.Now().UnixMilli()), float64(diskUsed), float64(diskTotal),
	}
}

// loadHistory 读取并清洗历史（只保留 1 周内的点）。
func loadHistory(cutoffMS float64) [][]float64 {
	var history [][]float64
	if data, err := os.ReadFile(watchLogFile); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &history)
	}
	out := history[:0]
	for _, s := range history {
		if len(s) > 4 && s[4] > cutoffMS {
			out = append(out, s)
		}
	}
	return out
}

// watchSeries 采集当前点（必要时落盘），返回四组曲线 + 最新点。
func watchSeries() (series [][][2]float64, last []float64) {
	nowMS := float64(time.Now().UnixMilli())
	sample := collectSample()

	watchMu.Lock()
	history := loadHistory(nowMS - 7*24*3600*1000)
	if len(history) == 0 || nowMS-history[len(history)-1][4] >= watchLogEveryMS {
		history = append(history, sample)
		for len(history) > watchLogMax {
			history = history[1:]
		}
		if data, err := json.Marshal(history); err == nil {
			_ = os.WriteFile(watchLogFile, data, 0o644)
		}
	}
	watchMu.Unlock()

	// 历史 + 当前实时点（保证刷新连贯）。
	points := append(append([][]float64{}, history...), sample)

	totalMem, _, totalSwap, _ := readMemInfo()
	series = make([][][2]float64, 4)
	for _, s := range points {
		if len(s) < 5 {
			continue
		}
		ts := s[4]
		series[0] = append(series[0], [2]float64{ts, s[0]})
		series[1] = append(series[1], [2]float64{ts, pct(s[1], float64(totalMem))})
		series[2] = append(series[2], [2]float64{ts, pct(s[2], float64(totalSwap))})
		series[3] = append(series[3], [2]float64{ts, s[3]})
	}
	return series, points[len(points)-1]
}

func pct(v, total float64) float64 {
	if total == 0 {
		return 0
	}
	return v / total * 100
}

type watchVM struct {
	CPU     float64
	FreeMem int
	Swap    int
	TCP     int
	Disk    int
	Total   int
}

func (s *Server) handleAdminWatch(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		s.adminLoginRequired(w)
		return
	}

	series, last := watchSeries()

	if _, ok := r.URL.Query()["json"]; ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(series)
		return
	}

	s.renderPage(w, r, http.StatusOK, "adminWatchPage", "系统负荷监控", watchVM{
		CPU: last[0], FreeMem: int(last[1]), Swap: int(last[2]), TCP: int(last[3]),
		Disk: int(last[5]), Total: int(last[6]),
	})
}

func (s *Server) registerAdminWatchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/watch.php", s.handleAdminWatch)
}
