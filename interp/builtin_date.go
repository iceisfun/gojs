package interp

// builtin_date.go — ECMA-262 §21.4 (Date Objects)
//
// Date objects store a single internal [[DateValue]] slot as a Number
// (float64) of milliseconds since the Unix epoch (1 January 1970 00:00:00
// UTC). The sentinel value NaN represents an invalid date.
//
// # Timezone note
//
// This implementation operates entirely in UTC. LocalTime(t) == t and
// UTC(t) == t, so the local-time accessor variants (getFullYear, getMonth, …)
// are aliases for their getUTC* counterparts and getTimezoneOffset() returns 0
// for valid dates (NaN for invalid ones). This keeps behaviour deterministic
// and independent of the host timezone.
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
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Time-value constants (ECMA-262 §21.4.1)
// ---------------------------------------------------------------------------

const (
	msPerSecond  = 1000.0
	msPerMinute  = 60000.0
	msPerHour    = 3600000.0
	msPerDay     = 86400000.0
	maxTimeValue = 8.64e15 // 100,000,000 days either side of the epoch
)

var weekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var monthNames = [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// dtFinite reports whether x is neither NaN nor an infinity.
func dtFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// floorMod implements ECMAScript's modulo (result has the sign of the divisor).
func floorMod(a, b float64) float64 { return a - b*math.Floor(a/b) }

// ---------------------------------------------------------------------------
// Abstract time operations (ECMA-262 §21.4.1.3 – §21.4.1.11)
// ---------------------------------------------------------------------------

func dayNumber(t float64) float64     { return math.Floor(t / msPerDay) }
func timeWithinDay(t float64) float64 { return floorMod(t, msPerDay) }

func daysInYear(y float64) float64 {
	switch {
	case math.Mod(y, 4) != 0:
		return 365
	case math.Mod(y, 100) != 0:
		return 366
	case math.Mod(y, 400) != 0:
		return 365
	default:
		return 366
	}
}

func dayFromYear(y float64) float64 {
	return 365*(y-1970) + math.Floor((y-1969)/4) - math.Floor((y-1901)/100) + math.Floor((y-1601)/400)
}

func timeFromYear(y float64) float64 { return msPerDay * dayFromYear(y) }

func yearFromTime(t float64) float64 {
	y := math.Floor(t/(msPerDay*365.2425)) + 1970
	for timeFromYear(y) > t {
		y--
	}
	for timeFromYear(y+1) <= t {
		y++
	}
	return y
}

func inLeapYear(t float64) bool { return daysInYear(yearFromTime(t)) == 366 }

func dayWithinYear(t float64) float64 { return dayNumber(t) - dayFromYear(yearFromTime(t)) }

// monthAndDate returns the 0-based month and 1-based day of month for t.
func monthAndDate(t float64) (int, int) {
	d := int(dayWithinYear(t))
	feb := 28
	if inLeapYear(t) {
		feb = 29
	}
	months := [12]int{31, feb, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	m := 0
	for m < 12 && d >= months[m] {
		d -= months[m]
		m++
	}
	if m > 11 {
		m = 11
	}
	return m, d + 1
}

func monthFromTime(t float64) int { m, _ := monthAndDate(t); return m }
func dateFromTime(t float64) int  { _, d := monthAndDate(t); return d }

func weekDay(t float64) int      { return int(floorMod(dayNumber(t)+4, 7)) }
func hourFromTime(t float64) int { return int(floorMod(math.Floor(t/msPerHour), 24)) }
func minFromTime(t float64) int  { return int(floorMod(math.Floor(t/msPerMinute), 60)) }
func secFromTime(t float64) int  { return int(floorMod(math.Floor(t/msPerSecond), 60)) }
func msFromTime(t float64) int   { return int(floorMod(t, 1000)) }

// ---------------------------------------------------------------------------
// MakeTime / MakeDay / MakeDate / TimeClip (ECMA-262 §21.4.1.12 – §21.4.1.15)
// ---------------------------------------------------------------------------

func makeTime(hour, min, sec, ms float64) float64 {
	if !dtFinite(hour) || !dtFinite(min) || !dtFinite(sec) || !dtFinite(ms) {
		return math.NaN()
	}
	h := math.Trunc(hour)
	m := math.Trunc(min)
	s := math.Trunc(sec)
	milli := math.Trunc(ms)
	return ((h*msPerHour + m*msPerMinute) + s*msPerSecond) + milli
}

func makeDay(year, month, date float64) float64 {
	if !dtFinite(year) || !dtFinite(month) || !dtFinite(date) {
		return math.NaN()
	}
	y := math.Trunc(year)
	m := math.Trunc(month)
	dt := math.Trunc(date)
	ym := y + math.Floor(m/12)
	if !dtFinite(ym) {
		return math.NaN()
	}
	mn := int(floorMod(m, 12))
	feb := 28.0
	if daysInYear(ym) == 366 {
		feb = 29
	}
	monthDays := [12]float64{31, feb, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	days := dayFromYear(ym)
	for i := 0; i < mn; i++ {
		days += monthDays[i]
	}
	return days + dt - 1
}

func makeDate(day, time float64) float64 {
	if !dtFinite(day) || !dtFinite(time) {
		return math.NaN()
	}
	return day*msPerDay + time
}

func timeClip(t float64) float64 {
	if !dtFinite(t) {
		return math.NaN()
	}
	if math.Abs(t) > maxTimeValue {
		return math.NaN()
	}
	// ToIntegerOrInfinity truncates; adding +0 normalises -0 to +0.
	return math.Trunc(t) + 0
}

// yearPromote applies the two-digit year offset used by the multi-argument
// Date constructor and Date.UTC: ToInteger(y) ∈ [0, 99] ⇒ 1900 + ToInteger(y).
func yearPromote(y float64) float64 {
	if math.IsNaN(y) {
		return y
	}
	yi := math.Trunc(y)
	if yi >= 0 && yi <= 99 {
		return 1900 + yi
	}
	return y
}

// ---------------------------------------------------------------------------
// String formatting (ECMA-262 §21.4.4.41.1 – §21.4.4.41.4)
// ---------------------------------------------------------------------------

// yearStr formats a signed year zero-padded to at least four digits.
func yearStr(y int) string {
	sign := ""
	if y < 0 {
		sign = "-"
		y = -y
	}
	return fmt.Sprintf("%s%04d", sign, y)
}

func dateString(t float64) string {
	m, d := monthAndDate(t)
	return fmt.Sprintf("%s %s %02d %s",
		weekdayNames[weekDay(t)], monthNames[m], d, yearStr(int(yearFromTime(t))))
}

func timeString(t float64) string {
	return fmt.Sprintf("%02d:%02d:%02d GMT", hourFromTime(t), minFromTime(t), secFromTime(t))
}

// timeZoneString is "+0000 (Coordinated Universal Time)" in this UTC runtime.
func timeZoneString() string { return "+0000 (Coordinated Universal Time)" }

func dateToString(t float64) string {
	return dateString(t) + " " + timeString(t) + timeZoneString()
}

func dateToUTCString(t float64) string {
	m, d := monthAndDate(t)
	return fmt.Sprintf("%s, %02d %s %s %s",
		weekdayNames[weekDay(t)], d, monthNames[m], yearStr(int(yearFromTime(t))), timeString(t))
}

// dateToISO formats t as an ISO 8601 / UTC string with millisecond precision.
// Years in [0, 9999] use four digits; others use a signed six-digit form.
func dateToISO(t float64) string {
	y := int(yearFromTime(t))
	var ys string
	if y >= 0 && y <= 9999 {
		ys = fmt.Sprintf("%04d", y)
	} else {
		sign := "+"
		ay := y
		if y < 0 {
			sign = "-"
			ay = -y
		}
		ys = fmt.Sprintf("%s%06d", sign, ay)
	}
	m, d := monthAndDate(t)
	return fmt.Sprintf("%s-%02d-%02dT%02d:%02d:%02d.%03dZ",
		ys, m+1, d, hourFromTime(t), minFromTime(t), secFromTime(t), msFromTime(t))
}

// ---------------------------------------------------------------------------
// Date parsing (ECMA-262 §21.4.3.2 / §21.4.1.15)
// ---------------------------------------------------------------------------

func allDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func atoiField(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// parseDate parses a string as a date, returning a time value or NaN.
func parseDate(s string) float64 {
	if ms, ok := parseISO(s); ok {
		return ms
	}
	if ms, ok := parseLegacy(s); ok {
		return ms
	}
	return math.NaN()
}

// parseISO parses the Date Time String Format of §21.4.1.15, including the
// expanded (signed six-digit) year form.
func parseISO(s string) (float64, bool) {
	i, n := 0, len(s)

	neg, extended := false, false
	if i < n && (s[i] == '+' || s[i] == '-') {
		extended = true
		neg = s[i] == '-'
		i++
	}
	yDigits := 4
	if extended {
		yDigits = 6
	}
	if i+yDigits > n || !allDigits(s[i:i+yDigits]) {
		return 0, false
	}
	year := atoiField(s[i : i+yDigits])
	if neg {
		if year == 0 {
			return 0, false // "-000000" is an invalid extended year
		}
		year = -year
	}
	i += yDigits

	month, day := 1, 1
	if i < n && s[i] == '-' {
		i++
		if i+2 > n || !allDigits(s[i:i+2]) {
			return 0, false
		}
		month = atoiField(s[i : i+2])
		i += 2
		if month < 1 || month > 12 {
			return 0, false
		}
		if i < n && s[i] == '-' {
			i++
			if i+2 > n || !allDigits(s[i:i+2]) {
				return 0, false
			}
			day = atoiField(s[i : i+2])
			i += 2
			if day < 1 || day > 31 {
				return 0, false
			}
		}
	}

	hasTime := false
	hour, minute, sec, ms := 0, 0, 0, 0
	tzKnown := false
	offset := 0
	if i < n && s[i] == 'T' {
		hasTime = true
		i++
		if i+2 > n || !allDigits(s[i:i+2]) {
			return 0, false
		}
		hour = atoiField(s[i : i+2])
		i += 2
		if i >= n || s[i] != ':' {
			return 0, false
		}
		i++
		if i+2 > n || !allDigits(s[i:i+2]) {
			return 0, false
		}
		minute = atoiField(s[i : i+2])
		i += 2
		if i < n && s[i] == ':' {
			i++
			if i+2 > n || !allDigits(s[i:i+2]) {
				return 0, false
			}
			sec = atoiField(s[i : i+2])
			i += 2
			if i < n && s[i] == '.' {
				i++
				if i+3 > n || !allDigits(s[i:i+3]) {
					return 0, false
				}
				ms = atoiField(s[i : i+3])
				i += 3
			}
		}
		if i < n {
			switch s[i] {
			case 'Z':
				tzKnown = true
				offset = 0
				i++
			case '+', '-':
				sign := 1
				if s[i] == '-' {
					sign = -1
				}
				i++
				if i+2 > n || !allDigits(s[i:i+2]) {
					return 0, false
				}
				oh := atoiField(s[i : i+2])
				i += 2
				if i >= n || s[i] != ':' {
					return 0, false
				}
				i++
				if i+2 > n || !allDigits(s[i:i+2]) {
					return 0, false
				}
				om := atoiField(s[i : i+2])
				i += 2
				if oh > 23 || om > 59 {
					return 0, false
				}
				tzKnown = true
				offset = sign * (oh*60 + om)
			}
		}
	}
	if i != n {
		return 0, false
	}

	// Field range validation.
	if hour == 24 {
		if minute != 0 || sec != 0 || ms != 0 {
			return 0, false
		}
	} else if hour > 23 {
		return 0, false
	}
	if minute > 59 || sec > 59 {
		return 0, false
	}

	dv := makeDate(
		makeDay(float64(year), float64(month-1), float64(day)),
		makeTime(float64(hour), float64(minute), float64(sec), float64(ms)),
	)
	// Date-time forms with an explicit offset are shifted to UTC. Offsetless
	// forms are UTC for date-only strings and local (== UTC here) otherwise.
	if hasTime && tzKnown {
		dv -= float64(offset) * msPerMinute
	}
	return timeClip(dv), true
}

func monthIndex(name string) int {
	for i, m := range monthNames {
		if m == name {
			return i
		}
	}
	return -1
}

// parseLegacy recognises the output of Date.prototype.toString and
// Date.prototype.toUTCString so those round-trip through Date.parse.
func parseLegacy(s string) (float64, bool) {
	f := strings.Fields(s)
	if len(f) < 6 {
		return 0, false
	}
	var monthStr, dayStr, yearStr, timeStr string
	offset := 0
	if strings.HasSuffix(f[0], ",") {
		// "Www, dd Mmm yyyy HH:MM:SS GMT"
		dayStr, monthStr, yearStr, timeStr = f[1], f[2], f[3], f[4]
		if f[5] != "GMT" {
			return 0, false
		}
	} else {
		// "Www Mmm dd yyyy HH:MM:SS GMT+0000 (...)"
		monthStr, dayStr, yearStr, timeStr = f[1], f[2], f[3], f[4]
		tz := f[5]
		if !strings.HasPrefix(tz, "GMT") {
			return 0, false
		}
		rest := tz[3:]
		if rest != "" {
			if len(rest) != 5 || (rest[0] != '+' && rest[0] != '-') ||
				!allDigits(rest[1:3]) || !allDigits(rest[3:5]) {
				return 0, false
			}
			sign := 1
			if rest[0] == '-' {
				sign = -1
			}
			offset = sign * (atoiField(rest[1:3])*60 + atoiField(rest[3:5]))
		}
	}

	mo := monthIndex(monthStr)
	if mo < 0 || !allDigits(dayStr) {
		return 0, false
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return 0, false
	}
	tp := strings.Split(timeStr, ":")
	if len(tp) != 3 || !allDigits(tp[0]) || !allDigits(tp[1]) || !allDigits(tp[2]) {
		return 0, false
	}
	dv := makeDate(
		makeDay(float64(year), float64(mo), float64(atoiField(dayStr))),
		makeTime(float64(atoiField(tp[0])), float64(atoiField(tp[1])), float64(atoiField(tp[2])), 0),
	)
	dv -= float64(offset) * msPerMinute
	return timeClip(dv), true
}

// ---------------------------------------------------------------------------
// dateNow — clock-gated "current time in milliseconds" (ECMA-262 §21.4.4.1)
// ---------------------------------------------------------------------------

func (i *Interpreter) dateNow(ctx context.Context) float64 {
	if i.clock != nil {
		return float64(i.clock.Now(ctx).UnixMilli())
	}
	return 0
}

// ---------------------------------------------------------------------------
// initDate — entry point
// ---------------------------------------------------------------------------

// initDate installs the Date constructor and Date.prototype on the global
// object. It is called once from bootstrap.
//
// ECMA-262 §21.4
func (i *Interpreter) initDate() {
	proto := i.dateProto
	// Date.prototype is an ordinary object: it is NOT a Date instance and has
	// no [[DateValue]] slot (§21.4.4).

	// requireDate implements thisTimeValue: it returns the [[DateValue]] of a
	// genuine Date instance, or a TypeError for any other receiver.
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

	// setDV stores ms into the Date object's slot and returns it as a Number.
	setDV := func(obj *Object, ms float64) Value {
		obj.primitive = Number(ms)
		return Number(ms)
	}

	// getter builds a field-extraction method that returns NaN for invalid dates.
	getter := func(name string, extract func(t float64) float64) {
		i.defineMethod(proto, name, 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
			ms, _, err := requireDate(ctx, this, name)
			if err != nil {
				return nil, err
			}
			if math.IsNaN(ms) {
				return Number(math.NaN()), nil
			}
			return Number(extract(ms)), nil
		})
	}

	// -------------------------------------------------------------------------
	// getTime / valueOf (§21.4.4.10 / §21.4.4.44)
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
	// Field getters (local == UTC in this runtime)
	// -------------------------------------------------------------------------
	yearOf := func(t float64) float64 { return yearFromTime(t) }
	monthOf := func(t float64) float64 { return float64(monthFromTime(t)) }
	dateOf := func(t float64) float64 { return float64(dateFromTime(t)) }
	dayOf := func(t float64) float64 { return float64(weekDay(t)) }
	hourOf := func(t float64) float64 { return float64(hourFromTime(t)) }
	minOf := func(t float64) float64 { return float64(minFromTime(t)) }
	secOf := func(t float64) float64 { return float64(secFromTime(t)) }
	msOf := func(t float64) float64 { return float64(msFromTime(t)) }

	getter("getFullYear", yearOf)
	getter("getUTCFullYear", yearOf)
	getter("getMonth", monthOf)
	getter("getUTCMonth", monthOf)
	getter("getDate", dateOf)
	getter("getUTCDate", dateOf)
	getter("getDay", dayOf)
	getter("getUTCDay", dayOf)
	getter("getHours", hourOf)
	getter("getUTCHours", hourOf)
	getter("getMinutes", minOf)
	getter("getUTCMinutes", minOf)
	getter("getSeconds", secOf)
	getter("getUTCSeconds", secOf)
	getter("getMilliseconds", msOf)
	getter("getUTCMilliseconds", msOf)

	// getTimezoneOffset — §21.4.4.11. 0 for valid dates, NaN for invalid ones.
	i.defineMethod(proto, "getTimezoneOffset", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "getTimezoneOffset")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(ms) {
			return Number(math.NaN()), nil
		}
		return Number(0), nil
	})

	// -------------------------------------------------------------------------
	// Setters
	// -------------------------------------------------------------------------

	// setTime — §21.4.4.27
	i.defineMethod(proto, "setTime", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		_, obj, err := requireDate(ctx, this, "setTime")
		if err != nil {
			return nil, err
		}
		v, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return setDV(obj, timeClip(v)), nil
	})

	// setTimeOfDay backs setHours/setMinutes/setSeconds/setMilliseconds. field
	// is the index (0=hour…3=ms) of the first (mandatory) argument. All present
	// arguments are coerced before the invalid-date check per §21.4.4.
	setTimeOfDay := func(name string, field, length int) {
		fn := func(ctx context.Context, this Value, args []Value) (Value, error) {
			t, obj, err := requireDate(ctx, this, name)
			if err != nil {
				return nil, err
			}
			// vals[0..3] correspond to hour, min, sec, ms. present[k] records
			// whether an argument was supplied for that slot.
			var vals [4]float64
			var present [4]bool
			for k := field; k < 4; k++ {
				argIdx := k - field
				// The first (mandatory) argument is always coerced — even when
				// absent, yielding ToNumber(undefined) = NaN.
				if k == field || argIdx < len(args) {
					v, err := i.argNum(ctx, args, argIdx)
					if err != nil {
						return nil, err
					}
					vals[k] = v
					present[k] = true
				}
			}
			// If the stored time is NaN, return NaN without mutating the slot:
			// argument coercion above may have replaced it via a side effect.
			if math.IsNaN(t) {
				return Number(math.NaN()), nil
			}
			if !present[0] {
				vals[0] = float64(hourFromTime(t))
			}
			if !present[1] {
				vals[1] = float64(minFromTime(t))
			}
			if !present[2] {
				vals[2] = float64(secFromTime(t))
			}
			if !present[3] {
				vals[3] = float64(msFromTime(t))
			}
			date := makeDate(dayNumber(t), makeTime(vals[0], vals[1], vals[2], vals[3]))
			return setDV(obj, timeClip(date)), nil
		}
		i.defineMethod(proto, name, length, fn)
	}
	setTimeOfDay("setHours", 0, 4)
	setTimeOfDay("setUTCHours", 0, 4)
	setTimeOfDay("setMinutes", 1, 3)
	setTimeOfDay("setUTCMinutes", 1, 3)
	setTimeOfDay("setSeconds", 2, 2)
	setTimeOfDay("setUTCSeconds", 2, 2)
	setTimeOfDay("setMilliseconds", 3, 1)
	setTimeOfDay("setUTCMilliseconds", 3, 1)

	// setDate — §21.4.4.20
	setDateFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		t, obj, err := requireDate(ctx, this, "setDate")
		if err != nil {
			return nil, err
		}
		dt, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(t) {
			return Number(math.NaN()), nil
		}
		date := makeDate(makeDay(yearFromTime(t), float64(monthFromTime(t)), dt), timeWithinDay(t))
		return setDV(obj, timeClip(date)), nil
	}
	i.defineMethod(proto, "setDate", 1, setDateFn)
	i.defineMethod(proto, "setUTCDate", 1, setDateFn)

	// setMonth — §21.4.4.25  (month[, date])
	setMonthFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		t, obj, err := requireDate(ctx, this, "setMonth")
		if err != nil {
			return nil, err
		}
		m, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		hasDt := len(args) > 1
		var dt float64
		if hasDt {
			dt, err = i.argNum(ctx, args, 1)
			if err != nil {
				return nil, err
			}
		}
		if math.IsNaN(t) {
			return Number(math.NaN()), nil
		}
		if !hasDt {
			dt = float64(dateFromTime(t))
		}
		date := makeDate(makeDay(yearFromTime(t), m, dt), timeWithinDay(t))
		return setDV(obj, timeClip(date)), nil
	}
	i.defineMethod(proto, "setMonth", 2, setMonthFn)
	i.defineMethod(proto, "setUTCMonth", 2, setMonthFn)

	// setFullYear — §21.4.4.21  (year[, month[, date]]). Rescues an invalid
	// date by treating its time as +0.
	setFullYearFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		t, obj, err := requireDate(ctx, this, "setFullYear")
		if err != nil {
			return nil, err
		}
		y, err := i.argNum(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		hasM := len(args) > 1
		hasDt := len(args) > 2
		var m, dt float64
		if hasM {
			if m, err = i.argNum(ctx, args, 1); err != nil {
				return nil, err
			}
		}
		if hasDt {
			if dt, err = i.argNum(ctx, args, 2); err != nil {
				return nil, err
			}
		}
		if math.IsNaN(t) {
			t = 0
		}
		if !hasM {
			m = float64(monthFromTime(t))
		}
		if !hasDt {
			dt = float64(dateFromTime(t))
		}
		date := makeDate(makeDay(y, m, dt), timeWithinDay(t))
		return setDV(obj, timeClip(date)), nil
	}
	i.defineMethod(proto, "setFullYear", 3, setFullYearFn)
	i.defineMethod(proto, "setUTCFullYear", 3, setFullYearFn)

	// -------------------------------------------------------------------------
	// String conversion methods
	// -------------------------------------------------------------------------

	stringMethod := func(name string, length int, invalid string, fmtFn func(t float64) string) *Object {
		return i.defineMethod(proto, name, length, func(ctx context.Context, this Value, args []Value) (Value, error) {
			ms, _, err := requireDate(ctx, this, name)
			if err != nil {
				return nil, err
			}
			if math.IsNaN(ms) {
				return String(invalid), nil
			}
			return String(fmtFn(ms)), nil
		})
	}

	stringMethod("toString", 0, "Invalid Date", dateToString)
	stringMethod("toDateString", 0, "Invalid Date", dateString)
	stringMethod("toTimeString", 0, "Invalid Date", func(t float64) string {
		return timeString(t) + timeZoneString()
	})
	utcFn := stringMethod("toUTCString", 0, "Invalid Date", dateToUTCString)
	// Annex B: Date.prototype.toGMTString is the same function object.
	proto.SetHidden("toGMTString", utcFn)

	// toLocale* have no Intl support here: they mirror the plain conversions.
	stringMethod("toLocaleString", 0, "Invalid Date", dateToString)
	stringMethod("toLocaleDateString", 0, "Invalid Date", dateString)
	stringMethod("toLocaleTimeString", 0, "Invalid Date", func(t float64) string {
		return timeString(t) + timeZoneString()
	})

	// toISOString — §21.4.4.36. Throws RangeError when the time value is not
	// finite.
	i.defineMethod(proto, "toISOString", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ms, _, err := requireDate(ctx, this, "toISOString")
		if err != nil {
			return nil, err
		}
		if !dtFinite(ms) {
			return nil, i.throwError(ctx, "RangeError", "Invalid time value")
		}
		return String(dateToISO(ms)), nil
	})

	// toJSON — §21.4.4.37. ToObject → ToPrimitive(number); null for non-finite;
	// otherwise Invoke(O, "toISOString").
	i.defineMethod(proto, "toJSON", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		o, err := i.ToObject(ctx, this)
		if err != nil {
			return nil, err
		}
		tv, err := i.ToPrimitive(ctx, o, "number")
		if err != nil {
			return nil, err
		}
		if num, ok := tv.(Number); ok && !dtFinite(float64(num)) {
			return Nul, nil
		}
		iso, err := o.GetStr(ctx, "toISOString")
		if err != nil {
			return nil, err
		}
		fn, ok := iso.(*Object)
		if !ok || !fn.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "toISOString is not callable")
		}
		return fn.fn.call(ctx, o, nil)
	})

	// ordinaryToPrimitive implements §7.1.1.1 for @@toPrimitive.
	ordinaryToPrimitive := func(ctx context.Context, o *Object, hint string) (Value, error) {
		names := []string{"valueOf", "toString"}
		if hint == "string" {
			names = []string{"toString", "valueOf"}
		}
		for _, n := range names {
			m, err := o.GetStr(ctx, n)
			if err != nil {
				return nil, err
			}
			if fn, ok := m.(*Object); ok && fn.IsCallable() {
				res, err := fn.fn.call(ctx, o, nil)
				if err != nil {
					return nil, err
				}
				if isPrimitive(res) {
					return res, nil
				}
			}
		}
		return nil, i.throwError(ctx, "TypeError", "Cannot convert object to primitive value")
	}

	// Date.prototype[Symbol.toPrimitive] — §21.4.4.45
	toPrimFn := i.newNativeFunc("[Symbol.toPrimitive]", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		obj, ok := this.(*Object)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "Date.prototype[Symbol.toPrimitive] called on non-object")
		}
		hint, _ := asString(arg(args, 0))
		var tryFirst string
		switch hint {
		case "string", "default":
			tryFirst = "string"
		case "number":
			tryFirst = "number"
		default:
			return nil, i.throwError(ctx, "TypeError", "invalid hint")
		}
		return ordinaryToPrimitive(ctx, obj, tryFirst)
	})
	proto.defineOwn(SymKey(i.symToPrimitive), &Property{Value: toPrimFn, Writable: false, Enumerable: false, Configurable: true})

	// -------------------------------------------------------------------------
	// Date constructor (§21.4.2)
	// -------------------------------------------------------------------------

	newDateObj := func(ms float64) *Object {
		o := NewObject(i.dateProto)
		o.class = "Date"
		o.primitive = Number(ms)
		return o
	}

	// callFn — Date() called without new (§21.4.2.1): the current time as a
	// human-readable string, regardless of arguments.
	callFn := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return String(dateToString(timeClip(i.dateNow(ctx)))), nil
	}

	constructFn := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		var ms float64
		switch len(args) {
		case 0:
			ms = timeClip(i.dateNow(ctx))

		case 1:
			v := arg(args, 0)
			if o, ok := v.(*Object); ok && o.class == "Date" {
				// Copy the [[DateValue]] directly without user coercion.
				if n, ok := o.primitive.(Number); ok {
					ms = timeClip(float64(n))
				} else {
					ms = math.NaN()
				}
				break
			}
			prim, err := i.ToPrimitive(ctx, v, "default")
			if err != nil {
				return nil, err
			}
			if s, ok := prim.(String); ok {
				ms = timeClip(parseDate(string(s)))
			} else {
				f, err := i.ToNumberV(ctx, prim)
				if err != nil {
					return nil, err
				}
				ms = timeClip(f)
			}

		default:
			comps, err := i.dateComponents(ctx, args)
			if err != nil {
				return nil, err
			}
			ms = timeClip(comps)
		}
		// GetPrototypeFromConstructor (§21.4.2.1): a subclass instance takes its
		// prototype from new.target rather than %Date.prototype%.
		p, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.dateProto })
		if err != nil {
			return nil, err
		}
		o := newDateObj(ms)
		if p != i.dateProto {
			o.SetProto(p)
		}
		return o, nil
	}

	ctor := i.newNativeCtor("Date", 7, callFn, constructFn)
	linkCtor(ctor, proto)

	// -------------------------------------------------------------------------
	// Static methods
	// -------------------------------------------------------------------------

	// Date.now — §21.4.3.1
	i.defineMethod(ctor, "now", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return Number(timeClip(i.dateNow(ctx))), nil
	})

	// Date.parse — §21.4.3.1
	i.defineMethod(ctor, "parse", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		return Number(parseDate(s)), nil
	})

	// Date.UTC — §21.4.3.4
	i.defineMethod(ctor, "UTC", 7, func(ctx context.Context, this Value, args []Value) (Value, error) {
		comps, err := i.dateComponents(ctx, args)
		if err != nil {
			return nil, err
		}
		return Number(timeClip(comps)), nil
	})

	i.setGlobalHidden("Date", ctor)
}

