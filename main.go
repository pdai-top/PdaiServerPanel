package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/auth"
	"github.com/pdai/pdai/internal/caddy"
	"github.com/pdai/pdai/internal/config"
	"github.com/pdai/pdai/internal/database"
	"github.com/pdai/pdai/internal/execx"
	"github.com/pdai/pdai/internal/handler"
	"github.com/pdai/pdai/internal/model"
	"github.com/pdai/pdai/internal/plugin"
	"github.com/pdai/pdai/internal/service"
	cronjobplugin "github.com/pdai/pdai/plugins/cronjob"
	dbplugin "github.com/pdai/pdai/plugins/database"
	dockerplugin "github.com/pdai/pdai/plugins/docker"
	fmplugin "github.com/pdai/pdai/plugins/filemanager"
	firewallplugin "github.com/pdai/pdai/plugins/firewall"
	monitoringplugin "github.com/pdai/pdai/plugins/monitoring"
	supervisorplugin "github.com/pdai/pdai/plugins/supervisor"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z"
var Version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--reset-password", "-reset-password", "reset-password":
			resetPassword()
			return
		case "--version", "-v":
			fmt.Printf("Pdai v%s\n", Version)
			return
		}
	}

	cfg := config.Load()
	setupAppLogging(cfg)
	db := database.Init(cfg.DBPath)
	runStartupScript(db)
	caddyMgr := caddy.NewManager(cfg)
	hostSvc := service.NewHostService(db, caddyMgr, cfg)

	if err := caddyMgr.EnsureBinary(); err != nil {
		log.Printf("[ERROR] Failed to ensure Caddy binary: %v", err)
	}
	if err := caddyMgr.EnsureCaddyfile(); err != nil {
		log.Printf("[ERROR] Failed to ensure Caddyfile: %v", err)
	}
	if err := hostSvc.ApplyConfig(); err != nil {
		log.Printf("[ERROR] Failed to apply initial config: %v", err)
	}

	if !caddyMgr.IsRunning() {
		log.Println("[INFO] Caddy not running, auto-starting...")
		if err := caddyMgr.Start(); err != nil {
			log.Printf("[ERROR] Failed to auto-start Caddy: %v", err)
		}
	}

	r := gin.Default()

	corsOrigins := os.Getenv("PDAI_CORS_ORIGINS")
	r.Use(cors.New(cors.Config{
		AllowOriginWithContextFunc: func(c *gin.Context, origin string) bool {
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			hostname := u.Hostname()
			if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
				return true
			}
			if u.Host == c.Request.Host {
				reqScheme := "http"
				if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
					reqScheme = "https"
				}
				if u.Scheme == reqScheme {
					return true
				}
			}
			if corsOrigins != "" {
				for _, allowed := range strings.Split(corsOrigins, ",") {
					if strings.TrimSpace(allowed) == origin {
						return true
					}
				}
			}
			return false
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length", "Content-Disposition"},
		AllowCredentials: false,
	}))

	api := r.Group("/api")

	authH := handler.NewAuthHandler(db, cfg)
	api.GET("/auth/altcha-challenge", authH.AltchaChallenge)
	api.POST("/auth/login", authH.Login)
	api.POST("/auth/setup", authH.Setup)
	api.GET("/auth/need-setup", authH.NeedSetup)

	protected := api.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret, auth.WithDB(db)))

	adminOnly := api.Group("")
	adminOnly.Use(auth.Middleware(cfg.JWTSecret, auth.WithDB(db)))
	adminOnly.Use(auth.RequireAdmin(db))

	protected.GET("/auth/me", authH.Me)
	adminOnly.PUT("/auth/profile", authH.UpdateAdminProfile)

	// Dashboard

	dashH := handler.NewDashboardHandler(hostSvc, caddyMgr, Version)
	protected.GET("/dashboard/stats", dashH.Stats)
	protected.GET("/news", dashH.News)

	hostH := handler.NewHostHandler(hostSvc, db)
	protected.GET("/hosts", hostH.List)
	adminOnly.POST("/hosts", hostH.Create)
	protected.GET("/hosts/:id", hostH.Get)
	adminOnly.PUT("/hosts/:id", hostH.Update)
	adminOnly.DELETE("/hosts/:id", hostH.Delete)
	adminOnly.POST("/hosts/:id/clone", hostH.Clone)

	certH := handler.NewCertHandler(hostSvc, cfg)
	adminOnly.POST("/hosts/:id/cert", certH.Upload)
	adminOnly.DELETE("/hosts/:id/cert", certH.Delete)

	caddyH := handler.NewCaddyHandler(caddyMgr, db)
	protected.GET("/caddy/status", caddyH.Status)
	adminOnly.POST("/caddy/start", caddyH.Start)
	adminOnly.POST("/caddy/stop", caddyH.Stop)
	adminOnly.POST("/caddy/reload", caddyH.Reload)
	adminOnly.POST("/caddy/restart", caddyH.Restart)
	adminOnly.GET("/caddy/caddyfile", caddyH.GetCaddyfile)
	adminOnly.POST("/caddy/caddyfile", caddyH.SaveCaddyfile)
	adminOnly.POST("/caddy/fmt", caddyH.Format)
	adminOnly.POST("/caddy/validate", caddyH.Validate)

	logH := handler.NewLogHandler(cfg)
	protected.GET("/logs", logH.GetLogs)
	protected.GET("/logs/files", logH.ListLogFiles)
	protected.GET("/logs/download", logH.Download)
	protected.GET("/logs/system", logH.GetSystemLog)

	exportH := handler.NewExportHandler(hostSvc)
	adminOnly.GET("/config/export", exportH.Export)
	adminOnly.POST("/config/import", exportH.Import)

	settingH := handler.NewSettingHandler(db)
	adminOnly.GET("/settings/all", settingH.GetAll)
	adminOnly.PUT("/settings", settingH.Update)

	dnsH := handler.NewDnsProviderHandler(db)
	protected.GET("/dns-providers", dnsH.List)
	protected.GET("/dns-providers/:id", dnsH.Get)
	adminOnly.POST("/dns-providers", dnsH.Create)
	adminOnly.PUT("/dns-providers/:id", dnsH.Update)
	adminOnly.DELETE("/dns-providers/:id", dnsH.Delete)

	dnsCheckSvc := service.NewDnsCheckService(db)
	dnsCheckH := handler.NewDnsCheckHandler(dnsCheckSvc, db)
	protected.GET("/dns-check", dnsCheckH.Check)

	pluginRouter := protected.Group("/plugins")
	adminPluginRouter := adminOnly.Group("/plugins")
	if err := initPlugins(db, pluginRouter, adminPluginRouter, hostSvc, caddyMgr, cfg); err != nil {
		log.Fatalf("Failed to initialize plugins: %v", err)
	}

	setupFrontend(r)

	addr := ":" + cfg.Port
	log.Printf("[INFO] Pdai starting on http://localhost%s", addr)
	log.Printf("[INFO] Data directory: %s", cfg.DataDir)
	log.Printf("[INFO] Caddyfile path: %s", cfg.CaddyfilePath)
	if _, err := os.Stat("config.ini"); os.IsNotExist(err) {
		log.Printf("[INFO] No local config.ini found; a default one was generated in the current directory.")
	}

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func initPlugins(db *gorm.DB, protectedRouter *gin.RouterGroup, adminRouter *gin.RouterGroup, hostSvc *service.HostService, caddyMgr *caddy.Manager, cfg *config.Config) error {
	coreAPI := plugin.NewCoreAPI(db, hostSvc, caddyMgr, cfg.DataDir, cfg.JWTSecret)
	pluginMgr := plugin.NewManager(db, protectedRouter, adminRouter, adminRouter, nil, coreAPI, cfg.DataDir)
	coreAPI.SetEventBus(pluginMgr.EventBus())

	if err := pluginMgr.Register(dockerplugin.New()); err != nil {
		return fmt.Errorf("register docker plugin: %w", err)
	}
	if err := pluginMgr.Register(fmplugin.New()); err != nil {
		return fmt.Errorf("register filemanager plugin: %w", err)
	}
	if err := pluginMgr.Register(dbplugin.New()); err != nil {
		return fmt.Errorf("register database plugin: %w", err)
	}
	if err := pluginMgr.Register(monitoringplugin.New()); err != nil {
		return fmt.Errorf("register monitoring plugin: %w", err)
	}
	if err := pluginMgr.Register(firewallplugin.New()); err != nil {
		return fmt.Errorf("register firewall plugin: %w", err)
	}
	if err := pluginMgr.Register(cronjobplugin.New()); err != nil {
		return fmt.Errorf("register cronjob plugin: %w", err)
	}
	if err := pluginMgr.Register(supervisorplugin.New()); err != nil {
		return fmt.Errorf("register supervisor plugin: %w", err)
	}

	if err := pluginMgr.InitAll(); err != nil {
		return fmt.Errorf("plugin init failed: %w", err)
	}

	pluginH := handler.NewPluginHandler(pluginMgr)
	protectedRouter.GET("", pluginH.List)
	protectedRouter.GET("/frontend-manifests", pluginH.FrontendManifests)
	adminRouter.POST("/:id/enable", pluginH.Enable)
	adminRouter.POST("/:id/disable", pluginH.Disable)
	adminRouter.POST("/:id/sidebar", pluginH.SetSidebarVisibility)
	adminRouter.POST("/:id/install", pluginH.Install)

	if err := pluginMgr.StartAll(); err != nil {
		return fmt.Errorf("plugin start failed: %w", err)
	}

	return nil
}

