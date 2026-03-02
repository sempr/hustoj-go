package daemon

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/config"
)

// DaemonConfig stores all configuration for judged daemon
type DaemonConfig struct {
	*config.JudgeConfig
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
	UDPEnable      bool
	UDPServer      string
	UDPPort        int
	UseDocker      bool
	DockerPath     string
	InternalClient bool
	TurboMode      int
}

// LoadDaemonConfig reads judge.conf file and returns a DaemonConfig struct
func LoadDaemonConfig(path string) (*DaemonConfig, error) {
	// Load base judge configuration
	baseConfig, err := config.LoadJudgeConf("")
	if err != nil {
		return nil, fmt.Errorf("failed to load base config: %w", err)
	}

	// Default daemon-specific values
	cfg := &DaemonConfig{
		JudgeConfig:    baseConfig,
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

		assignDaemonConfigValue(cfg, key, value)
	}

	return cfg, scanner.Err()
}

func assignDaemonConfigValue(cfg *DaemonConfig, key, value string) {
	switch key {
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
	case "OJ_HTTP_API_PATH":
		cfg.HTTPAPIPath = value
	case "OJ_HTTP_LOGIN_PATH":
		cfg.HTTPLoginPath = value
	case "OJ_HTTP_USERNAME":
		cfg.HTTPUsername = value
	case "OJ_HTTP_PASSWORD":
		cfg.HTTPPassword = value
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
	case "OJ_UDP_ENABLE":
		cfg.UDPEnable, _ = strconv.ParseBool(value)
	case "OJ_UDP_SERVER":
		cfg.UDPServer = value
	case "OJ_UDP_PORT":
		cfg.UDPPort, _ = strconv.Atoi(value)
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
