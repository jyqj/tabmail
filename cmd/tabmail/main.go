package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/autocreate"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/retention"
	"tabmail/internal/settings"
	smtpsrv "tabmail/internal/smtp"
	"tabmail/internal/store"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/store/postgres"
	"tabmail/internal/store/s3obj"
)

var version = "dev"

const publicTenantID = "00000000-0000-0000-0000-000000000001"

func main() {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Str("version", version).Logger()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("loading config")
	}
	setLogLevel(&logger, cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- PostgreSQL ---
	pg, err := postgres.New(ctx, cfg.DB)
	if err != nil {
		logger.Fatal().Err(err).Msg("connecting to postgres")
	}
	defer pg.Close()
	logger.Info().Msg("connected to PostgreSQL and initialized schema")

	// --- Redis ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatal().Err(err).Msg("connecting to redis")
	}
	defer rdb.Close()
	logger.Info().Msg("connected to Redis")

	// --- Object store ---
	var obj store.ObjectStore
	switch strings.ToLower(strings.TrimSpace(cfg.ObjectStore)) {
	case "", "fs":
		obj, err = fileobj.New(cfg.DataDir)
		if err != nil {
			logger.Fatal().Err(err).Msg("initializing filesystem object store")
		}
		logger.Info().Str("backend", "fs").Str("data_dir", cfg.DataDir).Msg("object store initialized")
	case "s3":
		obj, err = s3obj.New(cfg.S3)
		if err != nil {
			logger.Fatal().Err(err).Msg("initializing s3 object store")
		}
		logger.Info().Str("backend", "s3").Str("endpoint", cfg.S3.Endpoint).Str("bucket", cfg.S3.Bucket).Msg("object store initialized")
	default:
		logger.Fatal().Str("object_store", cfg.ObjectStore).Msg("invalid object store backend")
	}

	// --- Bootstrap admin user ---
	bootstrapAdmin(ctx, pg, cfg, logger)

	// --- System settings (DB-backed, env-var seeded) ---
	settingsMgr := settings.NewManager(pg, logger)
	settingsMgr.Seed(ctx, map[string]settings.SeedValue{
		models.SettingAutoCreateRouteRPM:  {Value: strconv.Itoa(cfg.AutoCreateRouteRPM), Description: "Per-route auto-create mailbox RPM (0=disable)"},
		models.SettingAutoCreateTenantRPM: {Value: strconv.Itoa(cfg.AutoCreateTenantRPM), Description: "Per-tenant auto-create mailbox RPM (0=disable)"},
		models.SettingMailboxNaming:       {Value: cfg.MailboxNaming, Description: "Mailbox naming mode: full, local, or domain"},
		models.SettingStripPlusTag:        {Value: strconv.FormatBool(cfg.StripPlusTag), Description: "Strip +tag from local part of addresses"},
		models.SettingMonitorHistory:      {Value: strconv.Itoa(cfg.MonitorHistory), Description: "Number of recent events to keep for monitor replay (0=disable)"},
		models.SettingFallbackRetentionH:  {Value: strconv.Itoa(cfg.Storage.FallbackRetentionH), Description: "System-level fallback retention hours"},
		models.SettingOpenRegistration:    {Value: strconv.FormatBool(cfg.OpenRegistration), Description: "Allow public user registration"},
		models.SettingPublicIPRPM:         {Value: strconv.Itoa(cfg.HTTP.PublicIPRPM), Description: "Per-IP RPM for unauthenticated requests (0=disable)"},
	})

	// --- Domain resolver ---
	namingMode, err := policy.ParseNamingMode(cfg.MailboxNaming)
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid mailbox naming mode")
	}
	autoCreateLimiter := autocreate.NewLimiter(rdb, cfg.AutoCreateRouteRPM, cfg.AutoCreateTenantRPM)
	res := resolver.New(pg, namingMode, cfg.StripPlusTag, autoCreateLimiter)
	hub := realtime.NewHub(cfg.MonitorHistory, pg)
	dispatcher := hooks.New(hooks.Config{
		URLs:         cfg.Webhook.URLs,
		Secret:       cfg.Webhook.Secret,
		Timeout:      cfg.Webhook.Timeout,
		MaxRetries:   cfg.Webhook.MaxRetries,
		RetryDelay:   cfg.Webhook.RetryDelay,
		DeadLimit:    cfg.Webhook.DeadLimit,
		PollInterval: cfg.Webhook.PollInterval,
		BatchSize:    cfg.Webhook.BatchSize,
	}, logger).BindStore(pg)
	defaultPolicy := models.SMTPPolicy{
		DefaultAccept:       cfg.SMTP.DefaultAccept,
		AcceptDomains:       append([]string(nil), cfg.SMTP.AcceptDomains...),
		RejectDomains:       append([]string(nil), cfg.SMTP.RejectDomains...),
		DefaultStore:        cfg.SMTP.DefaultStore,
		StoreDomains:        append([]string(nil), cfg.SMTP.StoreDomains...),
		DiscardDomains:      append([]string(nil), cfg.SMTP.DiscardDomains...),
		RejectOriginDomains: append([]string(nil), cfg.SMTP.RejectOriginDomains...),
	}
	// Seed SMTP policy to DB on first start (if no DB policy exists yet).
	if existing, err := pg.GetSMTPPolicy(ctx); err == nil && existing == nil {
		if err := pg.UpsertSMTPPolicy(ctx, &defaultPolicy); err != nil {
			logger.Warn().Err(err).Msg("seed SMTP policy to DB")
		} else {
			logger.Info().Msg("seeded SMTP policy from env vars to DB")
		}
	}

	ingestSvc := ingest.NewService(pg, obj, res, hub, dispatcher, defaultPolicy, cfg.Storage.FallbackRetentionH, rdb, cfg.Ingest, logger)

	// --- Outbound service ---
	var outboundSvc *outbound.Service
	if cfg.Outbound.Enabled {
		outboundSvc = outbound.NewService(cfg.Outbound, pg, logger)
	}

	defaultPlanID, _ := uuid.Parse(cfg.DefaultPlanID)

	routerCfg := api.RouterConfig{
		Store:              pg,
		ObjectStore:        obj,
		Hub:                hub,
		Dispatcher:         dispatcher,
		NamingMode:         namingMode,
		StripPlus:          cfg.StripPlusTag,
		DefaultPolicy:      defaultPolicy,
		JWTSecret:          cfg.EffectiveJWTSecret(),
		MailboxTokenSecret: cfg.MailboxTokenSecret,
		ExpectedMXHost:     cfg.SMTP.Domain,
		PublicTenantID:     publicTenantID,
		DefaultPlanID:      defaultPlanID,
		OpenRegistration:   cfg.OpenRegistration,
		Settings:           settingsMgr,
		HTTP:               cfg.HTTP,
		OutboundService:    outboundSvc,
		Logger:             logger,
	}

	// --- Retention scanner ---
	ret := retention.New(pg, obj, cfg.Storage, logger)
	var smtpSrv *smtpsrv.Server
	var httpSrv *http.Server

	switch strings.ToLower(strings.TrimSpace(cfg.Role)) {
	case "", "all":
		go ret.Run(ctx)
		go dispatcher.Run(ctx)
		go ingestSvc.Run(ctx)
		if outboundSvc != nil {
			outboundSvc.StartWorker(ctx)
		}

		smtpSrv = smtpsrv.NewServer(cfg.SMTP, ingestSvc, res, logger)
		go func() {
			if err := smtpSrv.Start(ctx); err != nil {
				logger.Error().Err(err).Msg("SMTP server error")
				cancel()
			}
		}()

		rl := middleware.NewRateLimiter(rdb, pg, cfg.HTTP.PublicIPRPM, cfg.HTTP.TrustedProxies)
		routerCfg.RateLimiter = rl
		handler := api.NewRouter(routerCfg)
		httpSrv = newHTTPServer(cfg.HTTP.Addr, handler)
		go serveHTTP(ctx, cancel, httpSrv, logger)
	case "api":
		go dispatcher.Run(ctx)
		go ingestSvc.Run(ctx)
		rl := middleware.NewRateLimiter(rdb, pg, cfg.HTTP.PublicIPRPM, cfg.HTTP.TrustedProxies)
		routerCfg.RateLimiter = rl
		handler := api.NewRouter(routerCfg)
		httpSrv = newHTTPServer(cfg.HTTP.Addr, handler)
		go serveHTTP(ctx, cancel, httpSrv, logger)
	case "smtp":
		smtpSrv = smtpsrv.NewServer(cfg.SMTP, ingestSvc, res, logger)
		go func() {
			if err := smtpSrv.Start(ctx); err != nil {
				logger.Error().Err(err).Msg("SMTP server error")
				cancel()
			}
		}()
	case "worker":
		go dispatcher.Run(ctx)
		go ingestSvc.Run(ctx)
		if outboundSvc != nil {
			outboundSvc.StartWorker(ctx)
		}
	case "retention":
		go ret.Run(ctx)
	default:
		logger.Fatal().Str("role", cfg.Role).Msg("invalid role; expected all, api, smtp, worker, retention")
	}

	logger.Info().Str("role", strings.ToLower(strings.TrimSpace(cfg.Role))).Msg("TabMail is running")

	<-ctx.Done()
	logger.Info().Msg("shutting down...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()

	if httpSrv != nil {
		_ = httpSrv.Shutdown(shutCtx)
	}
	if smtpSrv != nil {
		_ = smtpSrv.Shutdown(shutCtx)
	}
	if outboundSvc != nil {
		outboundSvc.Shutdown()
	}
	logger.Info().Msg("shutdown complete")
}

