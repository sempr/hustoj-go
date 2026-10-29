package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
}

// Redacted 返回可安全写入日志的数据库定位信息，绝不含口令。
func (d DatabaseConfig) Redacted() string {
	user := d.User
	if user == "" {
		user = "(空)"
	}
	name := d.Name
	if name == "" {
		name = "(空)"
	}
	return fmt.Sprintf("%s@%s:%d/%s", user, d.Host, d.Port, name)
}

// DefaultCommunicationMaxPasses 是通讯题（spj=4）每组数据默认的最大轮数。
//
// 轮数上限只用于挡住写错而无限出题的校验器：正常题目由校验器自己决定何时
// 结束（不再写 nextpass.in）。judge.conf 的 OJ_COMMUNICATION_MAX_PASSES
// 可以覆盖这个默认值。
const DefaultCommunicationMaxPasses = 10

// JudgeConfig holds the main judge configuration
type JudgeConfig struct {
	Database DatabaseConfig
	OJHome   string
	Debug    bool
	Once     bool
	// CommunicationMaxPasses 是通讯题（spj=4）每组数据允许的最大运行轮数，
	// 取自 judge.conf 的 OJ_COMMUNICATION_MAX_PASSES；未配置时用默认值。
	CommunicationMaxPasses int
}

// valueOf 按键名取数据库字段值，供"写了键但留空"的校验使用。
func valueOf(key string, c *JudgeConfig) string {
	switch key {
	case "OJ_HOST_NAME":
		return c.Database.Host
	case "OJ_USER_NAME":
		return c.Database.User
	case "OJ_DB_NAME":
		return c.Database.Name
	}
	return ""
}

// LoadJudgeConf loads configuration from judge.conf file.
//
// 缺失或缺项一律报错，不再回退到 root/password/hustoj。
// 旧实现在 judge.conf 不存在（--ojhome 打错、卷未挂载）时静默返回这组默认值，
// 而它恰是 MariaDB 允许无密码登录时的典型弱口令组合：一旦目标库存在，
// 判题进程会连上错误的库取件并回写判罚，且没有任何日志提示。
func LoadJudgeConf(homePath string) (*JudgeConfig, error) {
	confPath := fmt.Sprintf("%s/etc/judge.conf", homePath)

	file, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("找不到配置文件 %s：请确认 --ojhome 指向 OJ 根目录，或该目录是否已正确挂载", confPath)
		}
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	// 四个必需键一律要求显式出现，不靠默认值蒙混过关：
	// 保留默认值等于把"写漏了"和"就想要这个值"变成同一种状态，
	// 从而静默连到别处。OJ_PASSWORD 例外（空口令本身合法）。
	config := &JudgeConfig{
		OJHome:                 homePath,
		CommunicationMaxPasses: DefaultCommunicationMaxPasses,
	}
	seen := map[string]bool{}

	// Parse key=value pairs
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "OJ_HOST_NAME":
			config.Database.Host = value
			seen[key] = true
		case "OJ_PORT_NUMBER":
			// 旧实现在此静默吞掉解析失败并沿用 3306，
			// 写成非数字端口不会有任何提示。
			port, perr := strconv.Atoi(value)
			if perr != nil {
				return nil, fmt.Errorf("%s 中 OJ_PORT_NUMBER=%q 不是合法端口号", confPath, value)
			}
			config.Database.Port = port
			seen[key] = true
		case "OJ_USER_NAME":
			config.Database.User = value
			seen[key] = true
		case "OJ_PASSWORD":
			// 空口令本身合法（本地 socket / trust 认证），故不强制非空。
			config.Database.Password = value
		case "OJ_DB_NAME":
			config.Database.Name = value
			seen[key] = true
		case "OJ_COMMUNICATION_MAX_PASSES":
			// 通讯题轮数上限：省略时用默认值，写了就必须是正整数，
			// 避免 "0" / 拼错被静默当成默认值而掩盖配置错误。
			n, perr := strconv.Atoi(value)
			if perr != nil || n < 1 {
				return nil, fmt.Errorf("%s 中 OJ_COMMUNICATION_MAX_PASSES=%q 不是正整数（轮数上限至少为 1）", confPath, value)
			}
			config.CommunicationMaxPasses = n
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// 固定顺序报错，避免因 map 遍历顺序导致提示语每次不同
	var missing []string
	for _, k := range []string{"OJ_HOST_NAME", "OJ_PORT_NUMBER", "OJ_USER_NAME", "OJ_DB_NAME"} {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s 缺少必需项: %s", confPath, strings.Join(missing, ", "))
	}
	for _, k := range []string{"OJ_HOST_NAME", "OJ_USER_NAME", "OJ_DB_NAME"} {
		if seen[k] && strings.TrimSpace(valueOf(k, config)) == "" {
			return nil, fmt.Errorf("%s 中 %s 为空值", confPath, k)
		}
	}
	if config.Database.Port <= 0 || config.Database.Port > 65535 {
		return nil, fmt.Errorf("%s 中 OJ_PORT_NUMBER=%d 超出合法端口范围", confPath, config.Database.Port)
	}

	return config, nil
}
