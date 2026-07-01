package interp

// builtin_date.go — ECMA-262 §20.4 (Date Objects)
//
// Date objects store a single internal [[DateValue]] slot as a Number
// (float64) of milliseconds since the Unix epoch (1 January 1970 00:00:00
// UTC). The sentinel value NaN represents an invalid date.
//
// # Timezone note
//
// This implementation operates entirely in UTC. The local-time accessor
// variants (getFullYear, getMonth, …) are aliases for their getUTC*
// counterparts. getTimezoneOffset() always returns 0. This is documented
// per method.
//
// # Clock gate
//
// All calls to "current time" go through i.dateNow, which consults the
// optional i.clock TimeProvider. When the provider is nil, "now" is
// permanently 0 ms (the epoch), keeping the sandbox deterministic.

import (
	"context"
	"fmt"
	"math"
	"time"
)

// ---------------------------------------------------------------------------
// dateNow — clock-gated "current time in milliseconds" (ECMA-262 §20.4.4.1)
// ---------------------------------------------------------------------------

// dateNow returns the current time as milliseconds since the Unix epoch. It
// consults i.clock when the TimeProvider is non-nil; when nil, it returns 0
// (the Unix epoch) so the sandbox stays deterministic without a real clock.
func (i *Interpreter) dateNow(ctx context.Context) float64 {
	if i.clock != nil {
		return float64(i.clock.Now(ctx).UnixMilli())
	}
	return 0
}

// ---------------------------------------------------------------------------
// utcTime — milliseconds → UTC time.Time
// ---------------------------------------------------------------------------

// utcTime converts a [[DateValue]] millisecond timestamp to a UTC time.Time
// for field extraction. NaN and ±Inf produce the zero value of time.Time;
// callers must guard against NaN before calling this.
func utcTime(ms float64) time.Time {
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return time.Time{}
	}
	return time.UnixMilli(int64(ms)).UTC()
}

// ---------------------------------------------------------------------------
// setDateMs — mutate a Date object's [[DateValue]]
// ---------------------------------------------------------------------------

// setDateMs stores ms into the Date object's primitive slot and returns ms as
// a Number, matching the return-value convention of all set* methods.
func setDateMs(obj *Object, ms float64) Value {
	obj.primitive = Number(ms)
	return Number(ms)
}

// ---------------------------------------------------------------------------
// dateParseStr — string → milliseconds (ECMA-262 §20.4.1.15)
// ---------------------------------------------------------------------------

// dateParseStr parses s as an ISO 8601 / RFC 3339 date string, returning the
// corresponding millisecond timestamp or NaN on failure.
//
// Layouts tried in order:
//  1. RFC 3339 with sub-second precision — "2006-01-02T15:04:05.999999999Z07:00"
//  2. RFC 3339                           — "2006-01-02T15:04:05Z07:00"
//  3. Date-only UTC                      — "2006-01-02"
func dateParseStr(s string) float64 {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return float64(t.UnixMilli())
		}
	}
	return math.NaN()
}

// ---------------------------------------------------------------------------
// dateConstruct — multi-arg construction (ECMA-262 §20.4.1.15 MakeDate)
// ---------------------------------------------------------------------------

// dateConstruct builds a [[DateValue]] from two or more arguments in the form
// Date(year, month[, day[, hours[, minutes[, seconds[, ms]]]]]).  month is
// 0-based (0 = January). If year is an integer in [0, 99] it is promoted to
// 1900+year per §20.4.1.15. All fields default to 0/1 when omitted.
func dateConstruct(year, month float64, rest ...float64) float64 {
	yr := int(year)
	// Promote two-digit years (§20.4.1.15): if ToInteger(y) ∈ [0, 99], use 1900+y.
	if !math.IsNaN(year) && float64(yr) == year && yr >= 0 && yr <= 99 {
		yr += 1900
	}

	// month is 0-based; Go's time.Month is 1-based.
	mo := int(month) + 1
	day := 1
	hours, minutes, seconds, millis := 0, 0, 0, 0

	if len(rest) > 0 {
		day = int(rest[0])
	}
	if len(rest) > 1 {
		hours = int(rest[1])
	}
	if len(rest) > 2 {
		minutes = int(rest[2])
	}
	if len(rest) > 3 {
		seconds = int(rest[3])
	}
	if len(rest) > 4 {
		millis = int(rest[4])
	}

	t := time.Date(yr, time.Month(mo), day, hours, minutes, seconds,
		millis*int(time.Millisecond), time.UTC)
	return float64(t.UnixMilli())
}

// ---------------------------------------------------------------------------
// dateToISO — toISOString implementation (ECMA-262 §20.4.4.36)
// ---------------------------------------------------------------------------

