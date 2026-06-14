package api

import (
	"embed"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
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
	if c != nil && now.Before(c.expiresAt) {
		return c.value
	}
	value := load()
	c.value = value
	c.expiresAt = now.Add(c.ttl)
	return value
}

// RouterConfig bundles all parameters for NewRouter.
type RouterConfig struct {
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
	OutboundService    *outbound.Service
	Resolver           *resolver.Resolver
	IngestInvalidator  policyInvalidatorProvider
	Logger             zerolog.Logger
}

// policyInvalidatorProvider narrows the ingest.Service to the method the admin
// handler needs, so router.go does not import internal/ingest (which would pull
// redis/enmime into the API package's dependency surface).
type policyInvalidatorProvider interface {
	InvalidateSMTPPolicy()
}

func NewRouter(cfg RouterConfig) http.Handler {
	st := cfg.Store
	r := chi.NewRouter()
	metricsCounts := newMetricsDBCountCache(5 * time.Second)

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   append([]string(nil), cfg.HTTP.AllowedOrigins...),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   append([]string(nil), cfg.HTTP.AllowedHeaders...),
		AllowCredentials: cfg.HTTP.AllowCredentials,
		MaxAge:           86400,
	}))

	r.Use(middleware.Auth(st, cfg.JWTSecret, cfg.PublicTenantID))
	r.Use(middleware.PermissionLoader(st))
	r.Use(cfg.RateLimiter.Middleware)

	dh := handlers.NewDomainHandler(st, cfg.ObjectStore, cfg.RawObjects, cfg.Dispatcher, cfg.ExpectedMXHost, cfg.NamingMode, cfg.MailboxTokenSecret, cfg.Resolver, cfg.Logger)
	mh := handlers.NewMailboxHandler(st, cfg.ObjectStore, cfg.RawObjects, cfg.Dispatcher, cfg.NamingMode, cfg.StripPlus, cfg.MailboxTokenSecret, cfg.RateLimiter, cfg.Logger)
	msg := handlers.NewMessageHandler(st, cfg.ObjectStore, cfg.RawObjects, cfg.Hub, cfg.Dispatcher, cfg.NamingMode, cfg.StripPlus, cfg.MailboxTokenSecret, cfg.Logger)
	adm := handlers.NewAdminHandler(st, cfg.Dispatcher, cfg.DefaultPolicy, cfg.Settings, cfg.IngestInvalidator, cfg.Logger)
	mon := handlers.NewMonitorHandler(st, cfg.Hub, cfg.Logger)
	auth := handlers.NewAuthHandler(st, cfg.JWTSecret, cfg.DefaultPlanID, cfg.OpenRegistration, cfg.Settings, cfg.Logger)
	perm := handlers.NewPermissionHandler(st, cfg.Logger)
	wh := handlers.NewWebhookEndpointHandler(st, cfg.Logger)
	si := handlers.NewSendIdentityHandler(st, cfg.Logger)
	var oh *handlers.OutboundHandler
	if cfg.OutboundService != nil {
		oh = handlers.NewOutboundHandler(cfg.OutboundService, st, cfg.Logger)
	}

	r.Route("/api/v1", func(r chi.Router) {
		// -- Auth (public, no auth required) --
		r.Post("/auth/login", auth.Login)
		r.Post("/auth/register", auth.Register)
		r.Post("/auth/refresh", auth.Refresh)
		r.Post("/auth/accept-invite", auth.AcceptInvite)

		// -- Auth (requires login) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Post("/auth/logout", auth.Logout)
			r.Get("/auth/me", auth.Me)
			r.Post("/auth/change-password", auth.ChangePassword)
			r.Get("/auth/me/permissions", perm.MyPermissions)
		})

		// -- Mailbox token issuance --
		r.Post("/token", mh.IssueToken)

		// -- Open resources (public or authenticated based on resource visibility) --
		r.Get("/resources/domains", dh.ListOpenZones)
		r.Get("/resources/domains/{id}/suggest-address", dh.SuggestOpenAddress)

		// -- Tenant resources (requires API key, JWT user, or admin) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireTenantKeyOrAdmin)

			// -- Domains / Zones --
			r.With(middleware.RequireScopes("domains:read")).Get("/domains", dh.ListZones)
			r.With(middleware.RequireScopes("domains:write")).Post("/domains", dh.CreateZone)
			r.With(middleware.RequireScopes("domains:write")).Delete("/domains/{id}", dh.DeleteZone)
			r.With(middleware.RequireScopes("domains:write")).Post("/domains/{id}/verify", dh.TriggerVerify)
			r.With(middleware.RequireScopes("domains:read")).Get("/domains/{id}/verification-status", dh.VerificationStatus)
			r.With(middleware.RequireScopes("domains:read")).Get("/domains/{id}/suggest-address", dh.SuggestAddress)

			// -- Domain routes --
			r.With(middleware.RequireScopes("routes:read", "domains:read")).Get("/domains/{id}/routes", dh.ListRoutes)
			r.With(middleware.RequireScopes("routes:write", "domains:write")).Post("/domains/{id}/routes", dh.CreateRoute)
			r.With(middleware.RequireScopes("routes:write", "domains:write")).Delete("/domains/{id}/routes/{routeId}", dh.DeleteRoute)
			r.With(middleware.RequireScopes("routes:read", "domains:read")).Post("/domains/{id}/routes/explain", dh.ExplainRoute)

			// -- Mailboxes --
			r.With(middleware.RequireScopes("mailboxes:read")).Get("/mailboxes", mh.List)
			r.With(middleware.RequireScopes("mailboxes:write")).Post("/mailboxes", mh.Create)
			r.With(middleware.RequireScopes("mailboxes:write")).Delete("/mailboxes/{id}", mh.Delete)

			// -- Webhook endpoints (tenant-level) --
			r.With(middleware.RequireScopes("webhooks:read")).Get("/webhook-endpoints", wh.List)
			r.With(middleware.RequireScopes("webhooks:write")).Post("/webhook-endpoints", wh.Create)
			r.With(middleware.RequireScopes("webhooks:write")).Patch("/webhook-endpoints/{id}", wh.Update)
			r.With(middleware.RequireScopes("webhooks:write")).Delete("/webhook-endpoints/{id}", wh.Delete)

			// -- Send Identities --
			r.With(middleware.RequireScopes("send:read")).Get("/send-identities", si.List)
			r.With(middleware.RequireScopes("send:write")).Post("/send-identities", si.Create)
			r.With(middleware.RequireScopes("send:write")).Delete("/send-identities/{id}", si.Delete)

			// -- Outbound / Sending --
			if oh != nil {
				r.With(middleware.RequireScopes("send:write")).Post("/send", oh.Send)
				r.With(middleware.RequireScopes("send:read")).Get("/outbound", oh.ListJobs)
				r.With(middleware.RequireScopes("send:read")).Get("/outbound/{id}", oh.GetJob)
				r.With(middleware.RequireScopes("send:read")).Get("/outbound/{id}/attempts", oh.ListAttempts)
				r.With(middleware.RequireScopes("send:write")).Post("/outbound/{id}/retry", oh.RetryJob)
				r.With(middleware.RequireScopes("send:read")).Get("/suppression", oh.ListSuppressions)
				r.With(middleware.RequireScopes("send:write")).Delete("/suppression/{id}", oh.DeleteSuppression)
			}

		})

		// -- User API keys (own tenant; interactive JWT sessions only) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Post("/keys", adm.UserCreateAPIKey)
			r.Get("/keys", adm.UserListAPIKeys)
			r.Delete("/keys/{keyId}", adm.UserDeleteAPIKey)
		})

		// -- Messages (mailbox-centric, public access for public mailboxes) --
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}", msg.ListMessages)
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}/events", msg.StreamMailbox)
		r.With(middleware.RequireScopes("messages:write")).Delete("/mailbox/{address}", msg.PurgeMailbox)
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}/{id}", msg.GetMessage)
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}/{id}/source", msg.GetSource)
		r.With(middleware.RequireScopes("messages:write")).Patch("/mailbox/{address}/{id}", msg.MarkSeen)
		r.With(middleware.RequireScopes("messages:write")).Delete("/mailbox/{address}/{id}", msg.DeleteMessage)

		// -- Break-glass: admin audited access to message content --
		r.With(middleware.RequireAdmin).Post("/mailbox/{address}/{id}/break-glass", msg.BreakGlassRead)
		r.With(middleware.RequireAdmin).Post("/mailbox/{address}/{id}/break-glass/source", msg.BreakGlassSource)

		// -- Admin (tenant-level, accessible by super_admin and admin) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAdmin) // super_admin or admin

			r.Get("/admin/domains", dh.AdminListZones)
			r.Patch("/admin/domains/{id}", dh.AdminUpdateZoneAccess)

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
			r.Get("/admin/users", auth.ListUsers)
			r.Patch("/admin/users/{id}", auth.UpdateUserByAdmin)
			r.Delete("/admin/users/{id}", auth.DeleteUserByAdmin)
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

			r.Post("/admin/invite", auth.InviteAdmin)

			r.Get("/admin/plans", adm.ListPlans)
			r.Post("/admin/plans", adm.CreatePlan)
			r.Patch("/admin/plans/{id}", adm.UpdatePlan)
			r.Delete("/admin/plans/{id}", adm.DeletePlan)

			r.Get("/admin/policy", adm.GetSMTPPolicy)
			r.Patch("/admin/policy", adm.UpdateSMTPPolicy)

			// -- System settings (platform admin only) --
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

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
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
		body := metrics.RenderPrometheus(snapshot, map[string]float64{
			"tabmail_webhooks_backlog":         float64(counts.webhookPending),
			"tabmail_ingest_backlog":           float64(counts.ingestReady + counts.ingestProcessing),
			"tabmail_ingest_queue_depth":       float64(counts.ingestReady + counts.ingestProcessing),
			"tabmail_ingest_queue_ready_depth": float64(counts.ingestReady),
			"tabmail_ingest_queue_inflight":    float64(counts.ingestProcessing),
		})
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})

	return r
}

func serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html><head><title>TabMail API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head><body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
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
<script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body></html>`))
}
