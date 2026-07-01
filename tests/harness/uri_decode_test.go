package harness

import "testing"

// decodeURI / decodeURIComponent per ECMA-262 §19.2.6 (the Decode abstract
// operation). These were entirely missing before this change.

func TestDecodeURIBasicUnescape(t *testing.T) {
	Expect(t, `
		assert.sameValue(decodeURIComponent("%41%42%43"), "ABC");
		assert.sameValue(decodeURIComponent("%61%62%63"), "abc");
		assert.sameValue(decodeURI("http://unipro.ru/0123456789"), "http://unipro.ru/0123456789");
		// lowercase hex digits decode too
		assert.sameValue(decodeURIComponent("%2f"), "/");
	`)
}

// decodeURI preserves the reserved set (";/?:@&=+$,#") as escapes; decodeURIComponent decodes them.
func TestDecodeURIReservedSet(t *testing.T) {
	Expect(t, `
		var reserved = ";/?:@&=+$,#";
		for (var k = 0; k < reserved.length; k++) {
			var c = reserved.charAt(k);
			var esc = "%" + c.charCodeAt(0).toString(16).toUpperCase();
			assert.sameValue(decodeURI(esc), esc, "decodeURI preserves " + c);
			assert.sameValue(decodeURIComponent(esc), c, "decodeURIComponent decodes " + c);
		}
	`)
}

// Every code unit except "%" round-trips through decodeURI unchanged when it has
// no escape (Test262 S15.1.3.1_A2.1_T1).
func TestDecodeURIIdentityNoEscape(t *testing.T) {
	Expect(t, `
		for (var i = 0; i <= 0x7ff; i++) {
			if (i === 0x25) continue;
			var s = String.fromCharCode(i);
			assert.sameValue(decodeURI(s), s, "identity at " + i);
		}
	`)
}

// Multibyte UTF-8 escapes reassemble to the right code point.
func TestDecodeURIMultibyte(t *testing.T) {
	Expect(t, `
		assert.sameValue(decodeURIComponent("%C2%80"), "");
		assert.sameValue(decodeURIComponent("%E2%82%AC"), "€");   // euro sign
		assert.sameValue(decodeURIComponent("%D0%90"), "А");      // Cyrillic A
		// U+FFFD (the replacement char) is a valid code point, not a decode
		// error — it must round-trip rather than be rejected as malformed.
		assert.sameValue(decodeURIComponent("%EF%BF%BD"), "�");
		assert.sameValue(decodeURIComponent("%EF%BF%BD").charCodeAt(0), 0xFFFD);
	`)
}

// Malformed escapes throw URIError.
func TestDecodeURIMalformedThrows(t *testing.T) {
	bad := []string{
		`decodeURI("%")`,
		`decodeURI("%A")`,
		`decodeURI("%1")`,
		`decodeURI("%G0")`,
		`decodeURI("%0G")`,
		`decodeURIComponent("%80")`,          // lone continuation byte
		`decodeURIComponent("%C0%80")`,       // overlong encoding of NUL
		`decodeURIComponent("%C2%20")`,       // second octet not a continuation
		`decodeURIComponent("%E2%82")`,       // truncated 3-byte sequence
		`decodeURIComponent("%F5%80%80%80")`, // code point > U+10FFFF
		`decodeURIComponent("%ED%A0%80")`,    // UTF-8 encoding of a surrogate
	}
	for _, src := range bad {
		ExpectError(t, src, "URIError")
	}
}

// Property shape: non-enumerable, callable, name/length.
func TestDecodeURIFunctionShape(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof decodeURI, "function");
		assert.sameValue(typeof decodeURIComponent, "function");
		assert.sameValue(decodeURI.name, "decodeURI");
		assert.sameValue(decodeURIComponent.name, "decodeURIComponent");
		assert.sameValue(decodeURI.length, 1);
		assert.sameValue(decodeURIComponent.length, 1);
		// name and length are non-writable, non-enumerable, configurable.
		var nd = Object.getOwnPropertyDescriptor(decodeURI, "name");
		assert.sameValue(nd.writable, false);
		assert.sameValue(nd.enumerable, false);
		assert.sameValue(nd.configurable, true);
		var ld = Object.getOwnPropertyDescriptor(decodeURI, "length");
		assert.sameValue(ld.writable, false);
		assert.sameValue(ld.enumerable, false);
		assert.sameValue(ld.configurable, true);
	`)
}
