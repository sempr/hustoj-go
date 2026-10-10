// Package config 提供 Web 服务的运行配置。
//
// 配置有两个来源，环境变量优先级更高：
//
//  1. web.toml —— 运营参数（监听地址、模板、会话等），位于 <OJHome>/etc/web.toml。
//     该文件不含任何凭据，可按需提交到配置管理。
//
//  2. 环境变量 —— 由 systemd 的 EnvironmentFile=<OJHome>/etc/judge.conf 注入。
//     数据库凭据只走这条路径：judge.conf 是既有部署资产（root:root 0600），
//     Web 服务复用它而不另建一份，也无需放宽其权限
//     （systemd 由 PID 1 以 root 身份读取 EnvironmentFile，再降权执行进程）。
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	ojconfig "github.com/sempr/hustoj-go/pkg/config"
)

// Config 是 Web 服务的完整运行配置。
type Config struct {
	// Listen 是 HTTP 监听地址。默认只绑回环，由 nginx 反代对外，
	// 避免 Web 端口绕过 TLS 与限流直接暴露。
	Listen string `toml:"listen"`

	// OJHome 是 OJ 根目录，用于定位 etc/ 与 data/。
	OJHome string `toml:"oj_home"`

	// Template 是默认皮肤。
	Template string `toml:"template"`

	// TrustedProxies 列出可信反向代理网段；仅这些来源的 X-Forwarded-* 会被采信。
	TrustedProxies []string `toml:"trusted_proxies"`

	// SessionDir 是 PHP session 文件目录，用于迁移期的 PHP 会话桥。
	SessionDir string `toml:"session_dir"`

	// SessionBridge 启用 PHP session 只读桥。
	//
	// 只读：Go 只解析 PHP 写入的会话以识别身份，绝不写回。
	// 写回会与 PHP-FPM 争抢同一批文件，且一旦 Go 开始写 user_id，
	// 两端对"谁拥有登录态"的约定就分裂了，后续下线 PHP 时还要再迁一次。
	// PHP 下线后置 false 并移除相关代码。
	SessionBridge bool `toml:"session_bridge"`

	// RedisURL 是 Go 自身会话的 Redis 连接串。
	//
	// Go 的登录态与 CSRF 全部存这里，与 PHP 会话完全解耦。
	// 为空时禁用 Go 会话，此时所有请求按匿名处理（仅调试用）。
	RedisURL string `toml:"redis_url"`

	// GoSessionTTL 是 Go 会话在 Redis 中的存活时长（秒）。
	// 每次成功的请求都会续期，所以活跃用户不会掉登录。
	GoSessionTTL int `toml:"go_session_ttl"`

	// GoSessionCookie 是 Go 会话 Cookie 名。
	//
	// 与 PHPSESSID 分开：迁移期两者共存（PHP 会话用于识别老用户，
	// Go 会话用于实际登录态），同名会让新 Cookie 覆盖旧会话。
	GoSessionCookie string `toml:"go_session_cookie"`

	// OJName 对应 PHP 的 $OJ_NAME（phpweb/include/db_info.inc.php:16），
	// 决定会话键前缀："<OJName>_user_id"、"<OJName>_csrf_keys" 等。
	//
	// 必须与 PHP 侧逐字节相同。线上实测为 "吉尔吉斯倔驴大学OJ"。
	// 配错不会报错，只会让所有人被当成匿名、CSRF 恒失败，
	// 因此启用 session_bridge 时该值不能为空（见 validate）。
	OJName string `toml:"oj_name"`

	// Debug 输出调试信息（含路由命中），生产必须为 false。
	Debug bool `toml:"debug"`

	// VCode 对应 PHP 的 $OJ_VCODE：登录/注册要求图形验证码。
	VCode bool `toml:"vcode"`

	// Register 对应 PHP 的 $OJ_REGISTER：是否允许注册。
	Register bool `toml:"register"`

	// LoginMod 对应 PHP 的 $OJ_LOGIN_MOD。register.php:32 在该值
	// 不是 "hustoj" 时禁止注册，其余模块（ldap/discuz/moodle）走外部认证。
	LoginMod string `toml:"login_mod"`

	// LoginFailLimit 对应 PHP 的 $OJ_LOGIN_FAIL_LIMIT：
	// 5 分钟内同一账号的失败上限，以及同 IP 的上限（该值 × 4）。
	LoginFailLimit int `toml:"login_fail_limit"`

	// NeedLogin 对应 PHP 的 $OJ_NEED_LOGIN。
	NeedLogin bool `toml:"need_login"`

	// ExamContestID 对应 PHP 的 $OJ_EXAM_CONTEST_ID，大于 0 时启用考试模式：
	// 考试开始后首次登录的 IP 会被记住，之后必须一致（管理员豁免）。
	ExamContestID int32 `toml:"exam_contest_id"`

	// FreePractice 对应 PHP 的 $OJ_FREE_PRACTICE：开启后题目列表不再排除
	// 比赛进行中的题目。必须在 Go 侧显式配置 —— Go 刻意不解析
	// db_info.inc.php（那是 PHP 侧的配置来源，且内含明文口令），
	// 所以两端各有一份开关，发布时必须同步，否则题目列表可见范围不一致。
	FreePractice bool `toml:"free_practice"`

	// NoIPKeyword 对应 PHP 的 $OJ_NOIP_KEYWORD：比赛标题含此关键字时
	// 启用 NOIP 模式，赛中隐藏 AC 数与提交数，防止泄题。
	NoIPKeyword string `toml:"noip_keyword"`

	// NoIPHint 对应 PHP 的 $OJ_NOIP_HINT。false（PHP 默认）表示 NOIP 模式下
	// 连题目提示一起隐藏；true 则保留提示。
	NoIPHint bool `toml:"noip_hint"`

	// ShowDiff 对应 PHP 的 $OJ_SHOW_DIFF（db_info.inc.php:70）。
	// status-ajax.php 判断"本人能否查看判题详情（tr=1）"时要求它为真；
	// 关闭后本人也看不到运行/编译错误详情。线上为 true。
	ShowDiff bool `toml:"show_diff"`

	// HideRightAnswer 对应 $OJ_HIDE_RIGHT_ANSWER（db_info.inc.php:71）。
	// status-ajax.php 对 spj==2 的题目在开启时返回"拒绝访问"提示，
	// 防止泄露填空/判断题答案。线上为 true。
	HideRightAnswer bool `toml:"hide_right_answer"`

	// PublicStatus 对应 $OJ_PUBLIC_STATUS（db_info.inc.php:102）。
	// 关闭后 status-ajax.php 只对本人或 source_browser 返回判题结果，
	// 其余请求返回 NOIP 警告文案。线上为 true。
	PublicStatus bool `toml:"public_status"`

	// RemoteJudge 对应 $OJ_REMOTE_JUDGE（db_info.inc.php:97）。
	// Go 侧不实现远程评测触发（status-ajax.php 的 trigger_judge），
	// 仅保留开关以对齐配置；线上为 false。
	RemoteJudge bool `toml:"remote_judge"`

	// RankLockPercent 对应 PHP 的 $OJ_RANK_LOCK_PERCENT：封榜时间占比赛
	// 总时长的比例。0.2 表示 5 小时的比赛最后 1 小时封榜。
	//
	// 封榜期内，提交时间早于封榜时刻的记录会隐藏结果与资源占用
	//（status.php:69-78、:402）。PHP 默认值为 0（不封榜），这里保持一致：
	// 封榜会直接改变比赛期间可见的信息，不能由 Go 侧单方面开启。
	RankLockPercent float64 `toml:"rank_lock_percent"`

	// RankHidden 对应 PHP 的 $OJ_RANK_HIDDEN：从排行榜中排除的账号。
	//
	// PHP 侧直接把它拼进 SQL（`user_id not in ('admin','zhblue')`），
	// 因此非管理员看不到这些人的任何页面 —— userinfo.php:107 同样
	// 带这个条件，所以匿名访客访问 ?user=admin 得到的是 "No such User!"，
	// 而不是管理员档案。这一点很容易在迁移时丢掉，导致被隐藏的账号
	// 反而能通过档案页泄露 solved/submit 等数据。
	//
	// 线上值为 "admin","zhblue"（db_info.inc.php:87）。
	RankHidden []string `toml:"rank_hidden"`

	// CEPenalty 对应 PHP 的 $OJ_CE_PENALTY：编译错误（result=11）是否计入
	// ACM 排名罚时。
	//
	// PHP 侧 contestrank.php:Add 的判据是
	//	`if (isset($OJ_CE_PENALTY) && !$OJ_CE_PENALTY && $res == 11) return;`
	// 注意 isset() 对取值为 false 的 static 变量同样返回 true，所以线上
	// 实际执行的是"CE 直接返回、不计错误次数"。
	//
	// 线上 db_info.inc.php:36 是 `static $OJ_CE_PENALTY=false;`，即 CE 不
	// 罚时。弄反会让"有 CE 但未通过"的题从空白变成 -1，是排名页的可见差异，
	// 必须与 PHP 同步。
	CEPenalty bool `toml:"ce_penalty"`

	// Database 来自 judge.conf 的环境变量，不来自 web.toml。
	// 复用 pkg/config 的类型，以便与判题机共用 repository.DSN。
	Database ojconfig.DatabaseConfig

	// ── 提交相关（对应 db_info.inc.php 的 $OJ_* 提交参数）─────────────
	//
	// 以下开关的 PHP 侧定义在 phpweb/include/db_info.inc.php。Go 刻意不解析
	// 该文件（内含明文口令），所以两端各有一份，发布时必须同步。

	// LangMask 对应 $OJ_LANGMASK：允许提交的语言位掩码。
	// PHP：$langmask & (1 << $language) 命中即拒绝，即掩码里为 0 的语言不可用。
	// 线上实测值见 db_info.inc.php:29。
	LangMask int32 `toml:"lang_mask"`

	// SubmitCooldown 对应 $OJ_SUBMIT_COOLDOWN_TIME：两次提交的最小间隔（秒）。
	// PHP 侧为 0 时会退化成"同一秒内第二次提交被静默丢弃并跳转到 status"，
	// 因此这里要求为正。
	SubmitCooldown int `toml:"submit_cooldown"`

	// AppendCode 对应 $OJ_APPENDCODE：是否按题目拼接 prepend/append 模板代码。
	// 关闭会改变送进编译器的源码，属于判题语义变化，必须与 PHP 一致。
	AppendCode bool `toml:"append_code"`

	// OIOneSolutionOnly 对应 $OJ_OI_1_SOLUTION_ONLY：NOIP 式比赛中只保留
	// 最后一次提交，新提交发生时把同场同题的历史提交记作"测试运行"（result=13）
	// 并把通过率清零。
	OIOneSolutionOnly bool `toml:"oi_one_solution_only"`

	// VCodePendingThreshold 对应 submit.php:26-29：
	// 待判题队列中 result<4 的行数超过该值时，提交也要求图形验证码。
	// 生产上判题机与 Web 同机，这个值实际起到了自保护的作用：
	// 队列堆积时堵住新提交，避免雪崩。
	VCodePendingThreshold int64 `toml:"vcode_pending_threshold"`

	// MaxSourceLength 是送判源码的字节上限。
	//
	// 刻意取 65535 而不是 PHP 的 65536：PHP 只判 `$len > 65536`，
	// 允许恰好 65536 字节的源码进入 INSERT，但 source_code.source /
	// source_code_user.source 都是 TEXT（上限 65535 字节），而线上
	// MariaDB 开着 STRICT_TRANS_TABLES。那一字节会触发 1406 Data too
	// long，INSERT 失败后 PHP 仍继续往下走，solution_id 为 0，
	// 用户看到的是"点了提交，什么都没发生"。
	//
	// 按列的真实容量设限，让边界处要么明确通过、要么明确拒绝，
	// 不留静默失败的窗口。
	MaxSourceLength int `toml:"max_source_length"`

	// OnSiteContestID 对应 PHP 的 $OJ_ON_SITE_CONTEST_ID：大于 0 时全站
	// 重定向到指定比赛（现场赛模式）。生产未配置（注释掉），默认 0。
	OnSiteContestID int32 `toml:"on_site_contest_id"`

	// IndexNewsTitle 对应 PHP 的 $OJ_INDEX_NEWS_TITLE：首页显示的
	// 新闻标题。线上值为 "HelloWorld!"（db_info.inc.php:94）。
	IndexNewsTitle string `toml:"index_news_title"`

	// LongLogin 对应 PHP 的 $OJ_LONG_LOGIN：启用长期登录自动刷新。
	// 线上为 false（db_info.inc.php:63）。
	LongLogin bool `toml:"long_login"`

	// MenuNews 对应 PHP 的 $OJ_MENU_NEWS：导航栏是否显示菜单新闻。
	// 线上为 true（db_info.inc.php:151）。
	MenuNews bool `toml:"menu_news"`

	// MathJax 对应 PHP 的 $OJ_MATHJAX：是否加载 MathJax 公式渲染。
	// 线上为 true（db_info.inc.php:75）。
	MathJax bool `toml:"mathjax"`

	// Lang 对应 PHP 的 $OJ_LANG：默认语言。线上为 "cn"（db_info.inc.php:26）。
	Lang string `toml:"lang"`
}

