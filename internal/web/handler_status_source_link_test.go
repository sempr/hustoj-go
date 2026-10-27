package web

import "testing"

// TestMarkViewableSources 校验"语言列→查看代码链接"的可见性规则。
func TestMarkViewableSources(t *testing.T) {
	rows := []StatusRow{
		{SolutionID: 1, UserID: "alice"},
		{SolutionID: 2, UserID: "bob"},
		{SolutionID: 3, UserID: "alice", Masked: true},
	}

	// 本人：只有自己那条可见；封榜行即使本人也不给链接。
	markViewableSources(rows, Identity{UserID: "alice", LoggedIn: true})
	if !rows[0].CanViewSource {
		t.Errorf("本人提交应可查看源码")
	}
	if rows[1].CanViewSource {
		t.Errorf("他人提交本人不可查看")
	}
	if rows[2].CanViewSource {
		t.Errorf("封榜隐藏行不应给查看链接")
	}

	// source_browser：全部可见（非封榜）。
	rows = []StatusRow{{SolutionID: 1, UserID: "alice"}, {SolutionID: 2, UserID: "bob"}}
	markViewableSources(rows, Identity{SourceBrowser: true})
	if !rows[0].CanViewSource || !rows[1].CanViewSource {
		t.Errorf("source_browser 应可查看全部源码")
	}

	// 匿名：都不可见。
	rows = []StatusRow{{SolutionID: 1, UserID: "alice"}}
	markViewableSources(rows, Anonymous())
	if rows[0].CanViewSource {
		t.Errorf("匿名不应可查看源码")
	}
}
