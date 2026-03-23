package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

const envPrefix = "TABMAIL"

type Root struct {
	Role                string `default:"all" desc:"Process role: all, api, smtp, retention"`
	LogLevel            string `default:"info" desc:"debug, info, warn, error"`
	DataDir             string `default:"/data" desc:"Base directory for raw .eml storage"`
	AdminKey            string `required:"true" desc:"Super-admin X-Admin-Key value"`
	MailboxTokenSecret  string `split_words:"true" required:"true" desc:"Signing secret for mailbox bearer tokens"`
	AutoCreateRouteRPM  int    `split_words:"true" default:"60" desc:"Per-route auto-create RPM (0=disable)"`
	AutoCreateTenantRPM int    `split_words:"true" default:"300" desc:"Per-tenant auto-create RPM (0=disable)"`
	MailboxNaming       string `default:"full" desc:"Mailbox naming: full, local, or domain"`
	StripPlusTag        bool   `default:"true" desc:"Strip +tag from local part"`
	MonitorHistory      int    `default:"50" desc:"Number of recent events to keep for monitor replay (0=disable)"`

	SMTP    SMTP
	HTTP    HTTP
	DB      DB
	Redis   Redis
	Storage Storage
	Webhook Webhook
	Ingest  Ingest
}

type SMTP struct {
	Addr                string        `default:"0.0.0.0:2525" desc:"SMTP listen address"`
	Domain              string        `default:"localhost" desc:"SMTP HELO/banner domain"`
	MaxRecipients       int           `default:"200" desc:"Max RCPT TO per message"`
	MaxMessageBytes     int           `default:"26214400" desc:"Max message size (bytes)"`
	Timeout             time.Duration `default:"300s" desc:"Idle connection timeout"`
	TLSEnabled          bool          `default:"false" desc:"Enable STARTTLS"`
	TLSCert             string        `default:"" desc:"TLS certificate path"`
	TLSKey              string        `default:"" desc:"TLS private key path"`
	ForceTLS            bool          `default:"false" desc:"Require TLS from connection start (implicit TLS)"`
	RejectOriginDomains []string      `desc:"Reject mail from these sender domains (supports * and ? wildcards)"`
	DefaultAccept       bool          `default:"true" desc:"Accept recipients by default unless rejected"`
	AcceptDomains       []string      `desc:"Accepted recipient domains when DefaultAccept=false"`
	RejectDomains       []string      `desc:"Rejected recipient domains when DefaultAccept=true"`
	DefaultStore        bool          `default:"true" desc:"Store recipients by default unless discarded"`
	StoreDomains        []string      `desc:"Store recipient domains when DefaultStore=false"`
	DiscardDomains      []string      `desc:"Discard recipient domains when DefaultStore=true"`
}

type HTTP struct {
	Addr             string   `default:"0.0.0.0:8080" desc:"HTTP API listen address"`
	BasePath         string   `default:"" desc:"URL path prefix (e.g. /api)"`
	AllowedOrigins   []string `split_words:"true" default:"http://127.0.0.1:3000,http://localhost:3000" desc:"Allowed CORS origins"`
	AllowedHeaders   []string `split_words:"true" default:"Authorization,Content-Type,X-API-Key,X-Admin-Key,X-Tenant-ID" desc:"Allowed CORS headers"`
	AllowCredentials bool     `split_words:"true" default:"false" desc:"Allow credentialed CORS requests"`
	TrustedProxies   []string `split_words:"true" default:"127.0.0.1/32,::1/128" desc:"Trusted proxy CIDRs/IPs for X-Real-IP/X-Forwarded-For"`
}

type DB struct {
	DSN             string        `default:"postgres://tabmail:tabmail@localhost:5432/tabmail?sslmode=disable" desc:"PostgreSQL connection string"`
	MaxOpenConns    int           `default:"25" desc:"Max open connections"`
	MaxIdleConns    int           `default:"5" desc:"Max idle connections"`
	ConnMaxLifetime time.Duration `default:"300s" desc:"Connection max lifetime"`
}

type Redis struct {
	Addr     string `default:"localhost:6379" desc:"Redis address"`
	Password string `default:"" desc:"Redis password"`
	DB       int    `default:"0" desc:"Redis database number"`
}

type Storage struct {
	RetentionScanInterval time.Duration `default:"60s" desc:"Retention scanner interval"`
	RetentionBatchSize    int           `default:"1000" desc:"Rows per cleanup batch"`
	FallbackRetentionH    int           `default:"24" desc:"System-level fallback retention hours"`
}

type Webhook struct {
	URLs         string        `default:"" desc:"Comma-separated inbound event webhook URLs"`
	Secret       string        `default:"" desc:"Optional webhook signature secret"`
	Timeout      time.Duration `default:"5s" desc:"Webhook request timeout"`
	MaxRetries   int           `default:"3" desc:"Max webhook retry attempts"`
	RetryDelay   time.Duration `default:"1s" desc:"Base webhook retry delay"`
	DeadLimit    int           `default:"100" desc:"Max in-memory dead-letter queue size"`
	PollInterval time.Duration `split_words:"true" default:"1s" desc:"Dispatcher polling interval for outbox/deliveries"`
	BatchSize    int           `split_words:"true" default:"100" desc:"Dispatcher batch size for outbox/deliveries"`
}

type Ingest struct {
	Durable      bool          `default:"false" desc:"When enabled, SMTP DATA durably enqueues ingest jobs instead of synchronously delivering"`
	PollInterval time.Duration `split_words:"true" default:"1s" desc:"Ingest worker polling interval"`
	BatchSize    int           `split_words:"true" default:"100" desc:"Ingest worker batch size"`
	MaxRetries   int           `split_words:"true" default:"5" desc:"Max ingest job retry attempts before dead-lettering"`
}

func Load() (*Root, error) {
	c := &Root{}
	if err := envconfig.Process(envPrefix, c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}