func setupAppLogging(cfg *config.Config) {
	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		log.Printf("[ERROR] Failed to create log dir: %v", err)
		return
	}

	logFile, err := os.OpenFile(filepath.Join(cfg.LogDir, "Pdai.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[ERROR] Failed to open application log file: %v", err)
		return
	}

	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	gin.DefaultWriter = io.MultiWriter(os.Stdout, logFile)
	gin.DefaultErrorWriter = io.MultiWriter(os.Stderr, logFile)
	log.Printf("[INFO] Application logging initialized at %s", filepath.Join(cfg.LogDir, "Pdai.log"))
}

func setupFrontend(r *gin.Engine) {
	distPath := "web/dist"

	if info, err := os.Stat(distPath); err == nil && info.IsDir() {
		r.Static("/assets", filepath.Join(distPath, "assets"))
		r.StaticFile("/favicon.ico", filepath.Join(distPath, "favicon.ico"))
		r.NoRoute(func(c *gin.Context) {
			serveDiskSPA(c, distPath)
		})
		log.Println("[INFO] Serving frontend from local web/dist")
		return
	}

	embeddedRoot, err := fs.Sub(embeddedFrontend, "web/dist")
	if err != nil {
		log.Printf("[ERROR] Embedded frontend unavailable: %v", err)
		return
	}

	fileServer := http.FileServer(http.FS(embeddedRoot))
	r.GET("/assets/*filepath", gin.WrapH(http.StripPrefix("/assets/", http.FileServer(http.FS(mustSubFS(embeddedRoot, "assets"))))))
	r.GET("/favicon.ico", func(c *gin.Context) {
		serveEmbeddedFile(c, embeddedRoot, "favicon.ico")
	})
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}
		cleaned := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/")
		if cleaned != "" && embeddedFileExists(embeddedRoot, cleaned) {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		serveEmbeddedFile(c, embeddedRoot, "index.html")
	})

	log.Println("[INFO] Serving frontend from embedded web/dist")
}

