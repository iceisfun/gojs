package interp

import (
	"context"
	"time"
)

// initTimers installs setTimeout/setInterval/clearTimeout/clearInterval,
// setImmediate, and queueMicrotask. They require a TimerProvider; without one
// the schedulers are absent, so a script cannot keep the event loop alive.
//
// Timer callbacks are delivered to the interpreter's event loop and executed on
// its goroutine, so callbacks never race with the main script. Cancelling the
// interpreter's context (via Close) stops all outstanding timers.
func (i *Interpreter) initTimers() {
	// queueMicrotask works without a TimerProvider (it is not a host timer).
	i.setGlobalFunc("queueMicrotask", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cb, ok := arg(args, 0).(*Object)
		if !ok || !cb.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "queueMicrotask argument is not a function")
		}
		i.loop.pushMicro(func() error {
			_, err := cb.fn.call(i.ctx, Undef, nil)
			return err
		})
		return Undef, nil
	})

	if i.timer == nil {
		return
	}

	timers := &timerRegistry{cancels: map[int64]func(){}}

	schedule := func(repeat bool) CallFn {
		return func(ctx context.Context, this Value, args []Value) (Value, error) {
			cb, ok := arg(args, 0).(*Object)
			if !ok || !cb.IsCallable() {
				return Number(0), nil
			}
			delayMs, _ := i.argNum(ctx, args, 1)
			if delayMs < 0 || delayMs != delayMs { // clamp NaN/negative to 0
				delayMs = 0
			}
			extra := append([]Value{}, args[min(2, len(args)):]...)
			id := timers.nextID()

			var fire func()
			fire = func() {
				// Runs in the timer goroutine: enqueue the JS callback for the
				// event loop rather than calling it here.
				i.loop.pushMacro(func() error {
					_, err := cb.fn.call(i.ctx, Undef, extra)
					if repeat {
						// Reschedule the interval.
						cancel := i.timer.AfterFunc(i.ctx, time.Duration(delayMs)*time.Millisecond, fire)
						timers.set(id, cancel)
					} else {
						timers.clear(id)
						i.loop.removeTimer()
					}
					return err
				})
			}
			i.loop.addTimer()
			cancel := i.timer.AfterFunc(i.ctx, time.Duration(delayMs)*time.Millisecond, fire)
			timers.set(id, cancel)
			return Number(float64(id)), nil
		}
	}

	clear := func(ctx context.Context, this Value, args []Value) (Value, error) {
		id := int64(ToInteger(ToNumber(arg(args, 0))))
		if timers.clear(id) {
			i.loop.removeTimer()
		}
		return Undef, nil
	}

	i.setGlobalFunc("setTimeout", 2, schedule(false))
	i.setGlobalFunc("setInterval", 2, schedule(true))
	i.setGlobalFunc("clearTimeout", 1, clear)
	i.setGlobalFunc("clearInterval", 1, clear)
	i.setGlobalFunc("setImmediate", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		// setImmediate is setTimeout with a zero delay for our purposes.
		return schedule(false)(ctx, this, append([]Value{arg(args, 0), Number(0)}, args[min(1, len(args)):]...))
	})
}

// timerRegistry tracks live timer cancel functions by id.
type timerRegistry struct {
	cancels map[int64]func()
	counter int64
}

func (r *timerRegistry) nextID() int64 {
	r.counter++
	return r.counter
}

func (r *timerRegistry) set(id int64, cancel func()) {
	r.cancels[id] = cancel
}

// clear cancels the timer with the given id, returning whether it existed.
func (r *timerRegistry) clear(id int64) bool {
	if cancel, ok := r.cancels[id]; ok {
		cancel()
		delete(r.cancels, id)
		return true
	}
	return false
}
