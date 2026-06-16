package torrentmeta

import (
	"strconv"

	"github.com/cockroachdb/errors"
)

// maxDepth bounds list/dict nesting to prevent a maliciously nested .torrent
// from overflowing the goroutine stack (a fatal, unrecoverable error). Real
// torrents nest only a handful of levels deep.
const maxDepth = 100

// decoder is a minimal bencode reader. It decodes integers, byte strings,
// lists, and dictionaries, and records the byte span of the top-level "info"
// value so the caller can hash it exactly.
//
// It is deliberately lenient about non-canonical encodings (e.g. leading-zero
// integers or string lengths): the info-hash is taken from the raw byte span,
// not a re-encoding, and rutracker emits canonical torrents, so strict
// validation would add complexity without catching a real failure mode.
type decoder struct {
	data  []byte
	pos   int
	depth int

	infoStart int
	infoEnd   int
}

// decode reads a single bencode value at the current position.
func (d *decoder) decode() (any, error) {
	current, err := d.peek()
	if err != nil {
		return nil, err
	}

	switch {
	case current == 'i':
		return d.decodeInt()
	case current == 'l':
		return d.decodeList()
	case current == 'd':
		return d.decodeDict()
	case current >= '0' && current <= '9':
		return d.decodeString()
	default:
		return nil, errors.Wrapf(ErrInvalidTorrent, "unexpected byte %q at %d", current, d.pos)
	}
}

// peek returns the byte at the current position without consuming it.
func (d *decoder) peek() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, errors.Wrap(ErrInvalidTorrent, "unexpected end of input")
	}

	return d.data[d.pos], nil
}

// decodeInt reads an "i<digits>e" integer.
func (d *decoder) decodeInt() (int64, error) {
	d.pos++ // consume 'i'

	end := d.indexOf('e')
	if end < 0 {
		return 0, errors.Wrap(ErrInvalidTorrent, "unterminated integer")
	}

	value, err := strconv.ParseInt(string(d.data[d.pos:end]), 10, 64)
	if err != nil {
		return 0, errors.Wrap(ErrInvalidTorrent, "invalid integer")
	}

	d.pos = end + 1

	return value, nil
}

// decodeString reads a "<length>:<bytes>" byte string.
func (d *decoder) decodeString() (string, error) {
	sep := d.indexOf(':')
	if sep < 0 {
		return "", errors.Wrap(ErrInvalidTorrent, "string missing length separator")
	}

	length, err := strconv.Atoi(string(d.data[d.pos:sep]))
	if err != nil || length < 0 {
		return "", errors.Wrap(ErrInvalidTorrent, "invalid string length")
	}

	start := sep + 1

	// Compare against the remaining bytes before adding, so a length near
	// math.MaxInt cannot overflow start+length into a negative end that would
	// slip past the bounds check and panic the slice expression.
	if length > len(d.data)-start {
		return "", errors.Wrap(ErrInvalidTorrent, "string length exceeds input")
	}

	end := start + length
	d.pos = end

	return string(d.data[start:end]), nil
}

// decodeList reads an "l...e" list.
func (d *decoder) decodeList() ([]any, error) {
	d.depth++
	defer func() { d.depth-- }()

	if d.depth > maxDepth {
		return nil, errors.Wrap(ErrInvalidTorrent, "maximum nesting depth exceeded")
	}

	d.pos++ // consume 'l'

	items := make([]any, 0)

	for {
		current, err := d.peek()
		if err != nil {
			return nil, err
		}

		if current == 'e' {
			d.pos++

			return items, nil
		}

		item, err := d.decode()
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}
}

// decodeDict reads a "d...e" dictionary, recording the byte span of the
// top-level "info" value for info-hash computation.
func (d *decoder) decodeDict() (map[string]any, error) {
	d.depth++
	defer func() { d.depth-- }()

	if d.depth > maxDepth {
		return nil, errors.Wrap(ErrInvalidTorrent, "maximum nesting depth exceeded")
	}

	d.pos++ // consume 'd'

	dict := make(map[string]any)

	for {
		current, err := d.peek()
		if err != nil {
			return nil, err
		}

		if current == 'e' {
			d.pos++

			return dict, nil
		}

		key, err := d.decodeString()
		if err != nil {
			return nil, err
		}

		valueStart := d.pos

		value, err := d.decode()
		if err != nil {
			return nil, err
		}

		if d.depth == 1 && key == "info" {
			d.infoStart = valueStart
			d.infoEnd = d.pos
		}

		dict[key] = value
	}
}

// indexOf returns the absolute index of the next occurrence of target at or
// after the current position, or -1 if absent.
func (d *decoder) indexOf(target byte) int {
	for i := d.pos; i < len(d.data); i++ {
		if d.data[i] == target {
			return i
		}
	}

	return -1
}
