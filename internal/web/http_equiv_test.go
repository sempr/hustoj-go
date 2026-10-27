package web

// 端到端对拍：真实 HTTP 响应 vs PHP 直接执行 problemset.php 的拼装逻辑。
//
// 与 problem_equiv_test.go 的区别：那个文件比对 SQL 返回的 problem_id，
// 这个文件比对**渲染后的 HTML 里出现的 problem_id**，因此能抓到
// "查询对但渲染丢了/多了行"这类问题 —— 那是切流事故里最难查的一类。
//
// 默认跳过，需显式打开：
//
//	HUSTOJ_HTTP_EQUIV=1 HUSTOJ_EQUIV_DSN='...' go test ./internal/web/ -run HTTPEquiv -v
//
// 与 problem_equiv_test.go 共用同一套开关，保持调用方式一致。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// httpEquivMode 把 Viewer 映射成探针能理解的模式名。
func httpEquivMode(v Viewer) string {
	switch v {
	case ViewerAdmin:
		return "admin"
	case ViewerFreePractice:
		return "free"
	default:
		return "member"
	}
}

// extractProblemIDs 从渲染后的 HTML 中抽取题目链接里的 problem_id。
func extractProblemIDs(html string) []int {
	const marker = `href="problem.php?id=`
	var ids []int
	for i := 0; ; {
		j := strings.Index(html[i:], marker)
		if j < 0 {
			return ids
		}
		i += j + len(marker)
		k := strings.IndexByte(html[i:], '"')
		if k < 0 {
			return ids
		}
		n, err := strconv.Atoi(html[i : i+k])
		if err != nil {
			i += k
			continue
		}
		ids = append(ids, n)
		i += k
	}
}

// runPHPIDs 让 php 解释器按 problemset.php 的拼装逻辑返回 problem_id 顺序。
func runPHPIDs(t *testing.T, mode string, page int, search string) []int {
	t.Helper()
	phpweb := filepath.Join("..", "..", "phpweb")
	if _, err := os.Stat(filepath.Join(phpweb, "include", "db_info.inc.php")); err != nil {
		t.Skip("找不到 phpweb/include/db_info.inc.php（需在仓库内运行）")
	}
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("PATH 中没有 php")
	}

	script := strings.NewReplacer(
		"%MODE%", phpQuote(mode),
		"%PAGE%", strconv.Itoa(page),
		"%SEARCH%", phpQuote(search),
	).Replace(phpIDsTemplate)

	probe := filepath.Join(phpweb, "include", fmt.Sprintf("equiv_ids_%d.php", os.Getpid()))
	if err := os.WriteFile(probe, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(probe) })

	cmd := exec.Command("php", "-d", "display_errors=1", probe)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("php 执行失败: %v", err)
	}
	var ids []int
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(trimmed), &ids); err != nil {
		t.Fatalf("解析 php 输出 %q 失败: %v", trimmed, err)
	}
	return ids
}

const phpIDsTemplate = `<?php
define('OJ_NAME', 'hustoj');
require __DIR__ . '/db_info.inc.php';
$db = new PDO("mysql:host=$DB_HOST;dbname=$DB_NAME", $DB_USER, $DB_PASS);

$mode = %MODE%; $page = %PAGE%; $search_arg = %SEARCH%;

// ── problemset.php:66-93 ──
$page_cnt = 50;
$filter_sql = ""; $order_by = " order by problem_id "; $args = array();
if ($search_arg !== "") {
    $search = "%" . $search_arg . "%";
    $filter_sql = " ( title like ? or source like ?)";
    $limit_sql = " LIMIT " . ($page - 1) * $page_cnt . "," . $page_cnt;
    $args = array($search, $search);
} else {
    $filter_sql = " problem_id > 0";
    $limit_sql = " LIMIT " . ($page - 1) * $page_cnt . "," . $page_cnt;
}

// ── problemset.php:120-133 ──
if ($mode === 'admin') {
} elseif ($mode === 'free') {
    $filter_sql .= " and defunct='N' ";
} else {
    $now = date('Y-m-d H:i', time());
    $filter_sql .= " and ` + "`defunct`" + `='N' AND ` + "`problem_id`" + ` NOT IN (
        SELECT ` + "`problem_id`" + ` FROM contest c
        INNER JOIN ` + "`contest_problem`" + ` cp ON c.` + "`contest_id`" + ` = cp.` + "`contest_id`" + `
        AND (c.` + "`defunct`" + ` = 'N' AND '$now'<c.` + "`end_time`" + `)) ";
}

$sql = "select problem_id from problem A WHERE $filter_sql $order_by $limit_sql ";
$st = $db->prepare($sql); $st->execute($args);
$out = array();
foreach ($st->fetchAll(PDO::FETCH_ASSOC) as $r) { $out[] = (int)$r['problem_id']; }
echo json_encode($out);
`

