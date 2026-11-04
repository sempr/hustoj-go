package web

import (
	"regexp"
	"strconv"
	"strings"
)

var sessionIDSafe = regexp.MustCompile(`^[A-Za-z0-9,-]{1,128}$`)

// sessionIDAllowed 与旧 PHP 会话 ID 白名单保持一致，供 Go 会话 ID
// 的校验继续复用同一套口径。
func sessionIDAllowed(sid string) bool {
	return sid != "" && len(sid) <= 128 && !strings.Contains(sid, "..") && sessionIDSafe.MatchString(sid)
}

// contestIDFromSuffix 解析 "c1000"/"m1000" 形式的 Ivan suffix。
func contestIDFromSuffix(suffix string, kind byte) (int32, bool) {
	if len(suffix) < 2 || suffix[0] != kind {
		return 0, false
	}
	n, err := strconv.Atoi(suffix[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return int32(n), true
}
