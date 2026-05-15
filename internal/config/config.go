package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all application configuration
type Config struct {
	Port          string // Panel HTTP port
	DBPath        string // SQLite database path
	JWTSecret     string // JWT signing secret
	CaddyBin      string // Path to caddy binary
	CaddyfilePath string // Path to generated Caddyfile
	LogDir        string // Directory for Caddy logs
	DataDir       string // Data directory root
	AdminAPI      string // Caddy admin API URL
}

// Load reads configuration from PDAI_CONFIG, ./config.ini and environment
// variables with sensible defaults. On first single-binary startup it creates
// ./config.ini so users have an explicit local file to review and edit.
func Load() *Config {
	loadConfigFile()
	dataDir := envOrDefault("PDAI_DATA_DIR", "./data")

	// Ensure directories exist early so we can write the secret file
	os.MkdirAll(dataDir, 0755)

	cfg := &Config{
		Port:          envOrDefault("PDAI_PORT", "39921"),
		DBPath:        envOrDefault("PDAI_DB_PATH", filepath.Join(dataDir, "Pdai.db")),
		JWTSecret:     resolveJWTSecret(dataDir),
		CaddyBin:      envOrDefault("PDAI_CADDY_BIN", "./data/caddy/caddy"),
		CaddyfilePath: envOrDefault("PDAI_CADDYFILE_PATH", "./data/caddy/Caddyfile"),
		LogDir:        envOrDefault("PDAI_LOG_DIR", filepath.Join(dataDir, "logs")),
		DataDir:       dataDir,
		AdminAPI:      envOrDefault("PDAI_ADMIN_API", "http://localhost:2019"),
	}

	// Ensure directories exist
	os.MkdirAll(cfg.LogDir, 0755)
	os.MkdirAll(filepath.Join(dataDir, "backups"), 0755)

	return cfg
}

// resolveJWTSecret determines the JWT secret using this priority:
//  1. PDAI_JWT_SECRET env var (if set and not an insecure default)
//  2. Persisted secret in data/.jwt_secret
//  3. Auto-generate a new cryptographic random secret and persist it
func resolveJWTSecret(dataDir string) string {
	// Known insecure defaults that must be rejected.
	insecureDefaults := map[string]bool{
		"pdai-change-me-in-production": true,
		"change-me-in-production":      true,
	}

	// 1. Explicit env var takes precedence (if not an insecure default)
	if envSecret := os.Getenv("PDAI_JWT_SECRET"); envSecret != "" && !insecureDefaults[envSecret] {
		return envSecret
	}

	// 2. Try to load persisted secret
	secretFile := filepath.Join(dataDir, ".jwt_secret")
	if data, err := os.ReadFile(secretFile); err == nil {
		secret := strings.TrimSpace(string(data))
		if secret != "" {
			return secret
		}
	}

	// 3. Generate a cryptographically random secret and persist it
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		log.Fatalf("FATAL: failed to generate JWT secret: %v", err)
	}
	secret := hex.EncodeToString(secretBytes)

	if err := os.WriteFile(secretFile, []byte(secret+"\n"), 0600); err != nil {
		log.Printf("⚠️  Could not persist JWT secret to %s: %v", secretFile, err)
		log.Printf("   Set PDAI_JWT_SECRET env var to ensure stable sessions across restarts.")
	} else {
		log.Printf("🔑 Generated new JWT secret and saved to %s", secretFile)
	}

	return secret
}

func loadConfigFile() {
	configPath := os.Getenv("PDAI_CONFIG")
	if configPath == "" {
		configPath = "config.ini"
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) && os.Getenv("PDAI_CONFIG") == "" {
		if err := writeDefaultConfig(configPath); err != nil {
			log.Printf("⚠️  Could not write default config file %s: %v", configPath, err)
		} else {
			log.Printf("📝 Created default config file: %s", configPath)
			printFirstRunGuide(configPath)
		}
	}

	file, err := os.Open(configPath)
	if err != nil {
		if os.Getenv("PDAI_CONFIG") != "" {
			log.Printf("⚠️  Could not open PDAI_CONFIG=%s: %v", configPath, err)
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, value)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("⚠️  Could not fully read config file %s: %v", configPath, err)
	}
}

func writeDefaultConfig(path string) error {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("generate jwt secret: %w", err)
	}
	secret := hex.EncodeToString(secretBytes)
	content := fmt.Sprintf(`# Pdai 单文件部署配置
# 首次启动时自动生成。修改本文件后，请重启面板使配置生效。

# 面板 HTTP 监听端口
PDAI_PORT=39921

# 面板数据目录。数据库、日志、Caddy、站点文件、备份等默认都会放在此目录下
PDAI_DATA_DIR=./data

# SQLite 数据库文件路径
PDAI_DB_PATH=./data/Pdai.db

# JWT 签名密钥。请勿泄露；修改后现有登录会话会失效
PDAI_JWT_SECRET=%s

# Caddy 可执行文件路径。若文件不存在，面板会自动下载对应架构的 Caddy 到此路径
PDAI_CADDY_BIN=./data/caddy/caddy

# Caddyfile 配置文件路径
PDAI_CADDYFILE_PATH=./data/caddy/Caddyfile

# 面板和 Caddy 日志目录
PDAI_LOG_DIR=./data/logs

# Caddy Admin API 地址。通常无需修改
PDAI_ADMIN_API=http://localhost:2019

# Gin 运行模式。生产环境建议保持 release
GIN_MODE=release
`, secret)
	return os.WriteFile(path, []byte(content), 0600)
}

func printFirstRunGuide(configPath string) {
	log.Printf("👉 First-run guide:")
	log.Printf("   1. Review config: %s", configPath)
	log.Printf("   2. Ensure Caddy is installed and available as PDAI_CADDY_BIN")
	log.Printf("   3. Ensure Podman/Docker socket is available for container features")
	log.Printf("   4. Open http://localhost:%s to create the admin account", envOrDefault("PDAI_PORT", "39921"))
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