// dateToISO formats ms as an ISO 8601 / UTC string with millisecond
// precision, e.g. "2009-11-10T23:00:00.000Z". Years outside [0000, 9999] are
// not zero-padded to six digits in this implementation.
func dateToISO(ms float64) string {
	t := utcTime(ms)
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d.%03dZ",
		t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), t.Second(),
		t.Nanosecond()/1_000_000,
	)
}

// ---------------------------------------------------------------------------
// initDate — entry point
// ---------------------------------------------------------------------------

// initDate installs the Date constructor and Date.prototype on the global
// object. It is called once from bootstrap.
//
// ECMA-262 §20.4
func (i *Interpreter) initDate() {
	proto := i.dateProto
	proto.class = "Date"
	// Date.prototype itself has [[DateValue]] = NaN (§20.4.4).
	proto.primitive = Number(math.NaN())

	// requireDate asserts that this is a Date object and returns its
	// [[DateValue]] and the object. Returns a TypeError when this is not a
	// Date. When the [[DateValue]] is absent (which should not happen for
	// normally constructed dates) NaN is returned.
	requireDate := func(ctx context.Context, this Value, method string) (float64, *Object, error) {
		obj, ok := this.(*Object)
		if !ok || obj.class != "Date" {
			return 0, nil, i.throwError(ctx, "TypeError",
				"Date.prototype."+method+" called on incompatible receiver")
		}
		n, ok := obj.primitive.(Number)
		if !ok {
			return math.NaN(), obj, nil
		}
		return float64(n), obj, nil
	}

	// -------------------------------------------------------------------------
	// Date.prototype — getTime / valueOf (§20.4.4.4 / §20.4.4.38)
	// -------------------------------------------------------------------------

	getTimeFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getTime")
		if err != nil {
			return nil, err
		}
		return Number(ms), nil
	}
	i.defineMethod(proto, "getTime", 0, getTimeFn)
	i.defineMethod(proto, "valueOf", 0, getTimeFn)

	// -------------------------------------------------------------------------
	// Date.prototype — year getters (§20.4.4.4 / §20.4.4.14)
	// Note: getFullYear is implemented as UTC (see file-level timezone note).
	// -------------------------------------------------------------------------

	// getFullYear — §20.4.4.4
	i.defineMethod(proto, "getFullYear", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getFullYear")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Year())), nil
	})

	// getUTCFullYear — §20.4.4.14
	i.defineMethod(proto, "getUTCFullYear", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCFullYear")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Year())), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — month getters (§20.4.4.8 / §20.4.4.18, 0-based)
	// -------------------------------------------------------------------------

	// getMonth — §20.4.4.8
	i.defineMethod(proto, "getMonth", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getMonth")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Month() - 1)), nil
	})

	// getUTCMonth — §20.4.4.18
	i.defineMethod(proto, "getUTCMonth", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCMonth")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Month() - 1)), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — day-of-month getters (§20.4.4.2 / §20.4.4.12, 1-based)
	// -------------------------------------------------------------------------

	// getDate — §20.4.4.2
	i.defineMethod(proto, "getDate", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getDate")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Day())), nil
	})

	// getUTCDate — §20.4.4.12
	i.defineMethod(proto, "getUTCDate", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCDate")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Day())), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — day-of-week getters (§20.4.4.3 / §20.4.4.13, 0=Sunday)
	// -------------------------------------------------------------------------

	// getDay — §20.4.4.3
	i.defineMethod(proto, "getDay", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getDay")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Weekday())), nil
	})

	// getUTCDay — §20.4.4.13
	i.defineMethod(proto, "getUTCDay", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCDay")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Weekday())), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — hours getters (§20.4.4.5 / §20.4.4.15)
	// -------------------------------------------------------------------------

	// getHours — §20.4.4.5
	i.defineMethod(proto, "getHours", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getHours")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Hour())), nil
	})

	// getUTCHours — §20.4.4.15
	i.defineMethod(proto, "getUTCHours", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCHours")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Hour())), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — minutes getters (§20.4.4.9 / §20.4.4.19)
	// -------------------------------------------------------------------------

	// getMinutes — §20.4.4.9
	i.defineMethod(proto, "getMinutes", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getMinutes")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Minute())), nil
	})

	// getUTCMinutes — §20.4.4.19
	i.defineMethod(proto, "getUTCMinutes", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCMinutes")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Minute())), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — seconds getters (§20.4.4.10 / §20.4.4.20)
	// -------------------------------------------------------------------------

	// getSeconds — §20.4.4.10
	i.defineMethod(proto, "getSeconds", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getSeconds")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Second())), nil
	})

	// getUTCSeconds — §20.4.4.20
	i.defineMethod(proto, "getUTCSeconds", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCSeconds")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Second())), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — milliseconds getters (§20.4.4.6 / §20.4.4.16)
	// -------------------------------------------------------------------------

	// getMilliseconds — §20.4.4.6
	i.defineMethod(proto, "getMilliseconds", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getMilliseconds")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Nanosecond() / 1_000_000)), nil
	})

	// getUTCMilliseconds — §20.4.4.16
	i.defineMethod(proto, "getUTCMilliseconds", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getUTCMilliseconds")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(float64(utcTime(ms).Nanosecond() / 1_000_000)), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — timezone (§20.4.4.11)
	// -------------------------------------------------------------------------

	// getTimezoneOffset — §20.4.4.11. Always returns 0: this runtime operates
	// in UTC (see file-level timezone note).
	i.defineMethod(proto, "getTimezoneOffset", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		_, _, err := requireDate(ctx, this, "getTimezoneOffset")
		if err != nil {
			return nil, err
		}
		return Number(0), nil
	})

	// -------------------------------------------------------------------------
	// Date.prototype — setter methods
	// -------------------------------------------------------------------------

	// setTime — §20.4.4.27
	i.defineMethod(proto, "setTime", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		_, obj, err := requireDate(ctx, this, "setTime")
		if err != nil {
			return nil, err
		}
		v, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return setDateMs(obj, v), nil
	})

	// setFullYear — §20.4.4.22  (year[, month[, date]])
	// Note: does NOT apply the 0-99 promotion; sets the exact Gregorian year.
	i.defineMethod(proto, "setFullYear", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setFullYear")
		if err != nil {
			return nil, err
		}
		yr, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		mo, day := t.Month(), t.Day()
		if len(args) > 1 {
			m, err := i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
			mo = time.Month(int(m) + 1)
		}
		if len(args) > 2 {
			d, err := i.argNum(ctx, args, 2)
			if err != nil {
				return nil, err
			}
			day = int(d)
		}
		nt := time.Date(int(yr), mo, day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// setMonth — §20.4.4.26  (month[, date])
	i.defineMethod(proto, "setMonth", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setMonth")
		if err != nil {
			return nil, err
		}
		m, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		day := t.Day()
		if len(args) > 1 {
			d, err := i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
			day = int(d)
		}
		nt := time.Date(t.Year(), time.Month(int(m)+1), day,
			t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// setDate — §20.4.4.21  (day-of-month)
	i.defineMethod(proto, "setDate", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setDate")
		if err != nil {
			return nil, err
		}
		d, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		nt := time.Date(t.Year(), t.Month(), int(d),
			t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// setHours — §20.4.4.23  (hours[, min[, sec[, ms]]])
	i.defineMethod(proto, "setHours", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setHours")
		if err != nil {
			return nil, err
		}
		h, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		min, sec, nsec := t.Minute(), t.Second(), t.Nanosecond()
		if len(args) > 1 {
			mn, err := i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
			min = int(mn)
		}
		if len(args) > 2 {
			s, err := i.argNum(ctx, args, 2)
			if err != nil {
				return nil, err
			}
			sec = int(s)
		}
		if len(args) > 3 {
			millis, err := i.argNum(ctx, args, 3)
			if err != nil {
				return nil, err
			}
			nsec = int(millis) * int(time.Millisecond)
		}
		nt := time.Date(t.Year(), t.Month(), t.Day(), int(h), min, sec, nsec, time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// setMinutes — §20.4.4.24  (min[, sec[, ms]])
	i.defineMethod(proto, "setMinutes", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setMinutes")
		if err != nil {
			return nil, err
		}
		mn, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		sec, nsec := t.Second(), t.Nanosecond()
		if len(args) > 1 {
			s, err := i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
			sec = int(s)
		}
		if len(args) > 2 {
			millis, err := i.argNum(ctx, args, 2)
			if err != nil {
				return nil, err
			}
			nsec = int(millis) * int(time.Millisecond)
		}
		nt := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), int(mn), sec, nsec, time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// setSeconds — §20.4.4.28  (sec[, ms])
	i.defineMethod(proto, "setSeconds", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setSeconds")
		if err != nil {
			return nil, err
		}
		s, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		nsec := t.Nanosecond()
		if len(args) > 1 {
			millis, err := i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
			nsec = int(millis) * int(time.Millisecond)
		}
		nt := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), int(s), nsec, time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// setMilliseconds — §20.4.4.25
	i.defineMethod(proto, "setMilliseconds", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cur, obj, err := requireDate(ctx, this, "setMilliseconds")
		if err != nil {
			return nil, err
		}
		millis, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		t := utcTime(cur)
		nt := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(),
			int(millis)*int(time.Millisecond), time.UTC)
		return setDateMs(obj, float64(nt.UnixMilli())), nil
	})

	// The setUTC* setters alias their local counterparts: gojs Date operates
	// entirely in UTC, so the local and UTC mutators are identical.
	for local, utc := range map[string]string{
		"setFullYear":     "setUTCFullYear",
		"setMonth":        "setUTCMonth",
		"setDate":         "setUTCDate",
		"setHours":        "setUTCHours",
		"setMinutes":      "setUTCMinutes",
		"setSeconds":      "setUTCSeconds",
		"setMilliseconds": "setUTCMilliseconds",
	} {
		if fn, ok := proto.props[StrKey(local)]; ok && fn.Value != nil {
			proto.SetHidden(utc, fn.Value)
		}
	}

	// -------------------------------------------------------------------------
	// Date.prototype — string conversion methods
	// -------------------------------------------------------------------------

	// toISOString — §20.4.4.36
	// Throws RangeError when the [[DateValue]] is NaN.
	i.defineMethod(proto, "toISOString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "toISOString")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return nil, i.throwError(ctx, "RangeError", "Invalid time value")
		}
		return String(dateToISO(ms)), nil
	})

	// toString — §20.4.4.35
	i.defineMethod(proto, "toString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "toString")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return String("Invalid Date"), nil
		}
		t := utcTime(ms)
		return String(t.Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)")), nil
	})

	// toDateString — §20.4.4.31
	i.defineMethod(proto, "toDateString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "toDateString")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return String("Invalid Date"), nil
		}
		t := utcTime(ms)
		return String(t.Format("Mon Jan 02 2006")), nil
	})

	// toTimeString — §20.4.4.34
	i.defineMethod(proto, "toTimeString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "toTimeString")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return String("Invalid Date"), nil
		}
		t := utcTime(ms)
		return String(t.Format("15:04:05 GMT+0000 (Coordinated Universal Time)")), nil
	})

	// toJSON — §20.4.4.37  (used by JSON.stringify; returns null for invalid dates)
	i.defineMethod(proto, "toJSON", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "toJSON")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Nul, nil
		}
		return String(dateToISO(ms)), nil
	})

	// -------------------------------------------------------------------------
	// Date constructor (§20.4.2)
	// -------------------------------------------------------------------------

	// newDateObj allocates a fresh Date instance with the given [[DateValue]].
	newDateObj := func(ms float64) *Object {
		o := NewObject(i.dateProto)
		o.class = "Date"
		o.primitive = Number(ms)
		return o
	}

	// callFn — Date() called without new (§20.4.2.1): returns the current time
	// as a human-readable string, regardless of any arguments.
	callFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms := i.dateNow(ctx)
		t := utcTime(ms)
		return String(t.Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)")), nil
	}

	// constructFn — new Date(...) (§20.4.2.2)
	//
	//   new Date()                → current time
	//   new Date(value)           → from number (ms) or string (parsed)
	//   new Date(year,month,...)  → components in UTC; month is 0-based
	constructFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		var ms float64
		switch len(args) {
		case 0:
			// new Date() — current time via the clock provider.
			ms = i.dateNow(ctx)

		case 1:
			// new Date(value) — number or string.
			v := arg(args, 0)
			if s, ok := v.(String); ok {
				ms = dateParseStr(string(s))
			} else {
				f, err := i.ToNumberV(ctx, v)
				if err != nil {
					return nil, err
				}
				ms = f
			}

		default:
			// new Date(year, month[, day[, hours[, minutes[, seconds[, ms]]]]]).
			yr, err := i.argNum(ctx, args, 0)
			if err != nil {
				return nil, err
			}
			mo, err := i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
			var rest []float64
			for n := 2; n < len(args); n++ {
				f, err := i.argNum(ctx, args, n)
				if err != nil {
					return nil, err
				}
				rest = append(rest, f)
			}
			ms = dateConstruct(yr, mo, rest...)
		}
		return newDateObj(ms), nil
	}

	ctor := i.newNativeCtor("Date", 7, callFn, constructFn)
	linkCtor(ctor, proto)

	// -------------------------------------------------------------------------
	// Date static methods
	// -------------------------------------------------------------------------

	// Date.now — §20.4.3.1
	i.defineMethod(ctor, "now", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Number(i.dateNow(ctx)), nil
	})

	// Date.parse — §20.4.3.2
	i.defineMethod(ctor, "parse", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(dateParseStr(s)), nil
	})

	// Date.UTC — §20.4.3.4  (year, month[, day[, hours[, minutes[, seconds[, ms]]]]])
	// Returns the UTC millisecond timestamp for the given components.
	i.defineMethod(ctor, "UTC", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if len(args) < 2 {
			return Number(math.NaN()), nil
		}
		yr, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		mo, err := i.argNum(ctx, args, 1)
		if err != nil {
			return nil, err
		}
		var rest []float64
		for n := 2; n < len(args); n++ {
			f, err := i.argNum(ctx, args, n)
			if err != nil {
				return nil, err
			}
			rest = append(rest, f)
		}
		return Number(dateConstruct(yr, mo, rest...)), nil
	})

	i.setGlobalHidden("Date", ctor)
}
