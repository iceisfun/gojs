package interp

import "testing"

// Number/BigInt.prototype.toLocaleString format through x/text's CLDR data
// (locale_format.go). These pin the default (English) output that matches a
// full-ICU host like Node, plus locale selection and the common options.
func TestNumberToLocaleString(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// default locale (English) grouping
		{`(664579).toLocaleString()`, "664,579"},
		{`(10000000).toLocaleString()`, "10,000,000"},
		{`(0).toLocaleString()`, "0"},
		{`(-1234.5).toLocaleString()`, "-1,234.5"},
		// default caps at 3 fraction digits, rounding
		{`(1234.5678).toLocaleString()`, "1,234.568"},
		// non-finite mirror a full-ICU host
		{`(NaN).toLocaleString()`, "NaN"},
		{`(Infinity).toLocaleString()`, "∞"},
		{`(-Infinity).toLocaleString()`, "-∞"},
		// explicit locales: de swaps separators, fr groups with U+00A0
		{`(1234567).toLocaleString("de-DE")`, "1.234.567"},
		{`(1234.5).toLocaleString("de-DE")`, "1.234,5"},
		{`(1234567).toLocaleString("fr-FR")`, "1 234 567"},
		// options: grouping off, fraction digits
		{`(1234567).toLocaleString("en-US",{useGrouping:false})`, "1234567"},
		{`(1.5).toLocaleString("en-US",{minimumFractionDigits:3})`, "1.500"},
		{`(1.23456).toLocaleString("en-US",{maximumFractionDigits:2})`, "1.23"},
		{`(7).toLocaleString("en-US",{minimumIntegerDigits:4})`, "0,007"},
		// array of locales: first element wins
		{`(1234567).toLocaleString(["de-DE","en-US"])`, "1.234.567"},
	}
	for _, tc := range cases {
		i := New(WithBytecode())
		v, err := i.RunString("t", tc.src)
		if err != nil {
			t.Errorf("%s: error %v", tc.src, err)
			continue
		}
		got, _ := asString(v)
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestBigIntToLocaleString(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`(664579n).toLocaleString()`, "664,579"},
		{`(1234567n).toLocaleString("de-DE")`, "1.234.567"},
		{`(-1000000n).toLocaleString()`, "-1,000,000"},
		{`(1234567n).toLocaleString("en-US",{useGrouping:false})`, "1234567"},
		// beyond int64: grouped via the manual 3-digit path
		{`(123456789012345678901234567890n).toLocaleString()`, "123,456,789,012,345,678,901,234,567,890"},
		{`(-123456789012345678901234567890n).toLocaleString("de-DE")`, "-123.456.789.012.345.678.901.234.567.890"},
	}
	for _, tc := range cases {
		i := New(WithBytecode())
		v, err := i.RunString("t", tc.src)
		if err != nil {
			t.Errorf("%s: error %v", tc.src, err)
			continue
		}
		got, _ := asString(v)
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestToLocaleStringErrors(t *testing.T) {
	// malformed locale tag → RangeError; null locales → TypeError (via ToObject).
	cases := []struct {
		src  string
		want string // substring of the thrown error's name
	}{
		{`try{(1).toLocaleString("en-US-@@bad")}catch(e){e.constructor.name}`, "RangeError"},
		{`try{(1).toLocaleString(null)}catch(e){e.constructor.name}`, "TypeError"},
	}
	for _, tc := range cases {
		i := New(WithBytecode())
		v, err := i.RunString("t", tc.src)
		if err != nil {
			t.Errorf("%s: error %v", tc.src, err)
			continue
		}
		got, _ := asString(v)
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}
