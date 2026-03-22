package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/retention"
	smtpsrv "tabmail/internal/smtp"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/store/postgres"
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
	logger.Info().Msg("connected to PostgreSQL")

	// --- Redis ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatal().Err(err).Msg("connecting to redis")
	}
	defer rdb.Close()
	logger.Info().Msg("connected to Redis")

	// --- Object store ---
	obj, err := fileobj.New(cfg.DataDir)
	if err != nil {
		logger.Fatal().Err(err).Msg("initializing file object store")
	}

	// --- Domain resolver ---
	namingMode, err := policy.ParseNamingMode(cfg.MailboxNaming)
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid mailbox naming mode")
	}
	res := resolver.New(pg, namingMode, cfg.StripPlusTag)
	hub := realtime.NewHub(cfg.MonitorHistory, pg)
	dispatcher := hooks.New(hooks.Config{
		URLs:       cfg.Webhook.URLs,
		Secret:     cfg.Webhook.Secret,
		Timeout:    cfg.Webhook.Timeout,
		MaxRetries: cfg.Webhook.MaxRetries,
		RetryDelay: cfg.Webhook.RetryDelay,
		DeadLimit:  cfg.Webhook.DeadLimit,
	}, logger)
	defaultPolicy := models.SMTPPolicy{
		DefaultAccept:       cfg.SMTP.DefaultAccept,
		AcceptDomains:       append([]string(nil), cfg.SMTP.AcceptDomains...),
		RejectDomains:       append([]string(nil), cfg.SMTP.RejectDomains...),
		DefaultStore:        cfg.SMTP.DefaultStore,
		StoreDomains:        append([]string(nil), cfg.SMTP.StoreDomains...),
		DiscardDomains:      append([]string(nil), cfg.SMTP.DiscardDomains...),
		RejectOriginDomains: append([]string(nil), cfg.SMTP.RejectOriginDomains...),
	}

	// --- Retention scanner ---
	ret := retention.New(pg, obj, cfg.Storage, logger)
	go ret.Run(ctx)

	// --- SMTP server ---
	smtp := smtpsrv.NewServer(cfg.SMTP, cfg.Storage.FallbackRetentionH, pg, obj, res, hub, dispatcher, defaultPolicy, logger)
	go func() {
		if err := smtp.Start(ctx); err != nil {
			logger.Error().Err(err).Msg("SMTP server error")
			cancel()
		}
	}()

	// --- HTTP API ---
	rl := middleware.NewRateLimiter(rdb, pg, 20)
	handler := api.NewRouter(pg, obj, hub, dispatcher, namingMode, cfg.StripPlusTag, defaultPolicy, cfg.AdminKey, cfg.SMTP.Domain, publicTenantID, rl, logger)

	httpSrv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info().Str("addr", cfg.HTTP.Addr).Msg("HTTP API starting")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("HTTP server error")
			cancel()
		}
	}()

	logger.Info().Msg("TabMail is running")

	<-ctx.Done()
	logger.Info().Msg("shutting down...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()

	_ = httpSrv.Shutdown(shutCtx)
	_ = smtp.Shutdown(shutCtx)
	logger.Info().Msg("shutdown complete")
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
