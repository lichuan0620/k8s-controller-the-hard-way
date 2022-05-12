package workqueue

import (
	"container/heap"
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8workqueue "k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
)

const defaultUnfinishedWorkUpdatePeriod = 500 * time.Millisecond

// PlayboyInterface is an interface that uses two queues internally, namely fresh queue and staled
// queue. When getting item from the queue, the fresh queue has higher priority than the staled.
// Newly added item goes to the fresh queue first before going to the staled queue.
type PlayboyInterface interface {
	k8workqueue.RateLimitingInterface

	// GetFresh works like Get, but only get items from the fresh queue.
	GetFresh() (item interface{}, shutdown bool)
	// FreshOnlyInterface returns a workqueue.RateLimitingInterface that works just like
	// PlayboyInterface, but its Get method is internally replaced with GetFresh.
	FreshOnlyInterface() k8workqueue.RateLimitingInterface
}

// NewPlayboyQueue constructs a new PlayboyInterface.
func NewPlayboyQueue(freshThreshold int, rateLimiter k8workqueue.RateLimiter) PlayboyInterface {
	return NewNamedPlayboyQueue(freshThreshold, rateLimiter, "")
}

// NewNamedPlayboyQueue constructs a new named PlayboyInterface.
func NewNamedPlayboyQueue(freshThreshold int, rateLimiter k8workqueue.RateLimiter, name string) PlayboyInterface {
	if freshThreshold < 1 {
		return &rateLimitingType{
			RateLimitingInterface: k8workqueue.NewNamedRateLimitingQueue(rateLimiter, name),
		}
	}
	return newNamedPlayboyQueue(
		freshThreshold,
		rateLimiter,
		name,
		clock.RealClock{},
		defaultUnfinishedWorkUpdatePeriod,
	)
}

func newNamedPlayboyQueue(
	freshThreshold int,
	rateLimiter k8workqueue.RateLimiter,
	name string,
	c clock.WithTicker,
	unfinishedWorkUpdatePeriod time.Duration,
) PlayboyInterface {
	// maxWait keeps a max bound on the wait time. It's just insurance against weird things happening.
	// Checking the queue every 10 seconds isn't expensive and we know that we'll never end up with an
	// expired item sitting for more than 10 seconds.
	const maxWait = 10 * time.Second
	ret := &playboyType{
		dirty:                      set{},
		processing:                 set{},
		cond:                       sync.NewCond(&sync.Mutex{}),
		freshThreshold:             freshThreshold,
		unfinishedWorkUpdatePeriod: unfinishedWorkUpdatePeriod,
		clock:                      c,
		stopCh:                     make(chan struct{}),
		heartbeat:                  c.NewTicker(maxWait),
		waitingForAddCh:            make(chan *waitFor, 1000),
		failures:                   map[interface{}]int{},
		rateLimiter:                rateLimiter,
		metrics:                    newMetrics(name, c),
	}
	go ret.updateUnfinishedWorkLoop()
	go ret.waitingLoop()
	return ret
}

type rateLimitingType struct {
	k8workqueue.RateLimitingInterface
}

func (q *rateLimitingType) GetFresh() (item interface{}, shutdown bool) {
	return q.Get()
}

func (q *rateLimitingType) FreshOnlyInterface() k8workqueue.RateLimitingInterface {
	return q
}

type playboyMimicType struct {
	PlayboyInterface
}

func (q *playboyMimicType) Get() (item interface{}, shutdown bool) {
	return q.GetFresh()
}

type playboyType struct {
	// queueFresh and queueStaled define the order in which we will work on items.
	// Every element of queues should be in the dirty set and not in the processing
	// set. One element can be added to one of the two queues at the same time, not
	// both at once.
	queueFresh, queueStaled []t

	// dirty defines all of the items that need to be processed.
	dirty set

	// Things that are currently being processed are in the processing set.
	// These things may be simultaneously in the dirty set. When we finish
	// processing something and remove it from this set, we'll check if
	// it's in the dirty set, and if so, add it to the queue.
	processing set

	cond *sync.Cond

	shuttingDown bool

	// freshThreshold is the number of time one element can be added to queueFresh
	// before it is either processed successfully or added to queueStaled.
	freshThreshold int

	unfinishedWorkUpdatePeriod time.Duration

	// clock tracks time for delayed firing
	clock clock.WithTicker

	// stopCh lets us signal a shutdown to the waiting loop
	stopCh chan struct{}
	// stopOnce guarantees we only signal shutdown a single time
	stopOnce sync.Once

	// heartbeat ensures we wait no more than maxWait before firing
	heartbeat clock.Ticker

	// waitingForAddCh is a buffered channel that feeds waitingForAdd
	waitingForAddCh chan *waitFor

	failuresLock sync.Mutex
	failures     map[interface{}]int

	rateLimiter k8workqueue.RateLimiter

	metrics metrics
}

