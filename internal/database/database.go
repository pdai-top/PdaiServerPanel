package database

import (
	"log"
	"os"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pdai/pdai/internal/model"
	"github.com/pdai/pdai/internal/notify"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Init initializes the SQLite database and runs auto-migration
func Init(dbPath string) *gorm.DB {
	gormLogger := logger.New(
		log.New(os.Stdout, "", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
			ParameterizedQueries:      true,
		},
	)
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Enable WAL mode for better concurrent read performance
	sqlDB, _ := db.DB()
	sqlDB.Exec("PRAGMA journal_mode=WAL")
	sqlDB.Exec("PRAGMA foreign_keys=ON")

	// Auto-migrate all models
	err = db.AutoMigrate(
		&model.User{},
		&model.Host{},
		&model.HostDomain{},
		&model.Upstream{},
		&model.Route{},
		&model.CustomHeader{},
		&model.AccessRule{},
		&model.BasicAuth{},
		&model.AuditLog{},
		&model.DnsProvider{},
		&model.Setting{},
		&model.Certificate{},
		&model.Group{},
		&model.Tag{},
		&model.HostTag{},
		&model.Template{},
		&notify.Channel{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Seed default settings
	db.Where("key = ?", "auto_reload").FirstOrCreate(&model.Setting{Key: "auto_reload", Value: "true"})
	db.Where("key = ?", "site_name").FirstOrCreate(&model.Setting{Key: "site_name", Value: ""})
	db.Where("key = ?", "server_ipv4").FirstOrCreate(&model.Setting{Key: "server_ipv4", Value: ""})
	db.Where("key = ?", "dns_verify_on_create").FirstOrCreate(&model.Setting{Key: "dns_verify_on_create", Value: "false"})
	db.Where("key = ?", "wildcard_domain").FirstOrCreate(&model.Setting{Key: "wildcard_domain", Value: ""})
	db.Where("key = ?", "wildcard_tls_mode").FirstOrCreate(&model.Setting{Key: "wildcard_tls_mode", Value: "auto"})
	db.Where("key = ?", "server_ipv6").FirstOrCreate(&model.Setting{Key: "server_ipv6", Value: ""})
	db.Where("key = ?", "panel_autostart").FirstOrCreate(&model.Setting{Key: "panel_autostart", Value: "false"})
	db.Where("key = ?", "startup_script").FirstOrCreate(&model.Setting{Key: "startup_script", Value: ""})

	// Role cleanup: older builds promoted the first administrator to "owner".
	// The panel now uses "admin" as the only full-control role.
	if tx := db.Model(&model.User{}).Where("role = ?", "owner").Update("role", "admin"); tx.Error == nil && tx.RowsAffected > 0 {
		log.Printf("RBAC migration: converted %d owner user(s) to admin", tx.RowsAffected)
	}
	if tx := db.Model(&model.User{}).Where("role <> ?", "admin").Update("role", "admin"); tx.Error == nil && tx.RowsAffected > 0 {
		log.Printf("RBAC migration: converted %d non-admin user(s) to admin", tx.RowsAffected)
	}

	log.Println("Database initialized successfully")
	return db
}

// SeedTemplatePresets seeds preset templates if the templates table is empty.
// This is called from main.go after TemplateService is initialized.
func SeedTemplatePresets(db *gorm.DB, seedFunc func()) {
	var count int64
	db.Model(&model.Template{}).Count(&count)
	if count == 0 {
		seedFunc()
	}
}
