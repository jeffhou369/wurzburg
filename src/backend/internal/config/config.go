package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 封裝應用程式的所有環境變數設定。
type Config struct {
	Port          int
	DBHost        string
	DBPort        int
	DBUser        string
	DBPassword    string
	DBName        string
	AdminUsername string
	AdminPassword string
	SecretKey     string
	CampusName    string
	Timezone      string
	AppEnv        string
}

// LoadFromEnv 從環境變數載入設定，並套用合理之預設值。
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		Port:          getEnvInt("WURZBURG_PORT", getEnvInt("PORT", 8080)),
		DBHost:        getEnv("WURZBURG_DB_HOST", "db"),
		DBPort:        getEnvInt("WURZBURG_DB_PORT", 3306),
		DBUser:        getEnv("WURZBURG_DB_USER", "wurzburg"),
		DBPassword:    getEnv("WURZBURG_DB_PASSWORD", ""),
		DBName:        getEnv("WURZBURG_DB_NAME", "wurzburg"),
		AdminUsername: getEnv("WURZBURG_ADMIN_USERNAME", ""),
		AdminPassword: getEnv("WURZBURG_ADMIN_PASSWORD", ""),
		SecretKey:     getEnv("WURZBURG_SECRET_KEY", ""),
		CampusName:    getEnv("WURZBURG_CAMPUS_NAME", "Würzburg"),
		Timezone:      getEnv("WURZBURG_TIMEZONE", "Asia/Taipei"),
		AppEnv:        getEnv("WURZBURG_ENV", "production"),
	}

	return cfg, nil
}

// DSN 產出適用於 MariaDB/MySQL 連線的 Data Source Name。
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
		c.DBUser,
		c.DBPassword,
		c.DBHost,
		c.DBPort,
		c.DBName,
	)
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
