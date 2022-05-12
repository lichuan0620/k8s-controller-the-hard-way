package workqueue

import (
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	k8workqueue "k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock/testing"
)

var _ = Describe("PlayboyQueue", func() {
	const (
		fastDelay      = time.Millisecond
		slowDelay      = time.Second
		freshThreshold = 3
		key            = "key"
	)

	var (
		testC *testing.FakeClock
		queue PlayboyInterface
	)

	BeforeEach(func() {
		testC = testing.NewFakeClock(time.Now())
		queue = newNamedPlayboyQueue(
			freshThreshold,
			k8workqueue.NewItemFastSlowRateLimiter(fastDelay, slowDelay, freshThreshold),
			"",
			testC,
			defaultUnfinishedWorkUpdatePeriod,
		)
	})

	// these are features that workqueue.RateLimitingInterface implements
	Describe("basic functionalities", func() {
		It("can get items in order", func() {
			const count = 10
			for i := 0; i < count; i++ {
				queue.Add(strconv.Itoa(i))
			}
			Expect(queue.Len()).To(Equal(count))

			getChan := spawnGetChan(queue)
			defer func() {
				queue.ShutDown()
				Eventually(getChan).Should(BeClosed())
			}()
			for i := 0; i < count; i++ {
				k := strconv.Itoa(i)
				Eventually(getChan).Should(Receive(Equal(k)))
				queue.Done(k)
			}
		})

		It("can deduplicate added items", func() {
			for i := 0; i < 10; i++ {
				queue.Add(key)
			}
			Expect(queue.Len()).To(Equal(1))

			getChan := spawnGetChan(queue)
			defer func() {
				queue.ShutDown()
				Eventually(getChan).Should(BeClosed())
			}()

			// first Get should receive the deduplicated item
			Eventually(getChan).Should(Receive(Equal(key)))

			// now the queue should be empty
			Expect(queue.Len()).To(Equal(0))
			Eventually(getChan).ShouldNot(Receive())

			// before calling Done, subsequent Add should have no effect
			queue.Add(key)
			Eventually(getChan).ShouldNot(Receive())

			// after calling Done, subsequent Add should actually add item to the queue
			queue.Done(key)
			queue.Add(key)
			Eventually(getChan).Should(Receive(Equal(key)))
		})

		It("can add items and get them in order", func() {
			getChan := spawnGetChan(queue)
			defer func() {
				queue.ShutDown()
				Eventually(getChan).Should(BeClosed())
			}()

			Eventually(getChan).ShouldNot(Receive())
			Expect(queue.Len()).To(Equal(0))
			queue.Add(key)
			Eventually(getChan).Should(Receive(Equal(key)))
		})

		Context("auto spawn get channel", func() {
			var getChan chan interface{}

			BeforeEach(func() {
				getChan = spawnGetChan(queue)
			})

			AfterEach(func() {
				queue.ShutDown()
				Eventually(getChan).Should(BeClosed())
			})

			It("can re-queue item added during processing", func() {
				// adding and getting an item without calling Done
				queue.Add(key)
				Eventually(getChan).Should(Receive(Equal(key)))

				// subsequent Add should not cause the queue to grow
				queue.Add(key)
				Consistently(queue.Len()).Should(Equal(0))
				Eventually(getChan).ShouldNot(Receive())

				// after calling Done, Get should receive the previously added item
				queue.Done(key)
				Eventually(getChan).Should(Receive(Equal(key)))
			})

			It("can add item with delay", func() {
				queue.AddAfter(key, slowDelay)
				testC.Step(fastDelay)
				Eventually(getChan).ShouldNot(Receive())

				testC.Step(slowDelay)
				Eventually(getChan).Should(Receive(Equal(key)))
			})

			It("can limit add rate", func() {
				fakeNow := time.Now()
				testC.SetTime(fakeNow)
				EventuallyShouldReceiveAfterDelay := func(delay time.Duration) {
					fakeNow = fakeNow.Add(delay - time.Microsecond)
					testC.SetTime(fakeNow)
					Eventually(getChan).ShouldNot(Receive())
					fakeNow = fakeNow.Add(2 * time.Microsecond)
					testC.SetTime(fakeNow)
					Eventually(getChan).Should(Receive(Equal(key)))
				}

				By("initial add")
				queue.Add(key)
				Eventually(getChan).Should(Receive(Equal(key)))
				for i := 0; i < freshThreshold*2; i++ {
					queue.AddRateLimited(key)
					queue.Done(key)
					delay := fastDelay
					if i >= freshThreshold {
						delay = slowDelay
					}
					EventuallyShouldReceiveAfterDelay(delay)
				}
				queue.Forget(key)
				queue.Done(key)

				By("repeated add after rate limiter reset")
				queue.Add(key)
				Eventually(getChan).Should(Receive(Equal(key)))
				queue.AddRateLimited(key)
				queue.Done(key)
				EventuallyShouldReceiveAfterDelay(fastDelay)
			})
		})
	})

	// these are features unique to the Playboy queue.
	Describe("playboy functionalities", func() {
		const keyA, keyB = "A", "B"

		mustGetQueueItem := func() interface{} {
			item, shutdown := queue.Get()
			Expect(shutdown).To(BeFalse())
			return item
		}

		BeforeEach(func() {
			// some test cases might block when they go wrong, so we make sure they
			// can exit after a timeout.
			go func() {
				qCopy := queue
				time.Sleep(10 * slowDelay)
				qCopy.ShutDown()
			}()
		})

		Context("with fake time", func() {
			var fakeNow time.Time

			BeforeEach(func() {
				fakeNow = time.Now()
				testC.SetTime(fakeNow)
			})

			retry := func(item interface{}) {
				queue.AddRateLimited(item)
				queue.Done(item)
				fakeNow = fakeNow.Add(slowDelay)
				testC.SetTime(fakeNow)
				time.Sleep(fastDelay)
			}

			It("can explicitly get fresh item", func() {
				queue.Add(key)
				Expect(mustGetQueueItem()).To(Equal(key))
				for i := 0; i < freshThreshold; i++ {
					retry(key)
					Expect(mustGetQueueItem()).To(Equal(key))
				}

				// retry the item one more time, this time it should go to the staled queue
				retry(key)

				// make sure the item has met the time condition for dequeue
				fakeNow = fakeNow.Add(slowDelay)
				testC.SetTime(fakeNow)
				time.Sleep(slowDelay)

				receiveCh := make(chan interface{})
				go func() {
					item, shutdown := queue.GetFresh()
					if shutdown {
						close(receiveCh)
					}
					receiveCh <- item
				}()
				Consistently(receiveCh).Should(BeEmpty())
			})

			It("can prioritize the fresh queue", func() {
				queue.Add(keyA)
				Expect(mustGetQueueItem()).To(Equal(keyA))

				for i := 0; i < freshThreshold; i++ {
					// before reaching the fresh threshold, both keys should go to the fresh queue; since
					// keyA is retried before keyB is added, keyA should be dequeued first
					retry(keyA)
					queue.Add(keyB)
					Expect(mustGetQueueItem()).To(Equal(keyA))
					Expect(mustGetQueueItem()).To(Equal(keyB))
					queue.Done(keyB)
				}

				// after reaching the fresh threshold, keyA should go to the staled queue; keyB should be
				// dequeued first
				retry(keyA)
				queue.Add(keyB)
				Expect(mustGetQueueItem()).To(Equal(keyB))
				Expect(mustGetQueueItem()).To(Equal(keyA))
			})
		})
	})
})

func spawnGetChan(queue PlayboyInterface) chan interface{} {
	getChan := make(chan interface{})
	go func() {
		for {
			item, shutdown := queue.Get()
			if shutdown {
				break
			}
			getChan <- item
		}
		close(getChan)
	}()
	return getChan
}
