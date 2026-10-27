package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminWatchJSON(t *testing.T) {
	old := watchLogFile
	watchLogFile = filepath.Join(t.TempDir(), "watchlog")
	defer func() { watchLogFile = old }()

	f := &fakeQuerier{}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/watch.php?json", nil, t.TempDir(), (*Server).handleAdminWatch)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var series [][][2]float64
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("响应不是合法 JSON 曲线: %v (%s)", err, rec.Body.String())
	}
	if len(series) != 4 {
		t.Errorf("曲线组数 = %d, 期望 4", len(series))
	}
	if len(series[0]) == 0 {
		t.Errorf("CPU 曲线不应为空")
	}
}

func TestAdminWatchPage(t *testing.T) {
	old := watchLogFile
	watchLogFile = filepath.Join(t.TempDir(), "watchlog")
	defer func() { watchLogFile = old }()

	f := &fakeQuerier{}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/watch.php", nil, t.TempDir(), (*Server).handleAdminWatch)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"CPU", "FREE", "SWAP", "watch.php?json"} {
		if !strings.Contains(body, want) {
			t.Errorf("监控页缺少 %q", want)
		}
	}
}

func TestAdminWatchRequiresRole(t *testing.T) {
	old := watchLogFile
	watchLogFile = filepath.Join(t.TempDir(), "watchlog")
	defer func() { watchLogFile = old }()

	f := &fakeQuerier{}
	rec := doProblem(t, f, "", http.MethodGet, "/admin/watch.php", nil, t.TempDir(), (*Server).handleAdminWatch)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}
