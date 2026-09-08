/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

/*

package discovery manages a queue of discovery requests: an ordered
queue with no duplicates.

push() operation never blocks while pop() blocks on an empty queue.

*/

package discovery

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/inst"

	"github.com/openark/orchestrator/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

// Queue contains information for managing discovery requests
type Queue struct {
	sync.Mutex

	name string

	queue        chan inst.InstanceKey
	queuedKeys   map[inst.InstanceKey]time.Time
	consumedKeys map[inst.InstanceKey]time.Time
	queuedItems  atomic.Int64
	activeItems  atomic.Int64
}

// DiscoveryQueue contains the discovery queue which can then be accessed via an API call for monitoring.
// Currently this is accessed by ContinuousDiscovery() but also from http api calls.
// I may need to protect this better?
var discoveryQueue map[string](*Queue)
var dcLock sync.Mutex

func init() {
	discoveryQueue = make(map[string](*Queue))
}

func ReturnQueue(name string) *Queue {
	dcLock.Lock()
	defer dcLock.Unlock()
	if q, found := discoveryQueue[name]; found {
		return q
	}
	return nil
}

// CreateOrReturnQueue allows for creation of a new discovery queue or
// returning a pointer to an existing one given the name.
func CreateOrReturnQueue(name string) *Queue {
	dcLock.Lock()
	defer dcLock.Unlock()
	if q, found := discoveryQueue[name]; found {
		return q
	}

	q := &Queue{
		name:         name,
		queuedKeys:   make(map[inst.InstanceKey]time.Time),
		consumedKeys: make(map[inst.InstanceKey]time.Time),
		queue:        make(chan inst.InstanceKey, config.Config.DiscoveryQueueCapacity),
	}
	observability.Gauge("orchestrator_discovery_queue_items", "Current deduplicated queue entries", q.queuedItems.Load, attribute.String("queue", name), attribute.String("state", "queued"))
	observability.Gauge("orchestrator_discovery_queue_items", "Current deduplicated queue entries", q.activeItems.Load, attribute.String("queue", name), attribute.String("state", "active"))

	discoveryQueue[name] = q

	return q
}

// QueueLen returns pending keys; channel entries are the same keys and must not be counted twice.
func (q *Queue) QueueLen() int {
	q.Lock()
	defer q.Unlock()

	return len(q.queuedKeys)
}

// Push enqueues a key if it is not on a queue and is not being
// processed; silently returns otherwise.
func (q *Queue) Push(key inst.InstanceKey) {
	q.Lock()
	defer q.Unlock()

	// is it enqueued already?
	if _, found := q.queuedKeys[key]; found {
		return
	}

	// is it being processed now?
	if _, found := q.consumedKeys[key]; found {
		return
	}

	q.queuedKeys[key] = time.Now()
	q.queuedItems.Store(int64(len(q.queuedKeys)))
	q.queue <- key
}

// Consume fetches a key to process; blocks if queue is empty.
// Release must be called once after Consume.
func (q *Queue) Consume() inst.InstanceKey {
	q.Lock()
	queue := q.queue
	q.Unlock()

	key := <-queue

	q.Lock()
	defer q.Unlock()

	// alarm if have been waiting for too long
	timeOnQueue := time.Since(q.queuedKeys[key])
	if timeOnQueue > time.Duration(config.Config.InstancePollSeconds)*time.Second {
		log.Warningf("key %v spent %.4fs waiting on a discoveryQueue", key, timeOnQueue.Seconds())
	}

	q.consumedKeys[key] = q.queuedKeys[key]

	delete(q.queuedKeys, key)
	q.queuedItems.Store(int64(len(q.queuedKeys)))
	q.activeItems.Store(int64(len(q.consumedKeys)))

	return key
}

// Release removes a key from a list of being processed keys
// which allows that key to be pushed into the queue again.
func (q *Queue) Release(key inst.InstanceKey) {
	q.Lock()
	defer q.Unlock()

	delete(q.consumedKeys, key)
	q.activeItems.Store(int64(len(q.consumedKeys)))
}
