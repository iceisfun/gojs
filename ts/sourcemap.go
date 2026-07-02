package ts

import (
	"sync"

	"github.com/iceisfun/typescript/sourcemap"
)

// Mapper is an interp.SourceMapper that accumulates the source maps of the
// TypeScript modules transpiled through this package, so error stacks can report
// original .ts positions. It is safe for concurrent use. Install it on a VM with
// gojs.WithSourceMapper and feed it from a transpiling provider (see WithTypeScript).
type Mapper struct {
	mu   sync.RWMutex
	maps map[string]*consumer // module source name -> decoded map
}

// NewMapper returns an empty source-map registry.
func NewMapper() *Mapper { return &Mapper{maps: map[string]*consumer{}} }

// MapPosition implements interp.SourceMapper: it maps a generated (transpiled)
// 1-based position for source back to the original .ts position.
func (m *Mapper) MapPosition(source string, line, column int) (string, int, int, bool) {
	m.mu.RLock()
	c := m.maps[source]
	m.mu.RUnlock()
	if c == nil {
		return "", 0, 0, false
	}
	return c.lookup(line, column)
}

// record decodes raw and stores it under the generated module's source name.
func (m *Mapper) record(source string, raw *sourcemap.RawSourceMap) {
	if raw == nil {
		return
	}
	m.mu.Lock()
	m.maps[source] = newConsumer(raw)
	m.mu.Unlock()
}

// consumer is a decoded v3 source map for one file: per generated line, the
// segments (sorted by generated column) that map to original positions.
type consumer struct {
	source string      // the original source name (sources[0])
	lines  [][]segment // indexed by 0-based generated line
}

type segment struct {
	genCol  int // 0-based generated column
	srcLine int // 0-based original line
	srcCol  int // 0-based original column
}

// lookup maps a 1-based generated (line, column) to the original source name and
// 1-based (line, column). It returns the mapping of the nearest segment at or
// before the column.
func (c *consumer) lookup(line, column int) (string, int, int, bool) {
	gl, gc := line-1, column-1
	if gl < 0 || gl >= len(c.lines) {
		return "", 0, 0, false
	}
	segs := c.lines[gl]
	var best *segment
	for i := range segs {
		if segs[i].genCol <= gc {
			best = &segs[i]
		} else {
			break
		}
	}
	if best == nil {
		return "", 0, 0, false
	}
	return c.source, best.srcLine + 1, best.srcCol + 1, true
}

func newConsumer(raw *sourcemap.RawSourceMap) *consumer {
	c := &consumer{}
	if len(raw.Sources) > 0 {
		c.source = raw.Sources[0]
	}
	// Cumulative fields carried across the whole mappings string (only genCol
	// resets at each generated line).
	var srcIdx, srcLine, srcCol int
	genLine := 0
	genCol := 0
	var line []segment
	pos := 0
	m := raw.Mappings
	fields := make([]int, 0, 5)
	for pos < len(m) {
		switch m[pos] {
		case ';':
			c.lines = append(c.lines, line)
			line = nil
			genLine++
			genCol = 0
			pos++
		case ',':
			pos++
		default:
			fields = fields[:0]
			for pos < len(m) && m[pos] != ',' && m[pos] != ';' {
				v, n := decodeVLQ(m[pos:])
				if n == 0 {
					pos = len(m) // malformed; stop
					break
				}
				fields = append(fields, v)
				pos += n
			}
			if len(fields) == 0 {
				continue
			}
			genCol += fields[0]
			if len(fields) >= 4 {
				srcIdx += fields[1]
				srcLine += fields[2]
				srcCol += fields[3]
				line = append(line, segment{genCol: genCol, srcLine: srcLine, srcCol: srcCol})
			}
			_ = srcIdx
		}
	}
	c.lines = append(c.lines, line) // final line (no trailing ';')
	return c
}

const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// decodeVLQ decodes one base64-VLQ value from the front of s, returning the
// value and the number of characters consumed (0 on error).
func decodeVLQ(s string) (int, int) {
	result, shift, consumed := 0, 0, 0
	for {
		if consumed >= len(s) {
			return 0, 0
		}
		d := b64index(s[consumed])
		if d < 0 {
			return 0, 0
		}
		consumed++
		result += (d & 31) << shift
		if d&32 == 0 {
			break
		}
		shift += 5
	}
	value := result >> 1
	if result&1 != 0 {
		value = -value
	}
	return value, consumed
}

func b64index(ch byte) int {
	for i := 0; i < len(b64); i++ {
		if b64[i] == ch {
			return i
		}
	}
	return -1
}
