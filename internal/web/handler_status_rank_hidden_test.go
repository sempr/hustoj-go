package web

import (
	"strings"
	"testing"
)

// TestBuildWhereRankHiddenOnExplicitUser 校验：匿名默认排除 rank-hidden 账号，
// 但显式 ?user_id= 时不排除（否则 admin 提交后跳转的 status.php?user_id=admin
// 会显示为空，看起来像"看不到自己的提交"）。
func TestBuildWhereRankHiddenOnExplicitUser(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, nil)

	// 匿名、无 user_id：应排除 rank-hidden 并带两个参数。
	where, args := srv.buildWhere(StatusFilter{}, Anonymous())
	if !strings.Contains(where, "NOT IN") || len(args) != 2 {
		t.Errorf("匿名默认应排除 rank-hidden：where=%q args=%v", where, args)
	}

	// 匿名、显式 user_id=admin：不排除 rank-hidden（但会带上 user_id 参数）。
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
