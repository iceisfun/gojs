package harness

import "testing"

// TestDateEpoch verifies the Unix-epoch baseline: new Date(0).
// Covers: getTime, valueOf, all getUTC* field getters, getUTCDay, toISOString.
func TestDateEpoch(t *testing.T) {
	Expect(t, `
		var d = new Date(0);
		assert.sameValue(d.getTime(), 0, "getTime at epoch");
		assert.sameValue(d.valueOf(), 0, "valueOf at epoch");
		assert.sameValue(d.getUTCFullYear(), 1970, "getUTCFullYear at epoch");
		assert.sameValue(d.getUTCMonth(), 0, "getUTCMonth at epoch (0=January)");
		assert.sameValue(d.getUTCDate(), 1, "getUTCDate at epoch");
		assert.sameValue(d.getUTCHours(), 0, "getUTCHours at epoch");
		assert.sameValue(d.getUTCMinutes(), 0, "getUTCMinutes at epoch");
		assert.sameValue(d.getUTCSeconds(), 0, "getUTCSeconds at epoch");
		assert.sameValue(d.getUTCMilliseconds(), 0, "getUTCMilliseconds at epoch");
		assert.sameValue(d.getUTCDay(), 4, "getUTCDay at epoch (4=Thursday)");
		assert.sameValue(d.toISOString(), "1970-01-01T00:00:00.000Z",
			"toISOString at epoch");
	`)
}

// TestDateFromMs verifies new Date(ms) for specific millisecond values and that
// valueOf() / Number() coercion both equal getTime().
func TestDateFromMs(t *testing.T) {
	Expect(t, `
		// 1 second past the epoch
		var d1 = new Date(1000);
		assert.sameValue(d1.getTime(), 1000, "getTime 1 s past epoch");
		assert.sameValue(d1.getUTCSeconds(), 1, "getUTCSeconds 1 s past epoch");
		assert.sameValue(d1.getUTCFullYear(), 1970, "getUTCFullYear 1 s past epoch");

		// 2020-01-15T00:00:00.000Z via Date.UTC
		var ms2020 = Date.UTC(2020, 0, 15);
		var d2020 = new Date(ms2020);
		assert.sameValue(d2020.getTime(), ms2020,
			"getTime matches Date.UTC result");
		assert.sameValue(d2020.getUTCFullYear(), 2020,
			"getUTCFullYear 2020-01-15");
		assert.sameValue(d2020.getUTCMonth(), 0,
			"getUTCMonth 2020-01-15 (0=January)");
		assert.sameValue(d2020.getUTCDate(), 15, "getUTCDate 2020-01-15");
		assert.sameValue(d2020.toISOString(), "2020-01-15T00:00:00.000Z",
			"toISOString 2020-01-15");

		// valueOf and Number() coercion equal getTime()
		assert.sameValue(d2020.valueOf(), ms2020, "valueOf equals getTime");
		assert.sameValue(Number(d2020), ms2020, "Number(date) equals getTime");
	`)
}

// TestDateUTCStatic verifies the Date.UTC() static method with several inputs.
func TestDateUTCStatic(t *testing.T) {
	Expect(t, `
		assert.sameValue(Date.UTC(1970, 0, 1), 0, "Date.UTC epoch");
		assert.sameValue(Date.UTC(1970, 0, 1, 0, 0, 1), 1000,
			"Date.UTC 1 second past epoch");
		assert.sameValue(Date.UTC(2000, 0, 1), 946684800000,
			"Date.UTC 2000-01-01");
		assert.sameValue(Date.UTC(1970, 0, 1, 0, 0, 0, 500), 500,
			"Date.UTC with milliseconds");
		assert.sameValue(Date.UTC(1970, 0, 2), 86400000,
			"Date.UTC second day (one full day in ms)");
	`)
}

