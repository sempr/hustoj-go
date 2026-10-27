package client

import "testing"

// client 子命令的参数解析：
//
//	client <solution_id> <runner_id> [oj_home_path] [-debug]
func TestParseClientArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantID     int
		wantRunner string
		wantHome   string
		wantDebug  bool
		wantErr    bool
	}{
		{
			name:       "最小参数",
			args:       []string{"client", "131832", "3"},
			wantID:     131832,
			wantRunner: "3",
			wantHome:   "/home/judge",
		},
		{
			name:       "带 OJ 根目录",
			args:       []string{"client", "7", "0", "/srv/hustoj"},
			wantID:     7,
			wantRunner: "0",
			wantHome:   "/srv/hustoj",
		},
		{
			name:       "debug 开关",
			args:       []string{"client", "7", "0", "/srv/hustoj", "-debug"},
			wantID:     7,
			wantRunner: "0",
			wantHome:   "/srv/hustoj",
			wantDebug:  true,
		},
		{
			name:       "DEBUG 关键字等价于 -debug",
			args:       []string{"client", "7", "0", "DEBUG"},
			wantID:     7,
			wantRunner: "0",
			wantHome:   "/home/judge",
			wantDebug:  true,
		},
		{name: "参数不足", args: []string{"client", "7"}, wantErr: true},
		{name: "solution_id 非法", args: []string{"client", "abc", "0"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, runner, home, debug, err := parseClientArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望出错，实际解析成功: id=%d runner=%q home=%q", id, runner, home)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if id != tc.wantID {
				t.Errorf("solution_id = %d，期望 %d", id, tc.wantID)
			}
			if runner != tc.wantRunner {
				t.Errorf("runner_id = %q，期望 %q", runner, tc.wantRunner)
			}
			if home != tc.wantHome {
				t.Errorf("oj_home = %q，期望 %q", home, tc.wantHome)
			}
			if debug != tc.wantDebug {
				t.Errorf("debug = %v，期望 %v", debug, tc.wantDebug)
			}
		})
	}
}
