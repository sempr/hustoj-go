package rawtext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --------- 1️⃣ 直接测试评分逻辑（无文件） ---------
func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name        string
		answers     map[int]Question
		userAns     []UserAnswer
		wantScore   float64
		wantDetails string
	}{
		{
			name: "all correct",
			answers: map[int]Question{
				1: {Answer: "A", Score: 2.0},
				2: {Answer: "B", Score: 3.0},
			},
			userAns: []UserAnswer{
				{QuestionID: 1, Answer: "A"},
				{QuestionID: 2, Answer: "B"},
			},
			wantScore:   5.0,
			wantDetails: "",
		},
		{
			name: "case insensitive",
			answers: map[int]Question{
				1: {Answer: "hello", Score: 2.0},
			},
			userAns: []UserAnswer{
				{QuestionID: 1, Answer: "HELLO"},
			},
			wantScore:   2.0,
			wantDetails: "",
		},
		{
			name: "wildcard *",
			answers: map[int]Question{
				1: {Answer: "*", Score: 1.0},
			},
			userAns: []UserAnswer{
				{QuestionID: 1, Answer: "anything"},
			},
			wantScore:   1.0,
			wantDetails: "",
		},
		{
			name: "wrong answer",
			answers: map[int]Question{
				1: {Answer: "A", Score: 2.0},
			},
			userAns: []UserAnswer{
				{QuestionID: 1, Answer: "B"},
			},
			wantScore:   0.0,
			wantDetails: "1 Answer:A[You:B] -2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, details := CalculateScore(tt.answers, tt.userAns)
			if got != tt.wantScore {
				t.Errorf("got %.2f, want %.2f", got, tt.wantScore)
			}
			details = strings.Trim(details, " \n\t")
			if details != tt.wantDetails {
				t.Errorf("got [%s], want [%s]", details, tt.wantDetails)

			}
		})
	}
}

// --------- 2️⃣ 文件 + 正常评分测试 ---------
func TestRawTextJudge_File_Normal(t *testing.T) {
	tests := []struct {
		name      string
		answerTxt string
		userTxt   string
		wantScore float64
		wantTotal float64
	}{
		{
			name:      "all correct",
			answerTxt: "1 [2] A\n2 [3] B\n",
			userTxt:   "1 A\n2 B\n",
			wantScore: 5.0,
			wantTotal: 5.0,
		},
		{
			name:      "case insensitive",
			answerTxt: "1 [2] hello\n",
			userTxt:   "1 HELLO\n",
			wantScore: 2.0,
			wantTotal: 2.0,
		},
		{
			name:      "wildcard *",
			answerTxt: "1 [1] *\n",
			userTxt:   "1 anything\n",
			wantScore: 1.0,
			wantTotal: 1.0,
		},
		{
			name:      "wrong answer",
			answerTxt: "1 [2] A\n",
			userTxt:   "1 B\n",
			wantScore: 0.0,
			wantTotal: 2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			answerFile := filepath.Join(dir, "answer.txt")
			userFile := filepath.Join(dir, "user.txt")
			os.WriteFile(answerFile, []byte(tt.answerTxt), 0644)
			os.WriteFile(userFile, []byte(tt.userTxt), 0644)

			ss, gotScore, gotTotal, err := RawTextJudge("", answerFile, userFile)
			if err != nil {
				t.Fatalf("RawTextJudge returned error: %v", err)
			}
			if gotScore != tt.wantScore {
				t.Errorf("gotScore = %.2f, want %.2f", gotScore, tt.wantScore)
			}
			if gotTotal != tt.wantTotal {
				t.Errorf("gotTotal = %.2f, want %.2f", gotTotal, tt.wantTotal)
			}
			if ss != "" {
				fmt.Println(ss)
			}
		})
	}
}

// --------- 3️⃣ 错误场景测试 ---------
func TestRawTextJudge_File_Error(t *testing.T) {
	tests := []struct {
		name      string
		answerTxt string
		userTxt   string
		wantErr   bool
	}{
		{
			name:      "answer file missing",
			answerTxt: "",
			userTxt:   "1 A\n",
			wantErr:   true,
		},
		{
			name:      "user file missing",
			answerTxt: "1 [2] A\n",
			userTxt:   "",
			wantErr:   true,
		},
		{
			name:      "answer file bad format",
			answerTxt: "1 A\n", // 缺分数
			userTxt:   "1 A\n",
			wantErr:   true,
		},
		{
			name:      "user file bad format",
			answerTxt: "1 [2] A\n",
			userTxt:   "A\n", // 缺题号
			wantErr:   false, // 跳过无效行，不报错
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			answerFile := filepath.Join(dir, "answer.txt")
			userFile := filepath.Join(dir, "user.txt")

			if tt.answerTxt != "" {
				os.WriteFile(answerFile, []byte(tt.answerTxt), 0644)
			}
			if tt.userTxt != "" {
				os.WriteFile(userFile, []byte(tt.userTxt), 0644)
			}

			_, _, _, err := RawTextJudge("", answerFile, userFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("RawTextJudge() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --------- 4️⃣ 空行和注释行测试 ---------
func TestRawTextJudge_File_CommentsEmpty(t *testing.T) {
	tests := []struct {
		name      string
		answerTxt string
		userTxt   string
		wantScore float64
		wantTotal float64
	}{
		{
			name:      "answer file with empty lines",
			answerTxt: "\n1 [2] A\n\n2 [3] B\n\n",
			userTxt:   "1 A\n2 B\n",
			wantScore: 5.0,
			wantTotal: 5.0,
		},
		{
			name:      "answer file with comment lines",
			answerTxt: "# comment line\n1 [2] A\n# another comment\n2 [3] B\n",
			userTxt:   "1 A\n2 B\n",
			wantScore: 5.0,
			wantTotal: 5.0,
		},
		{
			name:      "answer file mixed empty and comment lines",
			answerTxt: "\n# comment\n1 [2] A\n\n2 [3] B\n# end\n",
			userTxt:   "1 A\n2 B\n",
			wantScore: 5.0,
			wantTotal: 5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			answerFile := filepath.Join(dir, "answer.txt")
			userFile := filepath.Join(dir, "user.txt")

			os.WriteFile(answerFile, []byte(tt.answerTxt), 0644)
			os.WriteFile(userFile, []byte(tt.userTxt), 0644)

			_, gotScore, gotTotal, err := RawTextJudge("", answerFile, userFile)
			if err != nil {
				t.Fatalf("RawTextJudge returned error: %v", err)
			}
			if gotScore != tt.wantScore {
				t.Errorf("gotScore = %.2f, want %.2f", gotScore, tt.wantScore)
			}
			if gotTotal != tt.wantTotal {
				t.Errorf("gotTotal = %.2f, want %.2f", gotTotal, tt.wantTotal)
			}
		})
	}
}
