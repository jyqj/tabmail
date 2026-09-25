package api

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app/companymail"
	"tabmail/internal/app/submissions"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/settings"
	"tabmail/internal/store"
)

//go:embed openapi.yaml
var openapiSpec embed.FS

// docsAssets holds the vendored Swagger UI / ReDoc bundles so /docs and
// /redoc work without loading script from third-party CDNs.
//
//go:embed docsassets/*.js docsassets/*.css
var docsAssets embed.FS

type metricsDBCounts struct {
	webhookDead      int
	webhookPending   int
	ingestReady      int
	ingestProcessing int
}

type metricsDBCountCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	expiresAt time.Time
	value     metricsDBCounts
}

func newMetricsDBCountCache(ttl time.Duration) *metricsDBCountCache {
	return &metricsDBCountCache{ttl: ttl}
}

func (c *metricsDBCountCache) Get(now time.Time, load func() metricsDBCounts) metricsDBCounts {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.expiresAt) {
		return c.value
	}
	value := load()
	c.value = value
	c.expiresAt = now.Add(c.ttl)
	return value
}

// RouterConfig bundles all parameters for NewRouter.
type RouterConfig struct {
	CompanyOnly        bool
	Readiness          func(context.Context) error
	RuntimeConfig      map[string]any
	Store              store.Store
	ObjectStore        store.ObjectStore
	RawObjects         *rawobject.Store
	Hub                *realtime.Hub
	Dispatcher         *hooks.Dispatcher
	NamingMode         policy.NamingMode
	StripPlus          bool
	DefaultPolicy      models.SMTPPolicy
	JWTSecret          string
	MailboxTokenSecret string
	ExpectedMXHost     string
	PublicTenantID     string
	DefaultPlanID      uuid.UUID
	OpenRegistration   bool
	Settings           *settings.Manager
	HTTP               config.HTTP
	RateLimiter        *middleware.RateLimiter
	AuthCache          *middleware.CachedAuthStore
	OutboundService    *outbound.Service
	Resolver           *resolver.Resolver
	IngestInvalidator  policyInvalidatorProvider
	// CompanyRepository is the company workflow surface. The production
	// assembly (cmd/tabmail/main.go) always passes the Postgres store, which
	// implements company.Repository at compile time. Nil explicitly disables
	// the /company routes and the durable mailbox event stream — an affordance
	// for legacy test adapters whose store cannot implement the interface,
	// never a runtime fallback: a missing dependency is a wiring decision,
	// not a silent capability loss.
	CompanyRepository company.Repository
	Logger            zerolog.Logger
}

// policyInvalidatorProvider narrows the ingest.Service to the method the admin
// handler needs, so router.go does not import internal/ingest (which would pull
// redis/enmime into the API package's dependency surface).
type policyInvalidatorProvider interface {
	InvalidateSMTPPolicy()
}

