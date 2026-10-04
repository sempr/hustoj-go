package web

import "testing"

// isValidUserName 必须与 my_func.inc.php:589 的逐字节判定一致。
//
// 特别关注三个边界，它们都直接决定线上"查无此人"的分支：
//   - 空串必须通过校验（PHP 循环体不执行直接 return true）
//   - '*' 只在首位合法
//   - 非 ASCII 一律非法（PHP 是按字节判断，不是按码点）
func TestIsValidUserName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"普通名字", "zhblue", true},
		{"含数字", "user123", true},
		{"含连字符下划线", "a-b_c", true},
		{"首位星号", "*admin", true},
		{"星号不在首位", "ad*min", false},
		{"多个星号", "*a*b", false},
		{"空串通过", "", true},
		{"中文非法", "张三", false},
		{"邮箱非法", "a@b.com", false},
		{"感叹号非法", "bad!name", false},
		{"句点非法", "a.b", false},
		{"下划线合法", "SD_J00034", true},
		{"含空格非法", "a b", false},
		{"含斜杠非法", "a/b", false},
	}
	for _, c := range cases {
		if got := isValidUserName(c.in); got != c.want {
			t.Errorf("%s：isValidUserName(%q) = %v，期望 %v", c.name, c.in, got, c.want)
		}
	}
}

// userLevel 必须与模板 userinfo.php:16-17 的倒序覆盖循环一致。
//
// 线上实测基准（取自 PHP 渲染的档案页）：
//
//	sempr AC=1 → "萌新"，距离 小小牛 还需 19 题（20-1）
//	max   AC=6 → "萌新"，距离 小小牛 还需 14 题（20-6）
func TestUserLevel(t *testing.T) {
	cases := []struct {
		ac       int
		wantName string
		wantNext string
		wantNeed int
	}{
		{0, "萌新", "小小牛", 20},
		{1, "萌新", "小小牛", 19},
		{6, "萌新", "小小牛", 14},
		{10, "萌新", "小小牛", 10},
		{19, "萌新", "小小牛", 1},
		{20, "小小牛", "小牛", 10},
		{30, "小牛", "小犇", 20},
		{80, "中牛", "中犇", 20},
		{1000, "锦鲤", "萌新", -990}, // PHP 既有行为：锦鲤档会算出负数
		{2000, "锦鲤", "萌新", -1990},
	}
	for _, c := range cases {
		name, next, need := userLevel(c.ac)
		if name != c.wantName || next != c.wantNext || need != c.wantNeed {
			t.Errorf("AC=%d：得到 (%q, %q, %d)，期望 (%q, %q, %d)",
				c.ac, name, next, need, c.wantName, c.wantNext, c.wantNeed)
		}
	}
}

// getMappedSpecial 对应 my_func.inc.php:280。
func TestGetMappedSpecial(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 学号第 3-6 位（下标 2..5）才是专业码，前两位是入学年份。
		// 这组基准由真实 PHP 的 substr($u,2,4) 逐个跑出。
		{"2307010123", "23人智"},
		{"2307080123", "23网工"},
		{"2357020123", "23电科"},
		{"2357010123", "23软工"},
		{"2302070123", "23数技"},
		{"01010005", ""}, // 取到 0100，表里没有
		{"0237", ""},     // 太短，取到 "37"
		{"", ""},         // 空
	}
	for _, c := range cases {
		if got := getMappedSpecial(c.in); got != c.want {
			t.Errorf("getMappedSpecial(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// gravatarURL 对应模板 userinfo.php:37-44 的邮箱分派。
func TestGravatarURL(t *testing.T) {
	// md5("admin@x.com") 需与 PHP 的 md5(strtolower(trim(...))) 一致。
	if got := string(gravatarURL("admin@x.com")); got !=
		"https://www.gravatar.com/avatar/"+md5Hex("admin@x.com")+"?d=&s=500" {
		t.Errorf("普通邮箱头像地址不对：%s", got)
	}
	// 大小写与首尾空白都要归一化，PHP 用的是 strtolower(trim(...))。
	if string(gravatarURL("  ADMIN@X.com  ")) != string(gravatarURL("admin@x.com")) {
		t.Error("邮箱的大小写/空白未归一化")
	}
	// qq 邮箱走 qlogo.cn，QQ 号取 @ 之前的部分。
	if got := string(gravatarURL("12345@qq.com")); got !=
		"https://q1.qlogo.cn/g?b=qq&nk=12345&s=5" {
		t.Errorf("QQ 邮箱头像地址不对：%s", got)
	}
	// 空邮箱不能生成悬空的 gravatar 路径。
	if got := string(gravatarURL("")); got == "" || got == "https://www.gravatar.com/avatar/?d=&s=500"[len("https://www.gravatar.com/avatar"):] {
		t.Errorf("空邮箱地址不对：%s", got)
	}
}