func serveDiskSPA(c *gin.Context, distPath string) {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api") {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
		return
	}

	cleaned := filepath.Clean(path)
	filePath := filepath.Join(distPath, cleaned)
	absDistPath, _ := filepath.Abs(distPath)
	absFilePath, _ := filepath.Abs(filePath)
	if !strings.HasPrefix(absFilePath, absDistPath+string(filepath.Separator)) && absFilePath != absDistPath {
		c.File(filepath.Join(distPath, "index.html"))
		return
	}

	if _, err := os.Stat(filePath); err == nil {
		c.File(filePath)
		return
	}

	c.File(filepath.Join(distPath, "index.html"))
}

func serveEmbeddedFile(c *gin.Context, root fs.FS, name string) {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(data))
}

func embeddedFileExists(root fs.FS, name string) bool {
	file, err := root.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && !info.IsDir()
}

func mustSubFS(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func runStartupScript(db *gorm.DB) {
	var startupScript model.Setting
	if err := db.Where("key = ?", "startup_script").First(&startupScript).Error; err != nil {
		return
	}
	script := strings.TrimSpace(startupScript.Value)
	if script == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := execx.BashContext(ctx, script)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		log.Printf("[INFO] Startup script output:\n%s", strings.TrimSpace(string(output)))
	}
	if err != nil {
		log.Printf("[ERROR] Startup script failed: %v", err)
	}
}

func resetPassword() {
	fmt.Println("Pdai password reset tool")
	fmt.Println("========================")

	cfg := config.Load()
	db := database.Init(cfg.DBPath)
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter username (default admin): ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}

	fmt.Print("Enter new password (at least 8 characters): ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		fmt.Println("[ERROR] Password must be at least 8 characters")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("[ERROR] Failed to hash password: %v\n", err)
		os.Exit(1)
	}

	var user model.User
	result := db.Where("username = ?", username).First(&user)
	if result.Error != nil {
		user = model.User{
			Username: username,
			Password: string(hash),
			Role:     "admin",
		}
		if err := db.Create(&user).Error; err != nil {
			fmt.Printf("[ERROR] Failed to create user: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[OK] Created administrator account: %s\n", username)
	} else {
		user.Password = string(hash)
		if err := db.Save(&user).Error; err != nil {
			fmt.Printf("[ERROR] Failed to update password: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[OK] Reset password for user %s\n", username)
	}

	fmt.Println("\nRestart Pdai to use the new password:")
	fmt.Println("  systemctl restart pdai    # systemd")
	fmt.Println("  rc-service pdai restart   # OpenRC/Alpine")
}

func panelAutoStartEnabled(db *gorm.DB) bool {
	var setting model.Setting
	if err := db.Where("key = ?", "panel_autostart").First(&setting).Error; err != nil {
		return true
	}
	return setting.Value != "false"
}