// dateComponents coerces the (year, month, …) argument list shared by the
// multi-argument Date constructor and Date.UTC, applies the two-digit year
// offset, and returns MakeDate(MakeDay, MakeTime) before TimeClip. All present
// arguments are coerced in order regardless of intermediate NaNs.
func (i *Interpreter) dateComponents(ctx context.Context, args []Value) (float64, error) {
	y, err := i.argNum(ctx, args, 0)
	if err != nil {
		return 0, err
	}
	m := float64(0)
	if len(args) > 1 {
		if m, err = i.argNum(ctx, args, 1); err != nil {
			return 0, err
		}
	}
	dt := float64(1)
	if len(args) > 2 {
		if dt, err = i.argNum(ctx, args, 2); err != nil {
			return 0, err
		}
	}
	h := float64(0)
	if len(args) > 3 {
		if h, err = i.argNum(ctx, args, 3); err != nil {
			return 0, err
		}
	}
	mi := float64(0)
	if len(args) > 4 {
		if mi, err = i.argNum(ctx, args, 4); err != nil {
			return 0, err
		}
	}
	s := float64(0)
	if len(args) > 5 {
		if s, err = i.argNum(ctx, args, 5); err != nil {
			return 0, err
		}
	}
	milli := float64(0)
	if len(args) > 6 {
		if milli, err = i.argNum(ctx, args, 6); err != nil {
			return 0, err
		}
	}
	yr := yearPromote(y)
	return makeDate(makeDay(yr, m, dt), makeTime(h, mi, s, milli)), nil
}
