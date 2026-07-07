package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultWebAddr    = ":18080"
	DefaultAPIAddr    = ":18081"
	DefaultMCPAddr    = ":18082"
	DefaultRedisAddr  = "localhost:6379"
	DefaultQdrantURL  = "http://localhost:18083"
	DefaultArchiveDir = "/data/memory-os"
)

// Config 保存 Memory OS 各进程共享的基础配置。
type Config struct {
	AppEnv                 string
	EnableDevEndpoints     bool
	LegacyTurnEventArchive bool
	WebAddr                string
	APIAddr                string
	MCPAddr                string
	PostgresDSN            string
	RedisAddr              string
	QdrantURL              string
	ArchiveDir             string
	LLMBaseURL             string
	LLMAPIKey              string
	LLMModel               string
	EmbeddingModel         string
	RerankModel            string
	RerankMinScore         float64
}

// Load 从环境变量读取配置，并为本地开发提供安全默认值。
func Load() (Config, error) {
	cfg := Config{
		AppEnv:                 envOrDefault("APP_ENV", "production"),
		EnableDevEndpoints:     envBool("ENABLE_DEV_ENDPOINTS"),
		LegacyTurnEventArchive: envBool("LEGACY_TURN_EVENT_ARCHIVE"),
		WebAddr:                envOrDefault("MEMORY_WEB_ADDR", DefaultWebAddr),
		APIAddr:                envOrDefault("MEMORY_API_ADDR", DefaultAPIAddr),
		MCPAddr:                envOrDefault("MEMORY_MCP_ADDR", DefaultMCPAddr),
		PostgresDSN:            envOrDefault("POSTGRES_DSN", ""),
		RedisAddr:              envOrDefault("REDIS_ADDR", DefaultRedisAddr),
		QdrantURL:              envOrDefault("QDRANT_URL", DefaultQdrantURL),
		ArchiveDir:             envOrDefault("ARCHIVE_DIR", DefaultArchiveDir),
		LLMBaseURL:             envOrDefault("LLM_BASE_URL", ""),
		LLMAPIKey:              envOrDefault("LLM_API_KEY", ""),
		LLMModel:               envOrDefault("LLM_MODEL", "MiniMax-M2.7"),
		EmbeddingModel:         envOrDefault("EMBEDDING_MODEL", "bge-m3"),
		RerankModel:            envOrDefault("RERANK_MODEL", "bge-reranker-v2-m3"),
		RerankMinScore:         envFloatOrDefault("RERANK_MIN_SCORE", 0.2),
	}

	for name, addr := range map[string]string{
		"MEMORY_WEB_ADDR": cfg.WebAddr,
		"MEMORY_API_ADDR": cfg.APIAddr,
		"MEMORY_MCP_ADDR": cfg.MCPAddr,
	} {
		if err := validateAddr(addr); err != nil {
			return Config{}, fmt.Errorf("%s invalid: %w", name, err)
		}
	}

	if cfg.QdrantURL != "" {
		if _, err := url.ParseRequestURI(cfg.QdrantURL); err != nil {
			return Config{}, fmt.Errorf("QDRANT_URL invalid: %w", err)
		}
	}

	return cfg, nil
}

// RedactDSN 隐去 DSN 中的密码，避免日志泄露 secret。
func RedactDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil {
		return redactURLPasswordPattern(dsn)
	}

	username := parsed.User.Username()
	if _, hasPassword := parsed.User.Password(); !hasPassword {
		return dsn
	}

	parsed.User = url.UserPassword(username, "xxxxx")
	return parsed.String()
}

var urlPasswordPattern = regexp.MustCompile(`([^:/?#]+://[^:/?#]+:)([^@]+)(@)`)

func redactURLPasswordPattern(value string) string {
	return urlPasswordPattern.ReplaceAllString(value, `${1}xxxxx${3}`)
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envFloatOrDefault(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func validateAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}

	parsedPort, err := strconv.Atoi(port)
	if err != nil {
		return err
	}
	if parsedPort < 1 || parsedPort > 65535 {
		return fmt.Errorf("port out of range: %d", parsedPort)
	}

	return nil
}
