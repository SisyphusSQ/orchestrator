package discovery

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openark/orchestrator/go/inst"
	"github.com/openark/orchestrator/go/observability"
)

func TestQueueCountsEachKeyOnce(t *testing.T) {
	q := &Queue{queue: make(chan inst.InstanceKey, 4), queuedKeys: map[inst.InstanceKey]time.Time{}, consumedKeys: map[inst.InstanceKey]time.Time{}}
	key := inst.InstanceKey{Hostname: "fixture", Port: 3306}
	q.Push(key)
	q.Push(key)
	if q.QueueLen() != 1 || q.queuedItems.Load() != 1 {
		t.Fatalf("duplicate queue length: %d", q.QueueLen())
	}
	if got := q.Consume(); got != key {
		t.Fatal(got)
	}
	if q.QueueLen() != 0 || len(q.consumedKeys) != 1 || q.queuedItems.Load() != 0 || q.activeItems.Load() != 1 {
		t.Fatal("consume state inconsistent")
	}
	q.Push(key)
	if q.QueueLen() != 0 {
		t.Fatal("active key enqueued twice")
	}
	q.Release(key)
	if q.activeItems.Load() != 0 {
		t.Fatal("active gauge did not fall after release")
	}
	q.Push(key)
	if q.QueueLen() != 1 {
		t.Fatal("released key cannot be requeued")
	}
}

func TestMetricsScrapeDoesNotWaitForQueueLock(t *testing.T) {
	r, err := observability.New(t.Context(), "", 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Install()
	t.Cleanup(func() { _ = r.Close() })
	q := CreateOrReturnQueue("DEFAULT")
	q.Push(inst.InstanceKey{Hostname: "fixture", Port: 3306})
	q.Lock()
	defer q.Unlock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
		done <- w
	}()
	select {
	case w := <-done:
		if w.Code != 200 || !strings.Contains(w.Body.String(), `orchestrator_discovery_queue_items{queue="DEFAULT",state="queued"} 1`) {
			t.Fatalf("queue metric missing: status=%d", w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("scrape blocked on the business queue lock")
	}
}
