package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestContestRankXLSOracleDump 把 Go 的 contestrank.xls.php 导出结果落盘，
// 交给部署版 PHP 逐行复核。
//
// 用法：
//
//	TZ=Asia/Shanghai \
//	HUSTOJ_EQUIV_DSN='...' \
//	HUSTOJ_XLS_DUMP=/tmp/go_xls \
//	HUSTOJ_XLS_CIDS='1000,980001' \
//	go test ./internal/web -run TestContestRankXLSOracleDump
//
// 每个 cid 产出 <out>_<cid>.html 与 <out>_<cid>.headers。PHP 侧用
// /tmp/oracle-web 的部署树跑同一个 cid，逐行比对表格内容。
//
// 只在 HUSTOJ_XLS_DUMP 与 HUSTOJ_XLS_CIDS 同时设置时启用，常规测试运行跳过。
func TestContestRankXLSOracleDump(t *testing.T) {
	out := os.Getenv("HUSTOJ_XLS_DUMP")
	cids := os.Getenv("HUSTOJ_XLS_CIDS")
	if out == "" || cids == "" {
		t.Skip("跳过（需要 HUSTOJ_XLS_DUMP 与 HUSTOJ_XLS_CIDS）")
	}
	srv := newPageDB(t)
	for _, cid := range strings.Split(cids, ",") {
		cid = strings.TrimSpace(cid)
		if cid == "" {
			continue
		}
		if _, err := strconv.Atoi(cid); err != nil {
			t.Fatalf("cid %q 不是数字: %v", cid, err)
		}
		rec := httptest.NewRecorder()
		srv.handleContestRankXLS(rec, httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/contestrank.xls.php?cid=%s", cid), nil))

		body := rec.Body.String()
		base := fmt.Sprintf("%s_%s", out, cid)
		if err := os.WriteFile(base+".html", []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		// 响应头也要比对：Content-Type 与 Content-Disposition 决定浏览器
		// 会不会把它当附件、用什么文件名存。
		var hdr strings.Builder
		for _, k := range []string{"Content-Type", "Content-Disposition"} {
			if v := rec.Header().Get(k); v != "" {
				fmt.Fprintf(&hdr, "%s: %s\n", k, v)
			}
		}
		if err := os.WriteFile(base+".headers", []byte(hdr.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("cid=%s status=%d bytes=%d", cid, rec.Code, len(body))
	}
}