// Default 返回内置默认值。
func Default() *Config {
	return &Config{
		Listen:          "127.0.0.1:8080",
		OJHome:          "/home/judge",
		Template:        "syzoj",
		TrustedProxies:  []string{"127.0.0.1/32", "::1/128"},
		SessionDir:      "/var/lib/php/sessions",
		SessionBridge:   true,
		RedisURL:        "redis://127.0.0.1:6379/0",
		GoSessionTTL:    86400,
		GoSessionCookie: goSessionCookieDefault,
		OJName:          "hustoj",
		Debug:           false,
		// 默认 false，与 PHP 的 $OJ_FREE_PRACTICE 默认值一致。
		// 打开它会让题目列表包含比赛进行中的题目，属于可见范围变化，
		// 必须显式开启。
		FreePractice: false,
		// 与 phpweb/include/db_info.inc.php:85 保持一致。
		NoIPKeyword: "noip",
		// 与 db_info.inc.php:86 保持一致（false = 隐藏提示）。
		NoIPHint: false,
		// 以下与线上 db_info.inc.php:70/71/102/97 保持一致。
		ShowDiff:        true,
		HideRightAnswer: true,
		PublicStatus:    true,
		RemoteJudge:     false,
		// 与 db_info.inc.php:66 保持一致（0 = 不封榜）。
		RankLockPercent: 0,
		// 与 db_info.inc.php:87 保持一致。
		RankHidden: []string{"admin", "zhblue"},
		// 与 db_info.inc.php:36 保持一致（false = 编译错误不罚时）。
		CEPenalty: false,
		// 以下与 db_info.inc.php:32/36/59/60/64/65 的默认值一致。
		VCode:          false,
		Register:       true,
		LoginMod:       "hustoj",
		LoginFailLimit: 5,
		NeedLogin:      false,
		ExamContestID:  0,
		// 以下默认值取自线上 db_info.inc.php，发布时以 PHP 侧为准。
		LangMask:              16579600,
		SubmitCooldown:        10,
		AppendCode:            true,
		OIOneSolutionOnly:     false,
		VCodePendingThreshold: 50,
		MaxSourceLength:       65535,
		// 以下与线上 db_info.inc.php 保持一致。
		OnSiteContestID: 0, // 生产未配置（注释掉）
		IndexNewsTitle:  "HelloWorld!",
		LongLogin:       false,
		MenuNews:        true,
		MathJax:         true,
		Lang:            "cn",
	}
}

