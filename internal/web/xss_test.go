package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// 错误文案里混入 </script> 不得逃逸出脚本块（存储型 XSS）。
func TestAlertEscapesScriptBreakout(t *testing.T) {
	s := newTestServer(t, nil, nil)
	rec := httptest.NewRecorder()
	s.alertAndBack(rec, `</script><img src=x onerror=alert(1)>`)
	body := rec.Body.String()
	if strings.Contains(body, "<img") {
		t.Fatalf("标签逃逸出脚本块，构成 XSS:\n%s", body)
	}
	// 原文必须仍以某种形式出现在脚本里（信息不能丢）
	if !strings.Contains(body, "onerror") {
		t.Errorf("提示内容丢失:\n%s", body)
	}
}

// 消息必须被完整包在一个 JS 字符串字面量里，不能拼出第二个语句。
//
// 直接与 template.JSEscapeString 比对并不合适：该函数返回的是不带引号的
// 值，而 html/template 会自行挑选引号（这里选了双引号，于是单引号原样
// 留在字符串里 —— 这是安全的）。真正要保证的是三条不变量，逐条断言：
//  1. 整段输出只有一个 <script> 闭合标签，消息无法逃逸出脚本块；
//  2. history.go(-1) 只出现一次，消息无法注入第二个顶层语句；
//  3. alert(...) 的参数是一个完整闭合的字符串字面量（引号成对且被转义）。
func TestAlertCannotInjectStatement(t *testing.T) {
	for _, msg := range []string{
		`'; alert(1); //`,
		`"); alert(1); //`,
		"换行\n第二行",
		`反斜杠\结尾`,
		`双引号"与'单引号`,
		`</script><img src=x onerror=alert(1)>`,
	} {
		s := newTestServer(t, nil, nil)
		rec := httptest.NewRecorder()
		s.alertAndBack(rec, msg)
		body := rec.Body.String()

		if n := strings.Count(body, "</script>"); n != 1 {
			t.Errorf("消息 %q: 脚本块闭合标签出现 %d 次，应为 1\n%s", msg, n, body)
		}
		if n := strings.Count(body, "history.go(-1)"); n != 1 {
			t.Errorf("消息 %q: history.go(-1) 出现 %d 次，消息注入了额外语句\n%s", msg, n, body)
		}
		if !hasSingleStringArg(body, "alert(") {
			t.Errorf("消息 %q: alert() 的参数不是单个闭合的字符串字面量\n%s", msg, body)
		}
	}
}

// hasSingleStringArg 检查 name( 之后的第一个参数是否是一个完整闭合的
// 字符串字面量，且其中不含未转义的双引号。
func hasSingleStringArg(body, name string) bool {
	i := strings.Index(body, name)
	if i < 0 {
		return false
	}
	rest := body[i+len(name):]
	if !strings.HasPrefix(rest, `"`) {
		return false
	}
	rest = rest[1:]
	for k := 0; k < len(rest); k++ {
		switch rest[k] {
		case '\\': // 跳过被转义的下一个字符
			k++
		case '"':
			// 闭合引号之后必须紧跟 ");" 才是完整调用；
			// 若后面还有别的内容，说明消息拼出了额外语句。
			return strings.HasPrefix(rest[k+1:], `);`)
		case '\n', '\r':
			return false // 裸换行会终止 JS 字符串字面量
		}
	}
	return false
}