// bootstrapAdmin creates the bootstrap admin user if they don't already exist.
// Skips only when the bootstrap email is already registered, so existing
// non-admin users no longer block admin creation.
func bootstrapAdmin(ctx context.Context, st store.Store, cfg *config.Root, logger zerolog.Logger) {
	if cfg.BootstrapAdminEmail == "" || cfg.BootstrapAdminPass == "" {
		return
	}

	// Check if the bootstrap admin already exists
	existing, err := st.GetUserByEmail(ctx, cfg.BootstrapAdminEmail)
	if err != nil {
		logger.Warn().Err(err).Msg("bootstrap: failed to check existing users")
		return
	}
	if existing != nil {
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.BootstrapAdminPass), bcrypt.DefaultCost)
	if err != nil {
		logger.Fatal().Err(err).Msg("bootstrap: hash password")
	}

	// Use the "pro" plan for admin tenant
	proPlanID, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
	tenant := &models.Tenant{
		Name:    cfg.BootstrapAdminEmail,
		PlanID:  proPlanID,
		IsSuper: true,
	}
	if err := st.CreateTenant(ctx, tenant); err != nil {
		logger.Fatal().Err(err).Msg("bootstrap: create admin tenant")
	}

	email := strings.ToLower(strings.TrimSpace(cfg.BootstrapAdminEmail))
	user := &models.User{
		TenantID:     tenant.ID,
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  "Admin",
		Role:         models.RolePlatformAdmin,
		IsActive:     true,
	}
	if err := st.CreateUser(ctx, user); err != nil {
		logger.Fatal().Err(err).Msg("bootstrap: create admin user")
	}
	logger.Info().Str("email", email).Msg("bootstrap: admin user created")
}

func setLogLevel(l *zerolog.Logger, level string) {
	switch level {
	case "debug":
		*l = l.Level(zerolog.DebugLevel)
	case "warn":
		*l = l.Level(zerolog.WarnLevel)
	case "error":
		*l = l.Level(zerolog.ErrorLevel)
	default:
		*l = l.Level(zerolog.InfoLevel)
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func serveHTTP(_ context.Context, cancel context.CancelFunc, srv *http.Server, logger zerolog.Logger) {
	logger.Info().Str("addr", srv.Addr).Msg("HTTP API starting")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error().Err(err).Msg("HTTP server error")
		cancel()
	}
}