// Load 按 web.toml → 环境变量 的顺序装配配置。
// web.toml 缺失不算错误（可纯靠环境变量运行）；存在但解析失败则是错误。
func Load(ojHome, webTOML string) (*Config, error) {
	cfg := Default()
	if ojHome != "" {
		cfg.OJHome = ojHome
	}

	if webTOML == "" {
		webTOML = filepath.Join(cfg.OJHome, "etc", "web.toml")
	}
	if err := applyTOML(cfg, webTOML); err != nil {
		return nil, err
	}

	applyEnv(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyTOML 用 web.toml 覆盖默认值。文件不存在时静默跳过。
func applyTOML(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	// 严格解码：未知键报错，避免拼写错误被静默忽略
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return nil
}

// applyEnv 用环境变量覆盖配置。空值视为未设置，不覆盖已有配置。
func applyEnv(cfg *Config) {
	setStr(&cfg.Listen, "HUSTOJ_WEB_LISTEN")
	setStr(&cfg.Template, "OJ_TEMPLATE")
	setStr(&cfg.SessionDir, "HUSTOJ_WEB_SESSION_DIR")
	setStr(&cfg.OJName, "HUSTOJ_WEB_OJ_NAME")
	setBool(&cfg.Debug, "HUSTOJ_WEB_DEBUG")
	setBool(&cfg.SessionBridge, "HUSTOJ_WEB_SESSION_BRIDGE")
	setStr(&cfg.RedisURL, "HUSTOJ_WEB_REDIS_URL")
	setStr(&cfg.GoSessionCookie, "HUSTOJ_WEB_SESSION_COOKIE")
	setBool(&cfg.FreePractice, "HUSTOJ_WEB_FREE_PRACTICE")
	setString(&cfg.NoIPKeyword, "HUSTOJ_WEB_NOIP_KEYWORD")
	setBool(&cfg.NoIPHint, "HUSTOJ_WEB_NOIP_HINT")
	setBool(&cfg.ShowDiff, "HUSTOJ_WEB_SHOW_DIFF")
	setBool(&cfg.HideRightAnswer, "HUSTOJ_WEB_HIDE_RIGHT_ANSWER")
	setBool(&cfg.PublicStatus, "HUSTOJ_WEB_PUBLIC_STATUS")
	setBool(&cfg.RemoteJudge, "HUSTOJ_WEB_REMOTE_JUDGE")
	setBool(&cfg.CEPenalty, "HUSTOJ_WEB_CE_PENALTY")
	setBool(&cfg.VCode, "HUSTOJ_WEB_VCODE")
	setBool(&cfg.Register, "HUSTOJ_WEB_REGISTER")
	setStr(&cfg.LoginMod, "HUSTOJ_WEB_LOGIN_MOD")
	setInt(&cfg.LoginFailLimit, "HUSTOJ_WEB_LOGIN_FAIL_LIMIT")
	setInt(&cfg.GoSessionTTL, "HUSTOJ_WEB_SESSION_TTL")
	setBool(&cfg.NeedLogin, "HUSTOJ_WEB_NEED_LOGIN")
	setInt32(&cfg.ExamContestID, "HUSTOJ_WEB_EXAM_CONTEST_ID")
	setInt32(&cfg.OnSiteContestID, "HUSTOJ_WEB_ON_SITE_CONTEST_ID")
	setStr(&cfg.IndexNewsTitle, "HUSTOJ_WEB_INDEX_NEWS_TITLE")
	setBool(&cfg.LongLogin, "HUSTOJ_WEB_LONG_LOGIN")
	setBool(&cfg.MenuNews, "HUSTOJ_WEB_MENU_NEWS")
	setBool(&cfg.MathJax, "HUSTOJ_WEB_MATHJAX")
	setStr(&cfg.Lang, "HUSTOJ_WEB_LANG")
	if v, ok := lookupEnv("OJ_HOME"); ok {
		cfg.OJHome = v
	}
	if v, ok := lookupEnv("HUSTOJ_WEB_TRUSTED_PROXIES"); ok {
		cfg.TrustedProxies = splitList(v)
	}
	if v, ok := lookupEnv("HUSTOJ_WEB_RANK_HIDDEN"); ok {
		cfg.RankHidden = splitList(v)
	}

	// 数据库凭据：键名与 judge.conf 保持一致
	setStr(&cfg.Database.Host, "OJ_HOST_NAME")
	setInt(&cfg.Database.Port, "OJ_PORT_NUMBER")
	setStr(&cfg.Database.User, "OJ_USER_NAME")
	setStr(&cfg.Database.Password, "OJ_PASSWORD")
	setStr(&cfg.Database.Name, "OJ_DB_NAME")
}

func lookupEnv(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	return v, v != ""
}

func setStr(dst *string, key string) {
	if v, ok := lookupEnv(key); ok {
		*dst = v
	}
}

func setInt(dst *int, key string) {
	if v, ok := lookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
		// 非法整数保持原值：宁可用默认值也不要静默用 0 端口
	}
}

func setBool(dst *bool, key string) {
	if v, ok := lookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}

func setInt32(dst *int32, key string) {
	if v, ok := lookupEnv(key); ok {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			*dst = int32(n)
		}
	}
}

