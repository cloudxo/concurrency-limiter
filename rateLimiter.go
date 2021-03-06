package limiter

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// waiter is the individual goroutine waiting for accessing the resource.
// waiter waits for the signal through the done channel.
type waiter struct {
	done chan struct{}
}

// limit: max number of concurrent goroutines that can access aresource
//
// count: current number of goroutines accessing a resource
//
// waitList: list of goroutines waiting to access a resource. Goroutines will be added to
// this list if the number of concurrent requests are greater than the limit specified
//
// timeout: If this field is specified , goroutines will be automatically removed from the waitlist
// after the time passes the timeout specified even if the number of concurrent requests is greater than the limit. (in ms)
type Limiter struct {
	count    int
	limit    int
	mu       sync.Mutex
	waitList list.List
	timeout  *int
}

type Option func(*Limiter)

// New creates an instance of *Limiter. Configure the Limiter with the options specified.
// Example: limiter.New(4, WithTimeout(5))
func New(limit int, options ...Option) *Limiter {
	l := &Limiter{
		limit: limit,
	}

	for _, o := range options {
		o(l)
	}
	return l
}

// timeout: If this field is specified , goroutines will be automatically removed from the waitlist
// after the time passes the timeout specified even if the number of concurrent requests is greater than the limit.
func WithTimeout(timeout int) func(*Limiter) {
	return func(l *Limiter) {
		l.timeout = &timeout
	}
}

// Wait method waits if the number of concurrent requests is more than the limit specified.
// If a timeout is configured , then the goroutine will wait until the timeout occurs and then proceeds to
// access the resource irrespective of whether it has received a signal in the done channel.
func (l *Limiter) Wait(ctx context.Context) {
	ok, ch := l.proceed()
	if ok {
		return
	}
	if l.timeout != nil {
		select {
		case <-ch:
		case <-time.After((time.Duration(*l.timeout) * time.Millisecond)):
			l.removeWaiter(ch)
		case <-ctx.Done():
		}
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
		l.removeWaiter(ch)
	}
}

func (l *Limiter) removeWaiter(ch chan struct{}) {
	l.mu.Lock()
	for w := l.waitList.Front(); w != nil; w = w.Next() {
		ele := w.Value.(waiter)
		if ele.done == ch {
			close(ch)
			l.waitList.Remove(w)
			l.count += 1
			break
		}
	}
	l.mu.Unlock()
}

// proceed will return true if the number of concurrent requests is less than the limit else it
// will add the goroutine to the waiting list and will return a channel. This channel is used by goutines to
// check for signal when they are granted access to use the resource.
func (l *Limiter) proceed() (bool, chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.count < l.limit {
		l.count++
		return true, nil
	}
	ch := make(chan struct{})
	w := waiter{
		done: ch,
	}
	l.waitList.PushBack(w)
	return false, ch
}

// Finish will remove the goroutine from the waiting list and sends a signal
// to the waiting goroutine to access the resource
func (l *Limiter) Finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count -= 1
	first := l.waitList.Front()
	if first == nil {
		return
	}
	w := l.waitList.Remove(first).(waiter)
	w.done <- struct{}{}
	close(w.done)
}

// only used in tests
func (l *Limiter) waitListSize() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	len := l.waitList.Len()
	return len
}
