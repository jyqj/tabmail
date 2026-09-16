package postgres

import "context"

// Global operational counts contain no recipients, message content or credentials.
func (s *PgStore) CompanyMetrics(ctx context.Context) (map[string]float64, error) {
	queries := map[string]string{
		"tabmail_outbound_uncertain":         `SELECT count(*) FROM outbound_jobs WHERE in_flight_domain<>'' AND state<>'processing'`,
		"tabmail_outbound_backlog":           `SELECT count(*) FROM outbound_jobs WHERE state IN ('pending','processing','retry')`,
		"tabmail_outbound_terminal_failures": `SELECT count(*) FROM outbound_jobs WHERE state IN ('failed','dead')`,
		"tabmail_ingress_recovery_backlog":   `SELECT count(*) FROM ingest_jobs WHERE state='dead'`,
		"tabmail_outbound_oldest_seconds":    `SELECT COALESCE(EXTRACT(EPOCH FROM now()-min(created_at)),0) FROM outbound_jobs WHERE state IN ('pending','processing','retry')`,
		"tabmail_active_workers":             `SELECT count(*) FROM runtime_instances WHERE role IN ('worker','all') AND last_seen>now()-interval '90 seconds'`,
	}
	result := map[string]float64{}
	for name, q := range queries {
		var n float64
		if err := s.pool.QueryRow(ctx, q).Scan(&n); err != nil {
			return nil, err
		}
		result[name] = n
	}
	return result, nil
}