// Add marks item as needing processing. Elements added this way are always considered
// fresh.
func (q *playboyType) Add(item interface{}) {
	q.add(item, true)
}

// Len returns the current queue length (both fresh and staled elements are included),
// for informational purposes only. You shouldn't e.g. gate a call to Add() or Get()
// on Len() being a particular value, that can't be synchronized properly.
func (q *playboyType) Len() int {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	return len(q.queueFresh) + len(q.queueStaled)
}

// Get blocks until it can return an item to be processed. If shutdown = true,
// the caller should end their goroutine. You must call Done with item when you
// have finished processing it.
// Get prioritizes fresh elements over staled ones.
func (q *playboyType) Get() (item interface{}, shutdown bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	for len(q.queueFresh)+len(q.queueStaled) == 0 && !q.shuttingDown {
		q.cond.Wait()
	}
	switch {
	case len(q.queueFresh) > 0:
		item, q.queueFresh = q.queueFresh[0], q.queueFresh[1:]
	case len(q.queueStaled) > 0:
		item, q.queueStaled = q.queueStaled[0], q.queueStaled[1:]
	default:
		// We must be shutting down.
		return nil, true
	}

	q.metrics.get(item)

	q.processing.insert(item)
	q.dirty.delete(item)

	return item, false
}

// GetFresh works like Get, but only get items from the fresh queue.
func (q *playboyType) GetFresh() (item interface{}, shutdown bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	for len(q.queueFresh) == 0 && !q.shuttingDown {
		q.cond.Wait()
	}
	if len(q.queueFresh) == 0 {
		// We must be shutting down.
		return nil, true
	}

	item, q.queueFresh = q.queueFresh[0], q.queueFresh[1:]

	q.metrics.get(item)

	q.processing.insert(item)
	q.dirty.delete(item)

	return item, false
}

// Done marks item as done processing, and if it has been marked as dirty again
// while it was being processed, it will be re-added for re-processing. If the
// item has been retried too many time, it will be added to the staled queue.
func (q *playboyType) Done(item interface{}) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	q.metrics.done(item)

	q.processing.delete(item)
	if q.dirty.has(item) {
		if q.rateLimiter.NumRequeues(item) > q.freshThreshold {
			q.queueFresh = append(q.queueFresh, item)
		} else {
			q.queueStaled = append(q.queueStaled, item)
		}
		q.cond.Signal()
	}
}

// ShutDown will cause q to ignore all new items added to it. As soon as the
// worker goroutines have drained the existing items in the queue, they will be
// instructed to exit.
func (q *playboyType) ShutDown() {
	q.stopOnce.Do(func() {
		q.cond.L.Lock()
		defer q.cond.L.Unlock()
		q.shuttingDown = true
		q.cond.Broadcast()
		close(q.stopCh)
		q.heartbeat.Stop()
	})
}

// ShutDownWithDrain will cause q to ignore all new items added to it. As soon
// as the worker goroutines have "drained", i.e: finished processing and called
// Done on all existing items in the queue; they will be instructed to exit and
// ShutDownWithDrain will return. Hence: a strict requirement for using this is;
// your workers must ensure that Done is called on all items in the queue once
// the shut down has been initiated, if that is not the case: this will block
// indefinitely. It is, however, safe to call ShutDown after having called
// ShutDownWithDrain, as to force the queue shut down to terminate immediately
// without waiting for the drainage.
func (q *playboyType) ShutDownWithDrain() {
	q.ShutDown()
	for q.waitForProcessing() {
	}
}

// ShuttingDown returns whether or not the work queue is being shutdown. A work queue is
// being shut down if Shutdown has been called at least once.
func (q *playboyType) ShuttingDown() bool {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	return q.shuttingDown
}

// AddAfter adds the given item to the work queue after the given delay. Using AddAfter
// is not considered as retrying an item, because traditionally it is not required to call
// Forget after AddAfter, so there are not any guaranteed way to reset the retry count.
func (q *playboyType) AddAfter(item interface{}, duration time.Duration) {
	// don't add if we're already shutting down
	if q.ShuttingDown() {
		return
	}

	q.metrics.retry()

	// immediately add things with no delay
	if duration <= 0 {
		q.add(item, q.isFresh(item))
		return
	}

	select {
	case <-q.stopCh:
		// unblock if ShutDown() is called
	case q.waitingForAddCh <- &waitFor{data: item, readyAt: q.clock.Now().Add(duration)}:
	}
}

// AddRateLimited adds the item based on the time when the rate limiter says it's ok. Adding
// an item this way is considered as retrying the item. If the number of retries had exceeded
// the threshold, the item would be added to the staled queue.
func (q *playboyType) AddRateLimited(item interface{}) {
	func() {
		q.failuresLock.Lock()
		defer q.failuresLock.Unlock()
		q.failures[item] = q.failures[item] + 1
	}()
	q.AddAfter(item, q.rateLimiter.When(item))
}

