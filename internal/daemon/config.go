package daemon

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config stores all configuration for judged.
type Config struct {
	OJHome         string
	Debug          bool
	Once           bool
	HostName       string
	UserName       string
	Password       string
	DBName         string
	PortNumber     int
	MaxRunning     int
	SleepTime      int
	TotalJudges    int
	JudgeMod       int
	LangSet        string
	HTTPJudge      bool
	HTTPBaseURL    string
	HTTPAPIPath    string
	HTTPLoginPath  string
	HTTPUsername   string
	HTTPPassword   string
	RedisEnable    bool
	RedisServer    string
	RedisPort      int
	RedisAuth      string
	RedisQName     string
	UDPEable       bool
	UDPServer      string
	UDPPort        int
	UseDocker      bool
	DockerPath     string
	InternalClient bool
	TurboMode      int
}

// LoadConfig reads the judge.conf file and returns a Config struct.
func LoadConfig(path string) (*Config, error) {
	// Default values
	cfg := &Config{
		PortNumber:     3306,
		MaxRunning:     3,
		SleepTime:      1,
		TotalJudges:    1,
		JudgeMod:       0,
		LangSet:        "0,1,3,6",
		DockerPath:     "/usr/bin/docker",
		UDPServer:      "127.0.0.1",
		UDPPort:        1536,
		InternalClient: true,
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		assignConfigValue(cfg, key, value)
	}

	return cfg, scanner.Err()
}

func assignConfigValue(cfg *Config, key, value string) {
	switch key {
	case "OJ_HOST_NAME":
		cfg.HostName = value
	case "OJ_USER_NAME":
		cfg.UserName = value
	case "OJ_PASSWORD":
		cfg.Password = value
	case "OJ_DB_NAME":
		cfg.DBName = value
	case "OJ_PORT_NUMBER":
		cfg.PortNumber, _ = strconv.Atoi(value)
	case "OJ_RUNNING":
		cfg.MaxRunning, _ = strconv.Atoi(value)
	case "OJ_SLEEP_TIME":
		cfg.SleepTime, _ = strconv.Atoi(value)
	case "OJ_TOTAL":
		cfg.TotalJudges, _ = strconv.Atoi(value)
	case "OJ_MOD":
		cfg.JudgeMod, _ = strconv.Atoi(value)
	case "OJ_LANG_SET":
		cfg.LangSet = value
	case "OJ_HTTP_JUDGE":
		cfg.HTTPJudge, _ = strconv.ParseBool(value)
	case "OJ_HTTP_BASEURL":
		cfg.HTTPBaseURL = value
	// ... and so on for all other configuration keys ...
	case "OJ_REDISENABLE":
		v, _ := strconv.Atoi(value)
		cfg.RedisEnable = (v == 1)
	case "OJ_REDISSERVER":
		cfg.RedisServer = value
	case "OJ_REDISPORT":
		cfg.RedisPort, _ = strconv.Atoi(value)
	case "OJ_REDISAUTH":
		cfg.RedisAuth = value
	case "OJ_REDISQNAME":
		cfg.RedisQName = value
	case "OJ_USE_DOCKER":
		v, _ := strconv.Atoi(value)
		cfg.UseDocker = (v == 1)
	case "OJ_DOCKER_PATH":
		cfg.DockerPath = value
	case "OJ_INTERNAL_CLIENT":
		v, _ := strconv.Atoi(value)
		cfg.InternalClient = (v == 1)
	case "OJ_TURBO_MODE":
		cfg.TurboMode, _ = strconv.Atoi(value)
	}
}