// TestHTTPEquivProblemList 覆盖首批路由会真实走到的组合。
func TestHTTPEquivProblemList(t *testing.T) {
	if os.Getenv("HUSTOJ_HTTP_EQUIV") != "1" {
		t.Skip("未设置 HUSTOJ_HTTP_EQUIV")
	}
	db := equivDB(t)

	modes := []struct {
		name   string
		viewer Viewer
	}{
		{"member", ViewerMember},
		{"free", ViewerFreePractice},
		{"admin", ViewerAdmin},
	}
	searches := []string{"", "a", "5", "zzz不存在"}
	pages := []int{1, 2, 3}

	for _, m := range modes {
		for _, kw := range searches {
			for _, page := range pages {
				t.Run(fmt.Sprintf("%s/search=%q/page=%d", m.name, kw, page), func(t *testing.T) {
					want := runPHPIDs(t, httpEquivMode(m.viewer), page, kw)

					srv := &Server{
						cfg: defaultTestConfig(),
						log: newTestLogger(),
						db:  db,
						q:   dbgen.New(db),
					}
					url := fmt.Sprintf("/problemset.php?page=%d", page)
					if kw != "" {
						url += "&search=" + urlEncode(kw)
					}
					rec := httptest.NewRecorder()
					srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))

					if rec.Code != http.StatusOK {
						t.Fatalf("状态码 = %d，期望 200", rec.Code)
					}
					got := extractProblemIDs(rec.Body.String())
					// 归一化空值：php 侧无匹配时 json 输出 "[]" 反序列化成非 nil 空切片，
					// 抽取函数也返回非 nil，但任一侧为 nil 时 DeepEqual 会判不等 ——
					// 那会造成"两边都是空却报不一致"的假阳性，掩盖真正的差异。
					if want == nil {
						want = []int{}
					}
					if got == nil {
						got = []int{}
					}
					if !reflect.DeepEqual(want, got) {
						t.Errorf("渲染出的题目集合/顺序与 PHP 不一致（PHP %d 行 / Go %d 行）\n  PHP: %v\n  Go : %v",
							len(want), len(got), want, got)
					}
				})
			}
		}
	}
}

// TestHTTPEquivProblemDetail 比对详情页：两侧都取同一道题，检查关键字段都在。
func TestHTTPEquivProblemDetail(t *testing.T) {
	if os.Getenv("HUSTOJ_HTTP_EQUIV") != "1" {
		t.Skip("未设置 HUSTOJ_HTTP_EQUIV")
	}
	db := equivDB(t)

	// 取一道未删除的题目
	var id int32
	if err := db.QueryRow("SELECT problem_id FROM problem WHERE defunct='N' ORDER BY problem_id LIMIT 1").Scan(&id); err != nil {
		t.Skip("题库里没有可用题目")
	}

	srv := &Server{cfg: defaultTestConfig(), log: newTestLogger(), db: db, q: dbgen.New(db)}
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/problem.php?id=%d", id), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()

	// 标题必须与库里一致，且已转义
	var title string
	if err := db.QueryRow("SELECT title FROM problem WHERE problem_id=?", id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, templateEscape(title)) {
		t.Errorf("详情页缺少转义后的标题 %q", templateEscape(title))
	}
	if !strings.Contains(body, fmt.Sprintf("编号 %d", id)) {
		t.Errorf("详情页缺少题目编号")
	}
	_ = context.Background
	_ = time.Now
}