func setString(dst *string, key string) {
	if v, ok := lookupEnv(key); ok {
		*dst = v
	}
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validate 检查必填项，避免服务在缺配置时启动到一半才失败。
func (c *Config) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("监听地址为空：设置 web.toml 的 listen 或环境变量 HUSTOJ_WEB_LISTEN")
	}
	if c.Database.Host == "" {
		return fmt.Errorf("数据库主机为空：judge.conf 未注入 OJ_HOST_NAME（检查 systemd 的 EnvironmentFile）")
	}
	if c.Database.Name == "" {
		return fmt.Errorf("数据库名为空：judge.conf 未注入 OJ_DB_NAME（检查 systemd 的 EnvironmentFile）")
	}
	if c.Database.Port <= 0 || c.Database.Port > 65535 {
		return fmt.Errorf("数据库端口非法: %d", c.Database.Port)
	}
	if c.RedisURL == "" {
		return fmt.Errorf("redis_url 为空：Go 会话依赖 Redis，不设置会导致全员掉登录")
	}
	if c.GoSessionTTL <= 0 {
		return fmt.Errorf("go_session_ttl 必须为正数，收到 %d", c.GoSessionTTL)
	}
	if c.GoSessionCookie == "" {
		return fmt.Errorf("go_session_cookie 为空")
	}
	if c.GoSessionCookie == phpSessionCookieNameConst {
		// 同名会让 Go 的 Cookie 覆盖 PHP 的会话，迁移期老用户直接掉登录。
		return fmt.Errorf("go_session_cookie 不能与 PHP 的 %s 同名", phpSessionCookieNameConst)
	}
	if !cookieNameAllowed(c.GoSessionCookie) {
		return fmt.Errorf("go_session_cookie 含非法字符: %q", c.GoSessionCookie)
	}
	if c.SessionBridge {
		if c.SessionDir == "" {
			return fmt.Errorf("启用了 session_bridge 但未设置 session_dir")
		}
		if c.OJName == "" {
			// 缺这个值的后果是静默的：会话文件读得到但没有一个键名对得上，
			// 表现为全员匿名 + CSRF 恒失败。宁可启动失败。
			return fmt.Errorf("启用了 session_bridge 但未设置 oj_name（必须与 PHP 的 $OJ_NAME 逐字节相同）")
		}
	}
	return nil
}

