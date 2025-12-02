package rawtext

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Question 结构体
type Question struct {
	Answer string
	Score  float64
}

// UserAnswer 结构体
type UserAnswer struct {
	QuestionID int
	Answer     string
}

// parseAnswerLine: 解析单行答案文件，格式 "1 [2] A"
func parseAnswerLine(line string) (num int, score float64, ans string, err error) {
	// 找到 "[" 和 "]"
	lb := strings.Index(line, "[")
	rb := strings.Index(line, "]")
	if lb == -1 || rb == -1 || lb > rb {
		return 0, 0, "", fmt.Errorf("invalid answer line: %s", line)
	}

	// 题号
	numStr := strings.TrimSpace(line[:lb])
	num, err = strconv.Atoi(numStr)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid question number: %s", numStr)
	}

	// 分数
	scoreStr := strings.TrimSpace(line[lb+1 : rb])
	score, err = strconv.ParseFloat(scoreStr, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid score: %s", scoreStr)
	}

	// 答案
	ans = strings.TrimSpace(line[rb+1:])
	return num, score, ans, nil
}

// ReadAnswerFilePath 支持绝对路径，空行和注释行
func ReadAnswerFilePath(filename string) (map[int]Question, float64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	answers := make(map[int]Question)
	var totalScore float64

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		// 跳过空行或注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		num, score, ans, err := parseAnswerLine(line)
		if err != nil {
			return nil, 0, err
		}
		answers[num] = Question{Answer: ans, Score: score}
		totalScore += score
	}

	return answers, totalScore, sc.Err()
}

// ReadUserFilePath 支持绝对路径，空行跳过
func ReadUserFilePath(filename string) ([]UserAnswer, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ua []UserAnswer
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		// 跳过空行
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue // 跳过格式错误的行
		}
		qid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		ua = append(ua, UserAnswer{
			QuestionID: qid,
			Answer:     strings.TrimSpace(parts[1]),
		})
	}
	return ua, sc.Err()
}

// CalculateScore 评分逻辑（纯逻辑，不依赖文件）
func CalculateScore(answers map[int]Question, userAnswers []UserAnswer) float64 {
	var score float64
	for _, ua := range userAnswers {
		q, ok := answers[ua.QuestionID]
		if !ok {
			continue
		}
		if strings.EqualFold(ua.Answer, q.Answer) || strings.EqualFold(q.Answer, "*") {
			score += q.Score
		}
	}
	return score
}

// RawTextJudge 调用文件路径进行评分
func RawTextJudge(infile, outfile, userfile string) (float64, float64, error) {
	slog.Info("judging", "infile", infile, "outfile", outfile, "userfile", userfile)
	answers, totalScore, err := ReadAnswerFilePath(outfile)
	if err != nil {
		return 0, 0, err
	}

	userAns, err := ReadUserFilePath(userfile)
	if err != nil {
		return 0, 0, err
	}

	userScore := CalculateScore(answers, userAns)
	return userScore, totalScore, nil
}