func NewRouter(cfg RouterConfig) http.Handler {
	st := cfg.Store
	cached := cfg.AuthCache
	if cached == nil {
		cached = middleware.NewCachedAuthStore(st, st.EffectiveConfig)
	}
	r := chi.NewRouter()
	metricsCounts := newMetricsDBCountCache(5 * time.Second)

	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   append([]string(nil), cfg.HTTP.AllowedOrigins...),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   append([]string(nil), cfg.HTTP.AllowedHeaders...),
		AllowCredentials: cfg.HTTP.AllowCredentials,
		MaxAge:           86400,
	}))

	r.Use(middleware.Auth(cached, cfg.JWTSecret, cfg.PublicTenantID))
	r.Use(middleware.PermissionLoader(cached))
	r.Use(cfg.RateLimiter.Middleware)

	dh := handlers.NewDomainHandler(st, cfg.ObjectStore, cfg.RawObjects, cfg.Dispatcher, cfg.ExpectedMXHost, cfg.Resolver, cfg.Logger)
	adm := handlers.NewAdminHandler(st, cfg.Dispatcher, cfg.DefaultPolicy, cfg.Settings, cfg.IngestInvalidator, cfg.Logger)
	mon := handlers.NewMonitorHandler(st, cfg.Hub, cfg.Logger)
	refreshStream := func(r *http.Request) (*http.Request, error) {
		return middleware.RevalidateRequest(r, st, cfg.JWTSecret, cfg.PublicTenantID)
	}
	mon.SetStreamRevalidator(refreshStream)
	auth := handlers.NewAuthHandler(st, cfg.JWTSecret, cfg.DefaultPlanID, cfg.OpenRegistration, cfg.Settings, cfg.HTTP.CookieSecure, cfg.Logger)
	auth.SetCompanyOnly(cfg.CompanyOnly)
	ua := handlers.NewUserAdminHandler(st, cfg.Logger)
	perm := handlers.NewPermissionHandler(st, cfg.Logger)
	var oh *handlers.OutboundHandler
	if cfg.OutboundService != nil {
		oh = handlers.NewOutboundHandler(cfg.OutboundService, st, cfg.Logger)
	}
	// The submissions use-case service backs the company draft submit flow and
	// both handlers' outbound-job accessibility views. A nil OutboundService is
	// a wiring decision (outbound disabled), preserved as an explicit
	// capability flag on the service, never a runtime panic.
	subs := submissions.NewService(cfg.CompanyRepository, st, cfg.OutboundService, cfg.Logger)

	r.Route("/api/v1", func(r chi.Router) {
		if cfg.CompanyRepository != nil {
			cdh := handlers.NewCompanyDomainHandler(dh.Service(), cfg.CompanyRepository, cfg.Logger)
			mailService := companymail.NewService(cfg.CompanyRepository, cfg.ObjectStore)
			workbench := handlers.NewCompanyMailHandler(cfg.CompanyRepository, mailService, subs, cfg.Logger)
			events := handlers.NewMailboxEventHandler(cfg.CompanyRepository, refreshStream, cfg.Logger)
			handlers.RegisterCompanyRoutes(r, handlers.CompanyRoutes{
				Setup:     handlers.NewCompanySetupHandler(cfg.CompanyRepository, cfg.Logger),
				Mailboxes: handlers.NewMailboxAdminHandler(cfg.CompanyRepository, cfg.Logger),
				Templates: handlers.NewCompanyTemplateHandler(cfg.CompanyRepository, st, cfg.Logger),
				Recovery:  handlers.NewCompanyRecoveryHandler(cfg.CompanyRepository, st, cfg.ObjectStore, subs, cfg.Logger),
				Mail:      workbench, Events: events, Domains: cdh,
				Archive:   handlers.NewMailArchiveHandler(cfg.CompanyRepository, cfg.Logger),
				Employees: handlers.NewEmployeeLifecycleHandler(cfg.CompanyRepository, cfg.Logger),
				Drafts:    handlers.NewCompanyDraftHandler(cfg.CompanyRepository, cfg.Logger),
				Index:     handlers.NewCompanyIndexHandler(cfg.CompanyRepository, cfg.Logger),
				Console:   handlers.NewCompanyConsoleHandler(cfg.CompanyRepository, cfg.Logger),
			})
		}
		// -- Auth (public, no auth required) --
		r.Post("/auth/login", auth.Login)
		r.Post("/auth/register", auth.Register)
		r.Post("/auth/refresh", auth.Refresh)

		// -- Auth (requires login) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Post("/auth/logout", auth.Logout)
			r.Get("/auth/me", auth.Me)
			r.Post("/auth/change-password", auth.ChangePassword)
			r.Get("/auth/me/permissions", perm.MyPermissions)
		})

		// -- Tenant resources (requires API key, JWT user, or admin) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireTenantKeyOrAdmin)

			// -- Domains / Zones --
			r.With(middleware.RequireScopes("domains:read")).Get("/domains", dh.ListZones)

			// -- Outbound / Sending --
			if oh != nil {
				r.With(middleware.RequireScopes("send:read")).Get("/outbound", oh.ListJobs)
				r.With(middleware.RequireScopes("send:read")).Get("/outbound/{id}", oh.GetJob)
				r.With(middleware.RequireScopes("send:read")).Get("/outbound/{id}/attempts", oh.ListAttempts)
				r.With(middleware.RequireScopes("send:write")).Post("/outbound/{id}/retry", oh.RetryJob)
				r.With(middleware.RequireScopes("suppression:read")).Get("/suppression", oh.ListSuppressions)
				r.With(middleware.RequireScopes("suppression:manage")).Delete("/suppression/{id}", oh.DeleteSuppression)
			}

		})

		// -- User API keys (own tenant; interactive JWT sessions only) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Post("/keys", adm.UserCreateAPIKey)
			r.Get("/keys", adm.UserListAPIKeys)
			r.Delete("/keys/{keyId}", adm.UserDeleteAPIKey)
		})

		// -- Admin (tenant-level, accessible by super_admin and admin) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAdmin) // super_admin or admin

			r.Get("/admin/domains", dh.AdminListZones)

			// -- Permission profiles --
			r.Get("/admin/permissions", perm.ListProfiles)
			r.Post("/admin/permissions", perm.CreateProfile)
			r.Patch("/admin/permissions/{id}", perm.UpdateProfile)
			r.Delete("/admin/permissions/{id}", perm.DeleteProfile)

			// -- User permissions --
			r.Get("/admin/users/{id}/permissions", perm.GetUserPermission)
			r.Put("/admin/users/{id}/permissions", perm.SetUserPermissionOverride)
			r.Delete("/admin/users/{id}/permissions", perm.DeleteUserPermissionOverride)

			// -- User management --
			r.Get("/admin/users", ua.ListUsers)
			r.Patch("/admin/users/{id}", ua.UpdateUserByAdmin)
			r.Delete("/admin/users/{id}", ua.DeleteUserByAdmin)
		})

		// -- Super admin only --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireSuperAdmin) // only super_admin

			r.Get("/admin/tenants", adm.ListTenants)
			r.Post("/admin/tenants", adm.CreateTenant)
			r.Patch("/admin/tenants/{id}", adm.UpdateTenantOverride)
			r.Delete("/admin/tenants/{id}", adm.DeleteTenant)
			r.Get("/admin/tenants/{id}/config", adm.GetEffectiveConfig)

			r.Post("/admin/tenants/{id}/keys", adm.CreateAPIKey)
			r.Get("/admin/tenants/{id}/keys", adm.ListAPIKeys)
			r.Delete("/admin/tenants/{id}/keys/{keyId}", adm.DeleteAPIKey)

			r.Get("/admin/stats", adm.Stats)
			r.Get("/admin/status", adm.Stats)
			r.Get("/admin/monitor/events", mon.StreamAll)
			r.Get("/admin/monitor/history", mon.History)
			r.Get("/admin/audit", adm.ListAudit)
			r.Get("/admin/ingest/jobs", adm.ListIngestJobs)
			r.Get("/admin/webhooks/deliveries", adm.ListWebhookDeliveries)

			r.Post("/admin/invite", ua.InviteAdmin)

			r.Get("/admin/plans", adm.ListPlans)
			r.Post("/admin/plans", adm.CreatePlan)
			r.Patch("/admin/plans/{id}", adm.UpdatePlan)
			r.Delete("/admin/plans/{id}", adm.DeletePlan)

			r.Get("/admin/policy", adm.GetSMTPPolicy)
			r.Patch("/admin/policy", adm.UpdateSMTPPolicy)

			// -- System settings (platform admin only) --
			r.Get("/admin/runtime-config", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"data": cfg.RuntimeConfig})
			})
			r.Get("/admin/settings", adm.ListSettings)
			r.Patch("/admin/settings", adm.UpdateSettings)
		})
	})

	// --- Documentation ---
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(openapiSpec, "openapi.yaml")
		if err != nil {
			http.Error(w, "spec not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Write(data)
	})
	r.Get("/docs", serveSwaggerUI)
	r.Get("/redoc", serveRedoc)
	docsAssetsSub, _ := fs.Sub(docsAssets, "docsassets")
	docsAssetsServer := http.StripPrefix("/docs-assets/", http.FileServerFS(docsAssetsSub))
	r.Get("/docs-assets/*", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		docsAssetsServer.ServeHTTP(w, r)
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	metricsToken := strings.TrimSpace(cfg.HTTP.MetricsToken)
	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// /metrics is never public: it requires either the configured scrape
		// token or an authenticated super-admin session.
		if !metricsAuthorized(r, metricsToken) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		counts := metricsCounts.Get(time.Now(), func() metricsDBCounts {
			webhookDead, _ := st.CountWebhookDeliveriesByState(r.Context(), "dead")
			webhookPending, _ := st.CountWebhookDeliveriesByState(r.Context(), "pending", "retry", "processing")
			ingestReady, _ := st.CountIngestJobsByState(r.Context(), "pending", "retry")
			ingestProcessing, _ := st.CountIngestJobsByState(r.Context(), "processing")
			return metricsDBCounts{
				webhookDead:      webhookDead,
				webhookPending:   webhookPending,
				ingestReady:      ingestReady,
				ingestProcessing: ingestProcessing,
			}
		})
		snapshot := metrics.Snapshot(cfg.Dispatcher != nil && cfg.Dispatcher.Enabled(), counts.webhookDead)
		values := map[string]float64{
			"tabmail_webhooks_backlog":         float64(counts.webhookPending),
			"tabmail_ingest_backlog":           float64(counts.ingestReady + counts.ingestProcessing),
			"tabmail_ingest_queue_depth":       float64(counts.ingestReady + counts.ingestProcessing),
			"tabmail_ingest_queue_ready_depth": float64(counts.ingestReady),
			"tabmail_ingest_queue_inflight":    float64(counts.ingestProcessing),
		}
		if health, ok := st.(interface {
			CompanyMetrics(context.Context) (map[string]float64, error)
		}); ok {
			extra, err := health.CompanyMetrics(r.Context())
			if err != nil {
				values["tabmail_operational_metrics_error"] = 1
			} else {
				values["tabmail_operational_metrics_error"] = 0
				for k, v := range extra {
					values[k] = v
				}
			}
		}
		body := metrics.RenderPrometheus(snapshot, values)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Health endpoints must not depend on identity-store lookups before
		// reporting their own dependency status.
		if req.Method == http.MethodGet && (req.URL.Path == "/health" || req.URL.Path == "/ready") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			if req.URL.Path == "/health" {
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
			defer cancel()
			if cfg.Readiness == nil || cfg.Readiness(ctx) != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"status":"not_ready"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ready"}`))
			return
		}
		r.ServeHTTP(w, req)
	})
}

// metricsAuthorized allows a request to scrape /metrics when it carries the
// configured metrics bearer token or belongs to an authenticated super-admin
// session. With no token configured, only super admins can read metrics.
func metricsAuthorized(r *http.Request, token string) bool {
	if token != "" {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if strings.HasPrefix(header, prefix) {
			presented := strings.TrimSpace(strings.TrimPrefix(header, prefix))
			if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
				return true
			}
		}
	}
	if user := middleware.UserFromCtx(r.Context()); user != nil && user.Role == models.RoleSuperAdmin {
		return true
	}
	return false
}

func serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html><head><title>TabMail API</title>
<link rel="stylesheet" href="/docs-assets/swagger-ui.css">
</head><body>
<div id="swagger-ui"></div>
<script src="/docs-assets/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:"/openapi.yaml",dom_id:"#swagger-ui",deepLinking:true})</script>
</body></html>`))
}

func serveRedoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html><head><title>TabMail API</title>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
</head><body>
<redoc spec-url="/openapi.yaml"></redoc>
<script src="/docs-assets/redoc.standalone.js"></script>
</body></html>`))
}