// goSessionCookieDefault 是 Go 会话 Cookie 的默认名。
//
// 刻意不叫 PHPSESSID：迁移期两个会话并存，同名会互相覆盖。
const goSessionCookieDefault = "HJGOSESSID"

// phpSessionCookieNameConst 仅为让 validate 能在不依赖 web 包的情况下
// 表达"两个 Cookie 不能同名"这条约束。
const phpSessionCookieNameConst = "PHPSESSID"

// cookieNameAllowed 校验 Cookie 名。
//
// RFC 6265 的 token 字符集：不允许空格、逗号、分号、反斜杠与控制字符 ——
// 这几个都会让 SetCookie 生成畸形头部，或让浏览器忽略整个属性。
func cookieNameAllowed(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= 0x20 || c >= 0x7f {
			return false
		}
		if strings.IndexByte("()<>@,;:\\\"/[]?={}", c) >= 0 {
			return false
		}
	}
	return len(name) > 0
}

// RedisURLRedacted 返回脱敏后的 Redis 连接串。
//
// 启动日志会打印它用于排障。不能用 url.UserPassword 重新拼装：
// 它会把替换用的 "***" 百分号编码成 %2A%2A%2A，日志里反而更难认。
func (c Config) RedisURLRedacted() string {
	if c.RedisURL == "" {
		return "(空)"
	}
	u, err := url.Parse(c.RedisURL)
	if err != nil {
		return "(无法解析的 redis_url)"
	}
	if u.User == nil || u.User.Username() == "" {
		// 没有 userinfo，说明未配密码，原样输出即可。
		return u.String()
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "redis"
	}
	return scheme + "://" + u.User.Username() + ":***@" + u.Host + u.Path
}
