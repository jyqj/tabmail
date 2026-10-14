package realtime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"

	"tabmail/internal/metrics"
	"tabmail/internal/models"
)

func TestR5MonitorRecorderOutcomesAreObservable(t *testing.T) {
	for _, outcome := range []string{"stored", "storage-error", "deadline", "no-recorder"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				readCounters := func() (map[string]int64, string) {
					body := metrics.RenderPrometheus(metrics.Snapshot(false, 0), nil)
					values := make(map[string]int64)
					for _, line := range strings.Split(body, "\n") {
						fields := strings.Fields(line)
						if len(fields) == 2 && strings.HasPrefix(fields[0], "tabmail_realtime_monitor_events_") {
							value, err := strconv.ParseInt(fields[1], 10, 64)
							if err != nil {
								t.Fatalf("invalid monitor counter: %q", line)
							}
							values[fields[0]] = value
						}
					}
					return values, body
				}
				const recorded = "tabmail_realtime_monitor_events_recorded_total"
				const failed = "tabmail_realtime_monitor_events_failed_total"
				before, _ := readCounters()
				calls := 0
				var recorder Recorder
				if outcome != "no-recorder" {
					recorder = r5MonitorRecorderFunc(func(ctx context.Context, _ *models.MonitorEvent) error {
						calls++
						switch outcome {
						case "storage-error":
							return errors.New("private-database-error-detail")
						case "deadline":
							<-ctx.Done()
							return ctx.Err()
						default:
							return nil
						}
					})
				}
				hub := NewHub(1, recorder)
				hub.Publish(Event{Type: EventMessage, Mailbox: "private-recipient@mail.test", MessageID: "committed-message"})
				after, body := readCounters()
				if _, exists := after[recorded]; !exists {
					t.Error("successful monitor recording is not observable")
				}
				if _, exists := after[failed]; !exists {
					t.Error("failed monitor recording is not observable")
				}
				if len(after) != 2 || strings.Contains(body, "private-database-error-detail") || strings.Contains(body, "private-recipient@mail.test") {
					t.Error("monitor counters must expose only the two fixed result series")
				}
				var wantRecorded, wantFailed int64
				wantCalls := 1
				switch outcome {
				case "stored":
					wantRecorded = 1
				case "no-recorder":
					wantCalls = 0
				default:
					wantFailed = 1
				}
				if calls != wantCalls || after[recorded]-before[recorded] != wantRecorded || after[failed]-before[failed] != wantFailed {
					t.Errorf("wrong observed outcome: calls=%d recorded=%d failed=%d; want calls=%d recorded=%d failed=%d", calls, after[recorded]-before[recorded], after[failed]-before[failed], wantCalls, wantRecorded, wantFailed)
				}
			})
		})
	}
}