// TestDateMultiArg verifies the multi-argument constructor.
// In gojs, new Date(year, month, ...) is interpreted in UTC (month is 0-based).
func TestDateMultiArg(t *testing.T) {
	Expect(t, `
		var d = new Date(2020, 0, 15);
		assert.sameValue(d.getFullYear(), 2020, "getFullYear multi-arg");
		assert.sameValue(d.getMonth(), 0, "getMonth multi-arg (0=January)");
		assert.sameValue(d.getDate(), 15, "getDate multi-arg");
		assert.sameValue(d.getUTCFullYear(), 2020, "getUTCFullYear multi-arg");
		assert.sameValue(d.getUTCMonth(), 0, "getUTCMonth multi-arg");
		assert.sameValue(d.getUTCDate(), 15, "getUTCDate multi-arg");
		assert.sameValue(d.getTime(), Date.UTC(2020, 0, 15),
			"multi-arg constructor equals Date.UTC");

		// All seven fields
		var d2 = new Date(2020, 5, 15, 10, 30, 45, 123);
		assert.sameValue(d2.getUTCFullYear(), 2020, "full multi-arg year");
		assert.sameValue(d2.getUTCMonth(), 5, "full multi-arg month (5=June)");
		assert.sameValue(d2.getUTCDate(), 15, "full multi-arg day");
		assert.sameValue(d2.getUTCHours(), 10, "full multi-arg hours");
		assert.sameValue(d2.getUTCMinutes(), 30, "full multi-arg minutes");
		assert.sameValue(d2.getUTCSeconds(), 45, "full multi-arg seconds");
		assert.sameValue(d2.getUTCMilliseconds(), 123, "full multi-arg ms");
		assert.sameValue(d2.getTime(), Date.UTC(2020, 5, 15, 10, 30, 45, 123),
			"full multi-arg matches Date.UTC");
	`)
}

// TestDateGetterAliases verifies that the local-time getters are aliases for
// their UTC counterparts (gojs UTC-only mode) and that getTimezoneOffset is 0.
func TestDateGetterAliases(t *testing.T) {
	Expect(t, `
		var d = new Date(Date.UTC(2020, 3, 7, 14, 25, 36, 789));
		assert.sameValue(d.getFullYear(), d.getUTCFullYear(),
			"getFullYear === getUTCFullYear");
		assert.sameValue(d.getMonth(), d.getUTCMonth(),
			"getMonth === getUTCMonth");
		assert.sameValue(d.getDate(), d.getUTCDate(),
			"getDate === getUTCDate");
		assert.sameValue(d.getDay(), d.getUTCDay(),
			"getDay === getUTCDay");
		assert.sameValue(d.getHours(), d.getUTCHours(),
			"getHours === getUTCHours");
		assert.sameValue(d.getMinutes(), d.getUTCMinutes(),
			"getMinutes === getUTCMinutes");
		assert.sameValue(d.getSeconds(), d.getUTCSeconds(),
			"getSeconds === getUTCSeconds");
		assert.sameValue(d.getMilliseconds(), d.getUTCMilliseconds(),
			"getMilliseconds === getUTCMilliseconds");
		assert.sameValue(d.getTimezoneOffset(), 0,
			"getTimezoneOffset always returns 0");
	`)
}

