package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
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

	// TrustedProxies 是可信任的反向代理位址。只有直接連線來源落在其中時，
	// 才採信 X-Real-IP／X-Forwarded-For 作為客戶端 IP；空白代表不信任任何代理。
	TrustedProxies []netip.Prefix
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

	proxies, err := ParseTrustedProxies(os.Getenv("WURZBURG_TRUSTED_PROXIES"))
	if err != nil {
		return nil, err
	}
	cfg.TrustedProxies = proxies

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

// ParseTrustedProxies 解析以逗號分隔的 IPv4 位址或 CIDR 清單；單一位址視為 /32。
// 任何一項無效都回傳錯誤，讓設定錯誤在啟動時就被發現，而不是默默不採信代理。
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		var p netip.Prefix
		var err error
		if strings.Contains(item, "/") {
			p, err = netip.ParsePrefix(item)
		} else {
			var a netip.Addr
			a, err = netip.ParseAddr(item)
			if err == nil {
				p = netip.PrefixFrom(a, a.BitLen())
			}
		}
		if err != nil || !p.Addr().Is4() {
			return nil, fmt.Errorf("WURZBURG_TRUSTED_PROXIES 含無效的 IPv4 位址或 CIDR：%q", item)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