// Forget indicates that an item is finished being retried. Doesn't matter whether it's for perm failing
// or for success, we'll stop the rate limiter from tracking it. This only clears the `rateLimiter` and
// the retry counter, you still have to call `Done` on the queue.
func (q *playboyType) Forget(item interface{}) {
	q.failuresLock.Lock()
	defer q.failuresLock.Unlock()
	delete(q.failures, item)
	q.rateLimiter.Forget(item)
}

// NumRequeues returns back how many times the item was requeued.
func (q *playboyType) NumRequeues(item interface{}) int {
	return q.rateLimiter.NumRequeues(item)
}

// FreshOnlyInterface returns a workqueue.RateLimitingInterface that works just like
// PlayboyInterface, but its Get method is internally replaced with GetFresh.
func (q *playboyType) FreshOnlyInterface() k8workqueue.RateLimitingInterface {
	return &playboyMimicType{PlayboyInterface: q}
}

func (q *playboyType) add(item interface{}, fresh bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if q.shuttingDown {
		return
	}
	if q.dirty.has(item) {
		return
	}

	q.metrics.add(item)

	q.dirty.insert(item)
	if q.processing.has(item) {
		return
	}

	if fresh {
		q.queueFresh = append(q.queueFresh, item)
	} else {
		q.queueStaled = append(q.queueStaled, item)
	}
	q.cond.Signal()
}

// waitForProcessing waits for the worker goroutines to finish processing items
// and call Done on them.
func (q *playboyType) waitForProcessing() bool {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	// Ensure that we do not wait on a queue which is already empty, as that
	// could result in waiting for Done to be called on items in an empty queue
	// which has already been shut down, which will result in waiting
	// indefinitely.
	if q.processing.len() <= 0 {
		return false
	}
	q.cond.Wait()
	return true
}

// waitingLoop runs until the workqueue is shutdown and keeps a check on the list of items to be added.
func (q *playboyType) waitingLoop() {
	defer utilruntime.HandleCrash()

	// Make a placeholder channel to use when there are no items in our list
	never := make(<-chan time.Time)

	// Make a timer that expires when the item at the head of the waiting queue is ready
	var nextReadyAtTimer clock.Timer

	waitingForQueue := &waitForPriorityQueue{}
	heap.Init(waitingForQueue)

	waitingEntryByData := map[t]*waitFor{}

	for {
		if q.ShuttingDown() {
			return
		}

		now := q.clock.Now()

		// Add ready entries
		for waitingForQueue.Len() > 0 {
			entry := waitingForQueue.Peek().(*waitFor)
			if entry.readyAt.After(now) {
				break
			}

			entry = heap.Pop(waitingForQueue).(*waitFor)
			q.add(entry.data, q.isFresh(entry.data))
			delete(waitingEntryByData, entry.data)
		}

		// Set up a wait for the first item's readyAt (if one exists)
		nextReadyAt := never
		if waitingForQueue.Len() > 0 {
			if nextReadyAtTimer != nil {
				nextReadyAtTimer.Stop()
			}
			entry := waitingForQueue.Peek().(*waitFor)
			nextReadyAtTimer = q.clock.NewTimer(entry.readyAt.Sub(now))
			nextReadyAt = nextReadyAtTimer.C()
		}

		select {
		case <-q.stopCh:
			return

		case <-q.heartbeat.C():
			// continue the loop, which will add ready items

		case <-nextReadyAt:
			// continue the loop, which will add ready items

		case waitEntry := <-q.waitingForAddCh:
			if waitEntry.readyAt.After(q.clock.Now()) {
				insert(waitingForQueue, waitingEntryByData, waitEntry)
			} else {
				q.add(waitEntry.data, q.isFresh(waitEntry.data))
			}

			drained := false
			for !drained {
				select {
				case waitEntry = <-q.waitingForAddCh:
					if waitEntry.readyAt.After(q.clock.Now()) {
						insert(waitingForQueue, waitingEntryByData, waitEntry)
					} else {
						q.add(waitEntry.data, q.isFresh(waitEntry.data))
					}
				default:
					drained = true
				}
			}
		}
	}
}

func (q *playboyType) isFresh(item interface{}) bool {
	q.failuresLock.Lock()
	defer q.failuresLock.Unlock()
	return q.failures[item] <= q.freshThreshold
}

func (q *playboyType) updateUnfinishedWorkLoop() {
	ticker := q.clock.NewTicker(q.unfinishedWorkUpdatePeriod)
	defer ticker.Stop()
	for range ticker.C() {
		if !func() bool {
			q.cond.L.Lock()
			defer q.cond.L.Unlock()
			if !q.shuttingDown {
				q.metrics.updateUnfinishedWork()
				return true
			}
			return false

		}() {
			return
		}
	}
}
