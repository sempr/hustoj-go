// PHP 会话文件的**只读**解析。
//
// 迁移期为什么还要读 PHP 会话：
// 切流前的老用户浏览器里只有 PHPSESSID，Go 必须能认出他，
// 否则一上线所有人瞬间变成匿名。做法是读出登录态后立刻补发一枚
// Go 会话（HJGOSESSID），之后的请求就走 Redis，不再碰这个目录。
//
// 为什么只读、且刻意不做写回：
// 同一个文件 PHP-FPM 自己也在写（vcode、csrf_keys、比赛权限都在里面）。
// 双向可写意味着两套实现要精确对齐序列化细节、字段语义和锁策略，
// 任何一边改动都可能静默清掉另一边的状态 —— 表现是"验证码怎么输都错"
// 这类极难定位的问题。所以架构上切开：登录态各自独立存储，
// PHP 会话对 Go 而言只是迁移输入。
//
// 因此本文件不提供任何写入方法，也不提供 flock：
// 既然不改写，就不需要与 PHP-FPM 争锁，也就不存在半写风险。

package web

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// phpSession 是一个 PHP 会话文件的解析结果。
//
// 保留键序是为了让排障时的输出与文件内容对得上；
// PHP 本身不依赖键序。
type phpSession struct {
	keys []string
	vals map[string]string
}

func newPHPSession() *phpSession {
	return &phpSession{vals: make(map[string]string)}
}

// parsePHPSession 解析 "键|序列化值" 拼接的会话内容。
//
// 解析失败（格式被截断、遇到未知类型）时返回已解析出的部分而不是报错：
// 会话文件可能被并发写坏，此时宁可少拿几个键，也不能让整站 500。
func parsePHPSession(raw string) *phpSession {
	s := newPHPSession()
	i := 0
	for i < len(raw) {
		bar := strings.IndexByte(raw[i:], '|')
		if bar < 0 {
			break
		}
		key := raw[i : i+bar]
		i += bar + 1

		end, ok := skipPHPValue(raw, i)
		if !ok {
			break
		}
		val := raw[i:end]
		i = end

		if key == "" {
			continue
		}
		if _, dup := s.vals[key]; !dup {
			s.keys = append(s.keys, key)
		}
		s.vals[key] = val
	}
	return s
}

// skipPHPValue 从 i 处跳过一个完整的序列化值，返回其结束位置（不含）。
//
// 逐类型走，而不是简单地"找到分号为止"：字符串里可以合法地含有 ';'、
// '{'、'}'，靠扫分隔符会在密码或昵称里出现这些字符时解析错位。
func skipPHPValue(raw string, i int) (int, bool) {
	// 's' 与 'a' 分支会访问 i+2，所以这里要求至少还剩 2 个字节。
	if i+2 > len(raw) {
		return 0, false
	}
	switch raw[i] {
	case 'N':
		// N;  —— null
		if strings.HasPrefix(raw[i:], "N;") {
			return i + 2, true
		}
		return 0, false

	case 'b', 'i', 'd':
		// b:0;  i:-12;  d:1.5;
		semi := strings.IndexByte(raw[i:], ';')
		if semi < 0 {
			return 0, false
		}
		return i + semi + 1, true

	case 's':
		// s:<字节长度>:"<内容>";
		// 注意 raw[i+1] 本身就是第一个 ':'，长度要从 i+2 起找下一个冒号。
		colon := strings.IndexByte(raw[i+2:], ':')
		if colon < 0 {
			return 0, false
		}
		lenEnd := i + 2 + colon
		n, err := strconv.Atoi(raw[i+2 : lenEnd])
		if err != nil || n < 0 {
			return 0, false
		}
		// lenEnd 指向 ':'，其后必须是 '"'
		if lenEnd+1 >= len(raw) || raw[lenEnd+1] != '"' {
			return 0, false
		}
		contentStart := lenEnd + 2
		contentEnd := contentStart + n
		if contentEnd+1 >= len(raw) || raw[contentEnd] != '"' || raw[contentEnd+1] != ';' {
			return 0, false
		}
		return contentEnd + 2, true

	case 'a':
		// a:<元素数>:{ 键;值 键;值 ... }
		colon := strings.IndexByte(raw[i+2:], ':')
		if colon < 0 {
			return 0, false
		}
		countEnd := i + 2 + colon
		count, err := strconv.Atoi(raw[i+2 : countEnd])
		if err != nil || count < 0 {
			return 0, false
		}
		if countEnd+1 >= len(raw) || raw[countEnd+1] != '{' {
			return 0, false
		}
		pos := countEnd + 2
		for k := 0; k < count; k++ {
			// 键：i:0; 或 s:1:"0";
			end, ok := skipPHPValue(raw, pos)
			if !ok {
				return 0, false
			}
			pos = end
			// 值
			end, ok = skipPHPValue(raw, pos)
			if !ok {
				return 0, false
			}
			pos = end
		}
		if pos >= len(raw) || raw[pos] != '}' {
			return 0, false
		}
		return pos + 1, true
	}
	return 0, false
}

// has 报告键是否存在。
func (s *phpSession) has(key string) bool {
	_, ok := s.vals[key]
	return ok
}

