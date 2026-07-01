package interp

import "sync"

// eventLoop is a minimal single-threaded task scheduler modeled on the
// JavaScript job/task model. Two queues are serviced:
//
//   - microtasks: promise reactions; drained completely after every task and
//     before the loop blocks.
//   - macrotasks: timer callbacks (setTimeout/setInterval) that have fired.
//
// All queued functions execute on the goroutine that calls run, so user script
// code — the initial program plus every callback — runs single-threaded. Timer
// goroutines (owned by a TimerProvider) only enqueue work; they never touch
// interpreter state directly.
type eventLoop struct {
	mu    sync.Mutex
	cond  *sync.Cond
	micro []func() error
	macro []func() error

	// activeTimers counts scheduled one-shot/interval callbacks that have not
	// completed, so run knows whether to keep waiting for more work.
	activeTimers int
	stopped      bool
}

// newEventLoop returns a ready event loop.
func newEventLoop() *eventLoop {
	l := &eventLoop{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// pushMicro enqueues a microtask (promise job).
func (l *eventLoop) pushMicro(fn func() error) {
	l.mu.Lock()
	l.micro = append(l.micro, fn)
	l.cond.Signal()
	l.mu.Unlock()
}

// pushMacro enqueues a macrotask (a fired timer callback).
func (l *eventLoop) pushMacro(fn func() error) {
	l.mu.Lock()
	l.macro = append(l.macro, fn)
	l.cond.Signal()
	l.mu.Unlock()
}

// addTimer/removeTimer track outstanding timers so the loop keeps running while
// timers are pending even if both queues are momentarily empty.
func (l *eventLoop) addTimer() {
	l.mu.Lock()
	l.activeTimers++
	l.mu.Unlock()
}

func (l *eventLoop) removeTimer() {
	l.mu.Lock()
	if l.activeTimers > 0 {
		l.activeTimers--
	}
	l.cond.Signal()
	l.mu.Unlock()
}

// stop wakes the loop and causes run to return promptly.
func (l *eventLoop) stop() {
	l.mu.Lock()
	l.stopped = true
	l.cond.Broadcast()
	l.mu.Unlock()
}

// drainMicro runs all queued microtasks, returning the first error.
func (l *eventLoop) drainMicro() error {
	for {
		l.mu.Lock()
		if len(l.micro) == 0 {
			l.mu.Unlock()
			return nil
		}
		fn := l.micro[0]
		l.micro = l.micro[1:]
		l.mu.Unlock()
		if err := fn(); err != nil {
			return err
		}
	}
}

// run pumps the loop until there is no more work (no queued tasks and no
// pending timers) or until stop is called. It returns the first error raised by
// a task.
func (l *eventLoop) run() error {
	for {
		if err := l.drainMicro(); err != nil {
			return err
		}
		l.mu.Lock()
		for len(l.macro) == 0 && !l.stopped {
			// Nothing runnable right now. If no timers are pending either, the
			// loop is genuinely idle and we are done.
			if l.activeTimers == 0 && len(l.micro) == 0 {
				l.mu.Unlock()
				return nil
			}
			l.cond.Wait()
		}
		if l.stopped {
			l.mu.Unlock()
			return nil
		}
		fn := l.macro[0]
		l.macro = l.macro[1:]
		l.mu.Unlock()

		if err := fn(); err != nil {
			return err
		}
	}
}
