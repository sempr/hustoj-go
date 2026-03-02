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

// JudgeConfig holds the main judge configuration
type JudgeConfig struct {
	Database DatabaseConfig
	OJHome   string
	Debug    bool
	Once     bool
}

// LoadJudgeConf loads configuration from judge.conf file
func LoadJudgeConf(homePath string) (*JudgeConfig, error) {
	config := &JudgeConfig{
		OJHome: homePath,
		Database: DatabaseConfig{
			Host:     "127.0.0.1",
			Port:     3306,
			User:     "root",
			Password: "password",
			Name:     "hustoj",
		},
	}

	confPath := fmt.Sprintf("%s/etc/judge.conf", homePath)
	file, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

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
		case "OJ_PORT_NUMBER":
			if port, err := strconv.Atoi(value); err == nil {
				config.Database.Port = port
			}
		case "OJ_USER_NAME":
			config.Database.User = value
		case "OJ_PASSWORD":
			config.Database.Password = value
		case "OJ_DB_NAME":
			config.Database.Name = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	return config, nil
}