// TestDateNow verifies that Date.now() returns a number.
// The exact value is not asserted; it depends on the harness time provider.
func TestDateNow(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof Date.now(), "number", "Date.now() returns number");
	`)
}

// TestDateParse verifies Date.parse() and new Date(string) for ISO 8601 strings.
func TestDateParse(t *testing.T) {
	Expect(t, `
		assert.sameValue(Date.parse("1970-01-01T00:00:00.000Z"), 0,
			"Date.parse epoch with ms");
		assert.sameValue(Date.parse("1970-01-01T00:00:00Z"), 0,
			"Date.parse epoch without ms");

		var dDate = new Date("2020-01-15");
		assert.sameValue(dDate.getUTCFullYear(), 2020,
			"new Date(date-only string) year");
		assert.sameValue(dDate.getUTCMonth(), 0,
			"new Date(date-only string) month");
		assert.sameValue(dDate.getUTCDate(), 15,
			"new Date(date-only string) date");

		var dFull = new Date("2020-01-15T12:30:00.000Z");
		assert.sameValue(dFull.getUTCFullYear(), 2020,
			"new Date(ISO datetime) year");
		assert.sameValue(dFull.getUTCHours(), 12,
			"new Date(ISO datetime) hours");
		assert.sameValue(dFull.getUTCMinutes(), 30,
			"new Date(ISO datetime) minutes");

		assert(isNaN(Date.parse("not a date")), "Date.parse invalid string is NaN");
		assert(isNaN(Date.parse("nonsense")), "Date.parse nonsense is NaN");
	`)
}

// TestDateInvalid verifies that dates constructed from NaN or unparseable strings
// report NaN from getTime() and that toISOString() throws RangeError.
func TestDateInvalid(t *testing.T) {
	Expect(t, `
		var bad = new Date(NaN);
		assert(isNaN(bad.getTime()), "NaN date getTime() is NaN");
		assert(isNaN(bad.valueOf()), "NaN date valueOf() is NaN");

		var badStr = new Date("not a date");
		assert(isNaN(badStr.getTime()),
			"unparseable string date getTime() is NaN");

		assert.throws(RangeError,
			function () { new Date(NaN).toISOString(); },
			"invalid date toISOString() throws RangeError");
	`)
}

// TestDateSetters verifies the setter methods that are implemented in gojs.
// The non-UTC set* methods (setFullYear, setMonth, etc.) serve as the UTC
// setters because the engine operates entirely in UTC.
func TestDateSetters(t *testing.T) {
	Expect(t, `
		// setTime
		var d = new Date(0);
		d.setTime(1000);
		assert.sameValue(d.getTime(), 1000, "setTime(1000)");

		// setFullYear
		var d2 = new Date(0);
		d2.setFullYear(2000);
		assert.sameValue(d2.getUTCFullYear(), 2000, "setFullYear(2000)");
		assert.sameValue(d2.getUTCMonth(), 0,
			"setFullYear preserves month");
		assert.sameValue(d2.getUTCDate(), 1,
			"setFullYear preserves date");

		// setFullYear with optional month and day arguments
		var d3 = new Date(0);
		d3.setFullYear(2021, 5, 20);
		assert.sameValue(d3.getUTCFullYear(), 2021, "setFullYear(y,m,d) year");
		assert.sameValue(d3.getUTCMonth(), 5, "setFullYear(y,m,d) month");
		assert.sameValue(d3.getUTCDate(), 20, "setFullYear(y,m,d) date");

		// setMonth
		var d4 = new Date(Date.UTC(2020, 0, 15));
		d4.setMonth(6);
		assert.sameValue(d4.getUTCMonth(), 6, "setMonth(6)");
		assert.sameValue(d4.getUTCFullYear(), 2020, "setMonth preserves year");
		assert.sameValue(d4.getUTCDate(), 15, "setMonth preserves date");

		// setDate
		var d5 = new Date(Date.UTC(2020, 0, 1));
		d5.setDate(20);
		assert.sameValue(d5.getUTCDate(), 20, "setDate(20)");
		assert.sameValue(d5.getUTCMonth(), 0, "setDate preserves month");

		// setHours (single arg)
		var d6 = new Date(Date.UTC(2020, 0, 1, 0, 0, 0, 0));
		d6.setHours(14);
		assert.sameValue(d6.getUTCHours(), 14, "setHours(14)");

		// setHours with all optional args
		var d7 = new Date(Date.UTC(2020, 0, 1));
		d7.setHours(10, 30, 45, 123);
		assert.sameValue(d7.getUTCHours(), 10, "setHours(h,m,s,ms) hours");
		assert.sameValue(d7.getUTCMinutes(), 30, "setHours(h,m,s,ms) minutes");
		assert.sameValue(d7.getUTCSeconds(), 45, "setHours(h,m,s,ms) seconds");
		assert.sameValue(d7.getUTCMilliseconds(), 123, "setHours(h,m,s,ms) ms");

		// setMinutes (single arg)
		var d8 = new Date(Date.UTC(2020, 0, 1, 12, 0, 0));
		d8.setMinutes(45);
		assert.sameValue(d8.getUTCMinutes(), 45, "setMinutes(45)");
		assert.sameValue(d8.getUTCHours(), 12, "setMinutes preserves hours");

		// setMinutes with optional seconds and ms args
		var d9 = new Date(Date.UTC(2020, 0, 1, 12, 0, 0));
		d9.setMinutes(15, 30, 500);
		assert.sameValue(d9.getUTCMinutes(), 15, "setMinutes(m,s,ms) minutes");
		assert.sameValue(d9.getUTCSeconds(), 30, "setMinutes(m,s,ms) seconds");
		assert.sameValue(d9.getUTCMilliseconds(), 500, "setMinutes(m,s,ms) ms");

		// setSeconds (single arg)
		var d10 = new Date(Date.UTC(2020, 0, 1, 12, 0, 0));
		d10.setSeconds(55);
		assert.sameValue(d10.getUTCSeconds(), 55, "setSeconds(55)");

		// setSeconds with optional ms arg
		var d11 = new Date(Date.UTC(2020, 0, 1, 12, 0, 0));
		d11.setSeconds(10, 250);
		assert.sameValue(d11.getUTCSeconds(), 10, "setSeconds(s,ms) seconds");
		assert.sameValue(d11.getUTCMilliseconds(), 250, "setSeconds(s,ms) ms");

		// setMilliseconds
		var d12 = new Date(Date.UTC(2020, 0, 1, 12, 0, 0));
		d12.setMilliseconds(750);
		assert.sameValue(d12.getUTCMilliseconds(), 750, "setMilliseconds(750)");
		assert.sameValue(d12.getUTCSeconds(), 0, "setMilliseconds preserves seconds");
	`)
}

// TestDateUTCSetters exercises the setUTC* methods defined by ECMA-262 but NOT
// implemented in gojs. Each call is expected to throw (because the method is
// absent), making this test FAIL — that failure is the useful signal: it
// documents which UTC setters the engine still needs to add.
func TestDateUTCSetters(t *testing.T) {
	Expect(t, `
		var d = new Date(0);
		d.setUTCFullYear(2000);
		assert.sameValue(d.getUTCFullYear(), 2000, "setUTCFullYear(2000)");
		d.setUTCMonth(5);
		assert.sameValue(d.getUTCMonth(), 5, "setUTCMonth(5)");
		d.setUTCDate(15);
		assert.sameValue(d.getUTCDate(), 15, "setUTCDate(15)");
		d.setUTCHours(10);
		assert.sameValue(d.getUTCHours(), 10, "setUTCHours(10)");
		d.setUTCMinutes(30);
		assert.sameValue(d.getUTCMinutes(), 30, "setUTCMinutes(30)");
		d.setUTCSeconds(45);
		assert.sameValue(d.getUTCSeconds(), 45, "setUTCSeconds(45)");
		d.setUTCMilliseconds(500);
		assert.sameValue(d.getUTCMilliseconds(), 500, "setUTCMilliseconds(500)");
	`)
}

// TestDateToISO verifies toISOString() output format, toJSON() consistency with
// toISOString() for valid dates, and RangeError for invalid dates.
func TestDateToISO(t *testing.T) {
	Expect(t, `
		var epoch = new Date(0);
		assert.sameValue(epoch.toISOString(), "1970-01-01T00:00:00.000Z",
			"epoch toISOString");

		var d = new Date(Date.UTC(2020, 0, 15, 10, 30, 45, 123));
		assert.sameValue(d.toISOString(), "2020-01-15T10:30:45.123Z",
			"toISOString with sub-second precision");

		// toJSON returns the same string as toISOString for a valid date
		assert.sameValue(d.toJSON(), d.toISOString(),
			"toJSON === toISOString for valid date");

		// toISOString throws RangeError for an invalid date
		assert.throws(RangeError,
			function () { new Date(NaN).toISOString(); },
			"invalid date toISOString throws RangeError");

		// toJSON returns null for an invalid date (does not throw)
		assert.sameValue(new Date(NaN).toJSON(), null,
			"invalid date toJSON returns null");
	`)
}

// TestDateArithmetic verifies that the subtraction operator coerces Date objects
// via valueOf() and produces a numeric difference in milliseconds.
func TestDateArithmetic(t *testing.T) {
	Expect(t, `
		assert.sameValue(new Date(2000) - new Date(1000), 1000,
			"date subtraction yields ms difference");

		var a = new Date(Date.UTC(2020, 0, 2));
		var b = new Date(Date.UTC(2020, 0, 1));
		assert.sameValue(a - b, 86400000,
			"one day difference is 86400000 ms");
	`)
}

// TestDateMonthRollover verifies that Date.UTC() normalises out-of-range month
// values by rolling them into subsequent months/years.
func TestDateMonthRollover(t *testing.T) {
	Expect(t, `
		// month 12 (0-based, i.e. the 13th month) rolls over to January next year
		var rolled = Date.UTC(2020, 12, 1);
		var jan2021 = Date.UTC(2021, 0, 1);
		assert.sameValue(rolled, jan2021,
			"Date.UTC month 12 rolls to Jan of next year");

		// month 13 rolls over to February next year
		var rolled2 = Date.UTC(2020, 13, 1);
		var feb2021 = Date.UTC(2021, 1, 1);
		assert.sameValue(rolled2, feb2021,
			"Date.UTC month 13 rolls to Feb of next year");
	`)
}

// TestDateTimeClip verifies TimeClip: out-of-range values become NaN, -0
// normalises to +0, and fractional milliseconds are truncated.
func TestDateTimeClip(t *testing.T) {
	Expect(t, `
		assert.sameValue(new Date(8640000000000001).getTime(), NaN, "over max is NaN");
		assert.sameValue(new Date(-8640000000000001).getTime(), NaN, "under min is NaN");
		assert.sameValue(new Date(8640000000000000).getTime(), 8640000000000000, "max in range");
		assert.sameValue(new Date(Infinity).getTime(), NaN, "Infinity is NaN");
		assert.sameValue(1 / new Date(-0).getTime(), Infinity, "-0 normalises to +0");
		assert.sameValue(new Date(6.54321).getTime(), 6, "fractional ms truncated");
	`)
}

// TestDateInvalidSetters verifies setter semantics on an invalid date: all
// arguments are coerced first, and the slot is not clobbered by side effects.
func TestDateInvalidSetters(t *testing.T) {
	Expect(t, `
		var d = new Date(NaN);
		assert.sameValue(d.setHours(0), NaN, "setHours on invalid returns NaN");
		assert.sameValue(d.getTime(), NaN, "still invalid");

		// coercion happens before the invalid check; a valueOf side effect that
		// repairs the date must survive.
		var dt = new Date(NaN);
		var calls = 0;
		var v = { valueOf: function () { calls++; dt.setTime(0); return 1; } };
		var r = dt.setHours(v);
		assert.sameValue(calls, 1, "valueOf called exactly once");
		assert.sameValue(r, NaN, "result is NaN");
		assert.sameValue(dt.getTime(), 0, "slot updated by valueOf, not clobbered");

		// setHours() with no argument coerces undefined -> NaN.
		var d2 = new Date(0);
		assert.sameValue(d2.setHours(), NaN, "setHours() with no arg is NaN");
	`)
}

// TestDateNewValueTimeClip verifies setters clip overflowing new values to NaN.
func TestDateNewValueTimeClip(t *testing.T) {
	Expect(t, `
		var d = new Date(0);
		assert.sameValue(d.setMilliseconds(8640000000000001), NaN, "overflow -> NaN");
		assert.sameValue(d.getTime(), NaN, "slot is NaN after overflow");
	`)
}

// TestDateStringFormats verifies the exact toString/toUTCString/toDateString
// formats, including negative-year padding.
func TestDateStringFormats(t *testing.T) {
	Expect(t, `
		var d = new Date(0);
		assert.sameValue(d.toString(),
			"Thu Jan 01 1970 00:00:00 GMT+0000 (Coordinated Universal Time)",
			"toString at epoch");
		assert.sameValue(d.toUTCString(), "Thu, 01 Jan 1970 00:00:00 GMT",
			"toUTCString at epoch");
		assert.sameValue(d.toDateString(), "Thu Jan 01 1970", "toDateString");
		assert.sameValue(new Date("-000001-07-01T00:00Z").toUTCString().split(" ")[3],
			"-0001", "negative year padded to at least 4 digits");
		assert.sameValue(new Date(-8640000000000000).toISOString(),
			"-271821-04-20T00:00:00.000Z", "min date ISO extended year");
		assert.sameValue(new Date(8640000000000000).toISOString(),
			"+275760-09-13T00:00:00.000Z", "max date ISO extended year");
	`)
}

// TestDateParseRoundTrip verifies Date.parse handles ISO offsetless date-time,
// extended-year rejection, and round-trips toString/toUTCString/toISOString.
func TestDateParseRoundTrip(t *testing.T) {
	Expect(t, `
		assert.sameValue(Date.parse("1970-01-01T00:00:00"), 0, "offsetless datetime is UTC here");
		assert.sameValue(Date.parse("1970-01-01"), 0, "date-only is UTC");
		assert.sameValue(Date.parse("-000000-03-31T00:45Z"), NaN, "minus-zero extended year rejected");
		var z = new Date(0);
		assert.sameValue(Date.parse(z.toString()), 0, "round-trip toString");
		assert.sameValue(Date.parse(z.toUTCString()), 0, "round-trip toUTCString");
		assert.sameValue(Date.parse(z.toISOString()), 0, "round-trip toISOString");
	`)
}

// TestDateToPrimitiveAndJSON verifies Date[@@toPrimitive] hint handling and
// that toJSON delegates through the receiver's toISOString.
func TestDateToPrimitiveAndJSON(t *testing.T) {
	Expect(t, `
		var d = new Date(0);
		assert.sameValue(typeof d[Symbol.toPrimitive], "function", "@@toPrimitive present");
		assert.sameValue(d[Symbol.toPrimitive]("number"), 0, "number hint");
		assert.sameValue(d[Symbol.toPrimitive]("string"), d.toString(), "string hint");
		assert.throws(TypeError, function () { d[Symbol.toPrimitive]("bogus"); }, "invalid hint throws");

		// toJSON delegates to the receiver's toISOString.
		var called = 0;
		var obj = { toISOString: function () { called++; return "X"; } };
		assert.sameValue(Date.prototype.toJSON.call(obj), "X", "toJSON invokes toISOString");
		assert.sameValue(called, 1, "toISOString called once");
		assert.sameValue(new Date(NaN).toJSON(), null, "invalid date toJSON is null");
	`)
}

// TestDateProtoNoDateValue verifies Date.prototype has no [[DateValue]] slot.
func TestDateProtoNoDateValue(t *testing.T) {
	ExpectError(t, `Date.prototype.getTime();`, "TypeError")
}
