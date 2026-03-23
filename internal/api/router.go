package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/store"
)

//go:embed openapi.yaml
var openapiSpec embed.FS

func NewRouter(
	st store.Store,
	obj store.ObjectStore,
	hub *realtime.Hub,
	dispatcher *hooks.Dispatcher,
	namingMode policy.NamingMode,
	stripPlus bool,
	defaultPolicy models.SMTPPolicy,
	adminKey string,
	mailboxTokenSecret string,
	expectedMXHost string,
	publicTenantID string,
	httpCfg config.HTTP,
	rl *middleware.RateLimiter,
	logger zerolog.Logger,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   append([]string(nil), httpCfg.AllowedOrigins...),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   append([]string(nil), httpCfg.AllowedHeaders...),
		AllowCredentials: httpCfg.AllowCredentials,
		MaxAge:           86400,
	}))

	r.Use(middleware.Auth(st, adminKey, publicTenantID))
	r.Use(rl.Middleware)

	dh := handlers.NewDomainHandler(st, dispatcher, expectedMXHost, logger)
	mh := handlers.NewMailboxHandler(st, obj, dispatcher, namingMode, stripPlus, mailboxTokenSecret, logger)
	msg := handlers.NewMessageHandler(st, obj, hub, dispatcher, namingMode, stripPlus, mailboxTokenSecret, logger)
	adm := handlers.NewAdminHandler(st, dispatcher, defaultPolicy, logger)
	mon := handlers.NewMonitorHandler(st, hub, logger)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/token", mh.IssueToken)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireTenantKeyOrAdmin)

			// -- Domains / Zones --
			r.With(middleware.RequireScopes("domains:read")).Get("/domains", dh.ListZones)
			r.With(middleware.RequireScopes("domains:write")).Post("/domains", dh.CreateZone)
			r.With(middleware.RequireScopes("domains:write")).Delete("/domains/{id}", dh.DeleteZone)
			r.With(middleware.RequireScopes("domains:write")).Post("/domains/{id}/verify", dh.TriggerVerify)
			r.With(middleware.RequireScopes("domains:read")).Get("/domains/{id}/verification-status", dh.VerificationStatus)

			// -- Domain routes --
			r.With(middleware.RequireScopes("routes:read", "domains:read")).Get("/domains/{id}/routes", dh.ListRoutes)
			r.With(middleware.RequireScopes("routes:write", "domains:write")).Post("/domains/{id}/routes", dh.CreateRoute)
			r.With(middleware.RequireScopes("routes:write", "domains:write")).Delete("/domains/{id}/routes/{routeId}", dh.DeleteRoute)

			// -- Mailboxes --
			r.With(middleware.RequireScopes("mailboxes:read")).Get("/mailboxes", mh.List)
			r.With(middleware.RequireScopes("mailboxes:write")).Post("/mailboxes", mh.Create)
			r.With(middleware.RequireScopes("mailboxes:write")).Delete("/mailboxes/{id}", mh.Delete)
		})

		// -- Messages (mailbox-centric) --
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}", msg.ListMessages)
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}/events", msg.StreamMailbox)
		r.With(middleware.RequireScopes("messages:write")).Delete("/mailbox/{address}", msg.PurgeMailbox)
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}/{id}", msg.GetMessage)
		r.With(middleware.RequireScopes("messages:read")).Get("/mailbox/{address}/{id}/source", msg.GetSource)
		r.With(middleware.RequireScopes("messages:write")).Patch("/mailbox/{address}/{id}", msg.MarkSeen)
		r.With(middleware.RequireScopes("messages:write")).Delete("/mailbox/{address}/{id}", msg.DeleteMessage)

		// -- Admin (requires X-Admin-Key) --
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAdmin)

			r.Get("/admin/tenants", adm.ListTenants)
			r.Post("/admin/tenants", adm.CreateTenant)
			r.Patch("/admin/tenants/{id}", adm.UpdateTenantOverride)
			r.Delete("/admin/tenants/{id}", adm.DeleteTenant)
			r.Get("/admin/tenants/{id}/config", adm.GetEffectiveConfig)

			r.Post("/admin/tenants/{id}/keys", adm.CreateAPIKey)
			r.Get("/admin/tenants/{id}/keys", adm.ListAPIKeys)
			r.Delete("/admin/tenants/{id}/keys/{keyId}", adm.DeleteAPIKey)

			r.Get("/admin/plans", adm.ListPlans)
			r.Post("/admin/plans", adm.CreatePlan)
			r.Patch("/admin/plans/{id}", adm.UpdatePlan)
			r.Delete("/admin/plans/{id}", adm.DeletePlan)

			r.Get("/admin/stats", adm.Stats)
			r.Get("/admin/status", adm.Stats)
			r.Get("/admin/policy", adm.GetSMTPPolicy)
			r.Patch("/admin/policy", adm.UpdateSMTPPolicy)
			r.Get("/admin/monitor/events", mon.StreamAll)
			r.Get("/admin/monitor/history", mon.History)
			r.Get("/admin/audit", adm.ListAudit)
			r.Get("/admin/ingest/jobs", adm.ListIngestJobs)
			r.Get("/admin/webhooks/deliveries", adm.ListWebhookDeliveries)
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
		webhookDead, _ := st.CountWebhookDeliveriesByState(r.Context(), "dead")
		webhookPending, _ := st.CountWebhookDeliveriesByState(r.Context(), "pending", "retry", "processing")
		ingestPending, _ := st.CountIngestJobsByState(r.Context(), "pending", "retry", "processing")
		snapshot := metrics.Snapshot(dispatcher != nil && dispatcher.Enabled(), webhookDead)
		body := metrics.RenderPrometheus(snapshot, map[string]float64{
			"tabmail_webhooks_backlog": float64(webhookPending),
			"tabmail_ingest_backlog":   float64(ingestPending),
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
