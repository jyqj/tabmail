package realtime

import (
	"sync"
	"testing"
)

func TestConcurrentPublishAndUnsubscribeNeverSendsOnClosedChannel(t *testing.T) {
	h := NewHub(0, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, close := h.Subscribe("employee@example.test")
				h.Publish(Event{Mailbox: "employee@example.test", Type: EventPing})
				close()
			}
		}()
	}
	wg.Wait()
}
