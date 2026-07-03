package harness

import "testing"

// A tagged template's strings array is canonicalized by source location: the
// same source site evaluated twice hands the tag function the identical frozen
// object (GetTemplateObject / the realm's [[TemplateMap]], ECMA-262 §13.2.8.4).
func TestTaggedTemplateSameSiteIdentity(t *testing.T) {
	Expect(t, `
		var seen = [];
		function tag(strings) { seen.push(strings); return strings.join(','); }
		function run() { return tag`+"`a${1}b`"+`; }
		assert.sameValue(run(), "a,b", "cooked strings joined");
		run();
		assert.sameValue(seen.length, 2, "tag called twice");
		assert.sameValue(seen[0], seen[1],
			"the same source site yields the identical strings array");
		assert.sameValue(seen[0].raw, seen[1].raw,
			"the raw sub-array is canonicalized too");
	`)
}

// Distinct template source sites produce distinct strings arrays, even when
// their cooked/raw contents are identical.
func TestTaggedTemplateDifferentSites(t *testing.T) {
	Expect(t, `
		function tag(strings) { return strings; }
		var first = tag`+"`a${1}b`"+`;
		var second = tag`+"`a${1}b`"+`;
		assert.sameValue(first.join(','), second.join(','), "same contents");
		assert(first !== second, "different sites are different objects");
	`)
}

// The template object and its "raw" sub-array are frozen with the descriptors
// mandated by GetTemplateObject: "raw" is non-enumerable/non-writable/
// non-configurable, indices are enumerable/non-writable/non-configurable, and
// "length" is non-writable.
func TestTaggedTemplateFrozenDescriptors(t *testing.T) {
	Expect(t, `
		var obj;
		function tag(strings) { obj = strings; }
		tag`+"`x${1}y`"+`;

		var rawDesc = Object.getOwnPropertyDescriptor(obj, "raw");
		assert.sameValue(rawDesc.enumerable, false, "raw not enumerable");
		assert.sameValue(rawDesc.writable, false, "raw not writable");
		assert.sameValue(rawDesc.configurable, false, "raw not configurable");

		assert.sameValue(Object.isFrozen(obj), true, "template object frozen");
		assert.sameValue(Object.isFrozen(obj.raw), true, "raw array frozen");

		var idx = Object.getOwnPropertyDescriptor(obj, "0");
		assert.sameValue(idx.enumerable, true, "index enumerable");
		assert.sameValue(idx.writable, false, "index not writable");
		assert.sameValue(idx.configurable, false, "index not configurable");

		var lenDesc = Object.getOwnPropertyDescriptor(obj, "length");
		assert.sameValue(lenDesc.enumerable, false, "length not enumerable");
		assert.sameValue(lenDesc.writable, false, "length not writable");
		assert.sameValue(lenDesc.configurable, false, "length not configurable");

		var rawIdx = Object.getOwnPropertyDescriptor(obj.raw, "0");
		assert.sameValue(rawIdx.enumerable, true, "raw index enumerable");
		assert.sameValue(rawIdx.writable, false, "raw index not writable");
		assert.sameValue(rawIdx.configurable, false, "raw index not configurable");
	`)
}
