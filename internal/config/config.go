package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	DataDir          string
	BaseURL          string
	AccessPassword   string
	GuestEnabled     bool
	WikimediaEnabled bool
	LogLevel         slog.Level
}

func Load() (Config, error) {
	c := Config{Port: value("CURIO_PORT", "8080"), DataDir: value("CURIO_DATA_DIR", "/data"), BaseURL: strings.TrimRight(os.Getenv("CURIO_BASE_URL"), "/")}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("CURIO_PORT must be between 1 and 65535")
	}
	c.AccessPassword = os.Getenv("CURIO_ACCESS_PASSWORD")
	if len(c.AccessPassword) < 12 || len(c.AccessPassword) > 128 {
		return c, fmt.Errorf("CURIO_ACCESS_PASSWORD must contain 12–128 bytes; set it in Compose or the environment")
	}
	if c.GuestEnabled, err = boolean("CURIO_GUEST_ENABLED", true); err != nil {
		return c, err
	}
	if c.WikimediaEnabled, err = boolean("CURIO_WIKIMEDIA_ENABLED", true); err != nil {
		return c, err
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("CURIO_BASE_URL must be an http(s) origin without a path")
		}
	}
	if err := c.LogLevel.UnmarshalText([]byte(value("CURIO_LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("CURIO_LOG_LEVEL must be debug, info, warn, or error")
	}
	return c, nil
}
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.BaseURL, "https://") }
func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func boolean(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, e := strconv.ParseBool(v)
	if e != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return b, nil
}
