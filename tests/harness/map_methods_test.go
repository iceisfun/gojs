package harness

import "testing"

// TestMapGetOrInsert covers Map.prototype.getOrInsert / getOrInsertComputed.
func TestMapGetOrInsert(t *testing.T) {
	Expect(t, `
		var m = new Map([['a', 1]]);
		assert.sameValue(m.getOrInsert('a', 99), 1, "existing");
		assert.sameValue(m.getOrInsert('b', 2), 2, "inserted");
		assert.sameValue(m.get('b'), 2, "b stored");
		assert.sameValue(m.size, 2, "size");

		var calls = 0;
		var r = m.getOrInsertComputed('c', function(k) { calls++; return k + '!'; });
		assert.sameValue(r, 'c!', "computed value");
		assert.sameValue(calls, 1, "callback called once");
		// Existing key does not call the callback.
		m.getOrInsertComputed('a', function() { throw new Error("should not run"); });
		assert.sameValue(m.get('a'), 1, "a unchanged");
	`)
	ExpectError(t, `new Map().getOrInsertComputed('x', 5)`, "TypeError")
	ExpectError(t, `Map.prototype.getOrInsert.call({}, 'k', 'v')`, "TypeError")
}

// TestMapGroupBy covers Map.groupBy.
func TestMapGroupBy(t *testing.T) {
	Expect(t, `
		var m = Map.groupBy([1,2,3,4,5], function(x) { return x % 2 === 0 ? 'even' : 'odd'; });
		assert.sameValue(m instanceof Map, true, "is Map");
		assert.sameValue(m.get('odd').join(','), "1,3,5", "odd");
		assert.sameValue(m.get('even').join(','), "2,4", "even");

		// Object keys use SameValueZero identity.
		var k = {};
		var m2 = Map.groupBy([1,2], function() { return k; });
		assert.sameValue(m2.get(k).join(','), "1,2", "object key");
	`)
	ExpectError(t, `Map.groupBy([], 5)`, "TypeError")
}

// TestCollectionIteratorMutation covers the tombstone-based live iteration
// semantics: deletes are skipped, adds are seen, and clear terminates.
func TestCollectionIteratorMutation(t *testing.T) {
	Expect(t, `
		// Delete during iteration does not break the iterator (Map).
		var m = new Map([['a',1],['b',2],['c',3]]);
		var e = m.entries();
		e.next();            // 'a'
		m.delete('b');
		var n = e.next();
		assert.sameValue(n.value[0], 'c', "skips deleted b");

		// clear() terminates a live iterator.
		var m2 = new Map([[1,1],[2,2]]);
		var it = m2.entries();
		it.next();
		m2.clear();
		var d = it.next();
		assert.sameValue(d.done, true, "clear -> done");

		// Set forEach revisits a value deleted then re-added.
		var s = new Set([1]);
		var seen = [];
		var first = true;
		s.forEach(function(v) {
			seen.push(v);
			if (first) { first = false; s.delete(1); s.add(1); }
		});
		assert.sameValue(seen.length, 2, "revisit after delete/re-add");

		// Values added during iteration are visited (Set).
		var s2 = new Set([1,2]);
		var vals = [];
		for (var v of s2) { vals.push(v); if (v === 2) s2.add(3); }
		assert.sameValue(vals.join(','), "1,2,3", "added value seen");
	`)
}
