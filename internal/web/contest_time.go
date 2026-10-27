package web

import (
	"fmt"
	"strconv"
	"time"
)

// formatTimeLength 复刻 my_func.inc.php:94 的 formatTimeLength。
//
// PHP 那份有若干"分支缺失但控制流继续"的写法，最明显的一处：
// 分钟数 ≥ 10 时整个 if/else 链没有匹配的分支，却仍然往下走去算小时，
// 于是中间那段分钟被静默丢弃。例如 6000 秒（1 小时 40 分）输出的是
// "1 小时 "，40 分凭空消失；3660 秒（1 小时 1 分）却正常输出
// "1 小时 1 分 "，因为分钟落在 1..9 区间、有分支可走。
//
// 这不是可以顺手修掉的 bug：比赛列表的"剩余时间"直接用它，
// 迁移后数字若与 PHP 不同，用户会以为倒计时不准。
// 所以这里严格照抄控制流，包括那些"什么都不写"的空档，
// 并用 TestFormatTimeLengthMatchesPHP 锁住。
//
// 另有两处相同的空档：小时 %24 == 0 时、小时落在 1..9 且 $result 为空时，
// 都不写入任何小时片段。
func formatTimeLength(length int) string {
	const (
		secondsWord = "秒"
		minutesWord = "分"
		hoursWord   = "小时"
		daysWord    = "天"
	)
	if length < 60 {
		return fmt.Sprintf("%d %s", length, secondsWord)
	}

	result := ""
	second := length % 60
	if second > 0 && second < 10 {
		result = "0" + strconv.Itoa(second) + " " + secondsWord
	} else if second > 0 {
		result = strconv.Itoa(second) + " " + secondsWord
	}

	length = length / 60 // 变成分钟
	if length < 60 {
		return fmt.Sprintf("%d %s %s", length, minutesWord, result)
	}

	minute := length % 60
	if minute == 0 {
		if result != "" {
			result = "00 " + minutesWord + " " + result
		}
	} else if minute < 10 {
		if result != "" {
			result = "0" + strconv.Itoa(minute) + " " + minutesWord + " " + result
		} else {
			result = strconv.Itoa(minute) + " " + minutesWord + " " + result
		}
	}
	// minute >= 10：PHP 无匹配分支，不写任何内容，控制流继续往下。

	length = length / 60 // 变成小时
	if length < 24 {
		return fmt.Sprintf("%d %s %s", length, hoursWord, result)
	}

	hour := length % 24
	if hour == 0 {
		if result != "" {
			result = "00 " + hoursWord + " " + result
		}
	} else if hour < 10 {
		if result != "" {
			result = "0" + strconv.Itoa(hour) + " " + hoursWord + " " + result
		}
		// hour 在 1..9 且 result 为空时，PHP 什么都不写。
	} else {
		result = strconv.Itoa(hour) + " " + hoursWord + " " + result
	}

	length = length / 24
	return fmt.Sprintf("%d %s %s", length, daysWord, result)
}

// contestStatusText 生成比赛列表的状态文案，对应 contest.php:253-265。
//
// 三种形态与 PHP 完全对应：
//   - 已结束：只显示结束时间
//   - 未开始：显示开始时间 + 总赛时
//   - 进行中：显示开始时间 + 剩余时间
//
// PHP 的进行中分支有个 HTML 笔误（多一个未闭合的 </span>），
// 那个在 Go 侧不复刻 —— 模板输出结构化字段，由模板负责拼装标记。
func contestStatusText(start, end, now int64) contestStatus {
	switch {
	case end <= now:
		// contest.php:255 —— text-muted，只跟结束时间。
		return contestStatus{State: "已结束", Tone: "muted", Detail: formatUnix(end)}
	case now < start:
		// contest.php:260 —— text-success，跟开始时间与总赛时。
		return contestStatus{
			State:  "开始于",
			Tone:   "success",
			Detail: formatUnix(start),
			Extra:  "总赛时",
			Length: formatTimeLength(int(end - start)),
		}
	default:
		// contest.php:264 —— text-danger，跟开始时间与剩余时间。
		return contestStatus{
			State:  "运行中",
			Tone:   "danger",
			Detail: formatUnix(start),
			Extra:  "剩余",
			Length: formatTimeLength(int(end - now)),
		}
	}
}

// contestStatus 是状态文案的结构化形式，供模板渲染。
type contestStatus struct {
	State  string // 已结束 / 开始于 / 运行中
	Tone   string // muted / success / danger，对应 PHP 的 text-* class
	Detail string // 结束时间或开始时间
	Extra  string // 总赛时 / 剩余，已结束时为空
	Length string // formatTimeLength 输出，已结束时为空
}

// formatUnix 按 PHP 的默认输出格式渲染时间戳。
func formatUnix(ts int64) string {
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}
