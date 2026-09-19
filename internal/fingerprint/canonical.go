package fingerprint

import "github.com/gowebpki/jcs"

// Canonicalize renders JSON bytes into their RFC 8785 (JCS) canonical
// form: object members sorted by UTF-16 code unit, numbers rendered in a
// single fixed format (so `1` and `1.0` produce identical output), and no
// redundant whitespace or string escaping.
//
// encoding/json alone cannot do this: Marshal preserves whatever member
// order the input decoded into and does not guarantee a canonical number
// format (see mvp.md §6). This delegates to a tested RFC 8785
// implementation instead of reimplementing the algorithm.
func Canonicalize(rawJSON []byte) ([]byte, error) {
	return jcs.Transform(rawJSON)
}
