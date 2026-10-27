package web

import (
	"strings"
	"testing"
)

// 状态页不再排除 rank-hidden 账号：admin 等账号的提交一律可见。
//
// 背景：11218–11266 这批最新提交全部属于 admin（$OJ_RANK_HIDDEN），
// 若按游客分支排除，公开状态页最高只能显示到 11217，看起来像"看不到提交记录"。
func TestBuildWhereDoesNotExcludeRankHidden(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, nil)

	// 匿名、无 user_id。
	where, args := srv.buildWhere(StatusFilter{}, Anonymous())
	if strings.Contains(where, "NOT IN") {
		t.Errorf("匿名默认不应排除 rank-hidden：where=%q", where)
	}
	for _, a := range args {
		if str, ok := a.(string); ok && (str == "admin" || str == "zhblue") {
			t.Errorf("不应把 rank-hidden 账号作为排除参数：args=%v", args)
		}
	}

	// 匿名、显式 user_id=admin。
	where, args = srv.buildWhere(StatusFilter{UserID: "admin"}, Anonymous())
	if strings.Contains(where, "NOT IN") {
		t.Errorf("显式 user_id 不应排除 rank-hidden：where=%q", where)
	}
	if len(args) != 1 || args[0] != "admin" {
		t.Errorf("应只带 user_id 参数：args=%v", args)
	}
	if !strings.Contains(where, "user_id = ?") {
		t.Errorf("应带上 user_id 过滤：where=%q", where)
	}
}