// allKeys 返回全部会话键（保持解析时的原始顺序），主要用于调试与测试。
func (s *phpSession) allKeys() []string {
	out := make([]string, 0, len(s.vals))
	for _, k := range s.keys {
		if _, ok := s.vals[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// getString 取出字符串值。类型不符时返回 ok=false。
func (s *phpSession) getString(key string) (string, bool) {
	raw, ok := s.vals[key]
	if !ok {
		return "", false
	}
	return phpStringValue(raw)
}

// getBool 取布尔值。兼容 PHP 里 true 被写成 b:1 或字符串 "1"。
func (s *phpSession) getBool(key string) bool {
	raw, ok := s.vals[key]
	if !ok {
		return false
	}
	switch raw {
	case "b:1;":
		return true
	case "b:0;", "N;":
		return false
	}
	if str, ok := phpStringValue(raw); ok {
		return str != "" && str != "0"
	}
	if n, ok := phpIntValue(raw); ok {
		return n != 0
	}
	return false
}

// getStringSlice 取字符串数组，用于 csrf_keys 池。
func (s *phpSession) getStringSlice(key string) []string {
	raw, ok := s.vals[key]
	if !ok || len(raw) < 6 || raw[0] != 'a' || raw[1] != ':' {
		return nil
	}
	// 元素数之后还有第二个冒号（"a:<count>:{...}"）。
	colon := strings.IndexByte(raw[2:], ':')
	if colon < 0 {
		return nil
	}
	countEnd := 2 + colon
	count, err := strconv.Atoi(raw[2:countEnd])
	if err != nil || count < 0 {
		return nil
	}
	if countEnd+1 >= len(raw) || raw[countEnd+1] != '{' {
		return nil
	}
	pos := countEnd + 2
	out := make([]string, 0, count)
	for k := 0; k < count; k++ {
		end, ok := skipPHPValue(raw, pos)
		if !ok {
			return out
		}
		pos = end
		end, ok = skipPHPValue(raw, pos)
		if !ok {
			return out
		}
		if v, ok := phpStringValue(raw[pos:end]); ok {
			out = append(out, v)
		}
		pos = end
	}
	return out
}

// phpSerializeString 生成 s:LEN:"...";
// 长度是字节数而非字符数：PHP strlen() 计的是字节，
// 昵称/学校名含中文时按字符数算会被 PHP 判为损坏的序列化数据。
func phpSerializeString(s string) string {
	return fmt.Sprintf("s:%d:\"%s\";", len(s), s)
}

// phpStringValue 从 s:LEN:"..."; 取出内容。
func phpStringValue(raw string) (string, bool) {
	if len(raw) < 4 || raw[0] != 's' || raw[1] != ':' {
		return "", false
	}
	colon := strings.IndexByte(raw[2:], ':')
	if colon < 0 {
		return "", false
	}
	n, err := strconv.Atoi(raw[2 : 2+colon])
	if err != nil || n < 0 {
		return "", false
	}
	start := 2 + colon + 2 // 跳过开头的引号
	if start+n > len(raw) {
		return "", false
	}
	return raw[start : start+n], true
}

// phpIntValue 从 i:123; 取出整数。
func phpIntValue(raw string) (int64, bool) {
	if len(raw) < 3 || raw[0] != 'i' || raw[1] != ':' {
		return 0, false
	}
	semi := strings.LastIndexByte(raw, ';')
	if semi < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(raw[2:semi], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// phpSessionIDSafe 限制会话 ID 的字符集。
//
// PHP 只会生成 [A-Za-z0-9,-]，这里用白名单而不是只挡 "/" 与 "\"：
// 白名单同时挡住了 NUL、换行、以及 ".." 之外的编码变体，
// 避免 session_dir 配错时把读写引到目录之外。
var phpSessionIDSafe = regexp.MustCompile(`^[A-Za-z0-9,-]{1,128}$`)

// sessionIDAllowed 报告会话 ID 是否可安全用于文件名。
func sessionIDAllowed(sid string) bool {
	return sid != "" && len(sid) <= 128 && !strings.Contains(sid, "..") && phpSessionIDSafe.MatchString(sid)
}

// SessionStore 读写 PHP 会话文件。
type SessionStore struct {
	dir    string
	prefix string // $OJ_NAME，会话键前缀（不含结尾下划线）
}

// NewSessionStore 创建会话存储。prefix 来自 $OJ_NAME。
func NewSessionStore(dir, ojName string) *SessionStore {
	return &SessionStore{dir: dir, prefix: ojName}
}

// key 把逻辑键名（如 "user_id"）转成实际的会话键（"$OJ_NAME_user_id"）。
func (st *SessionStore) key(name string) string {
	return st.prefix + "_" + name
}

// path 返回会话文件路径，并对会话 ID 做白名单校验。
func (st *SessionStore) path(sid string) (string, error) {
	if !sessionIDAllowed(sid) {
		return "", fmt.Errorf("非法的会话 ID")
	}
	return filepath.Join(st.dir, "sess_"+sid), nil
}

// get 读取并解析 PHP 会话文件。
//
// 只读。这是本包对 PHP 会话目录的唯一访问方式 —— Go 不再写回，
// 也不再删除：PHP 侧仍在活跃使用这些文件，Go 的写回会与
// PHP-FPM 争抢同一批文件，并让"登录态由谁拥有"在两端分裂。
//
// 读之前先 stat 再开：O_CREATE 是写路径的语义，只读场景绝不能
// 顺手把不存在的会话文件创建出来 —— 那会在 PHP 的会话目录里
// 凭空造出一堆垃圾文件。
func (st *SessionStore) get(sid string) (*phpSession, error) {
	p, err := st.path(sid)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errSessionNotFound
		}
		return nil, err
	}
	return parsePHPSession(string(raw)), nil
}
