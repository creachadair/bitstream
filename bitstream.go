// Package bitstream is a library for manipulating bit streams.
//
// A stream represents a bounded sequence of binary digits, indexed from 0.
// When a stream is encoded as bytes for I/O, the bits in each byte are
// arranged from most to least significant.  For example:
//
//   byte  0               1               2 ...
//        +---------------+---------------+-
//        |7 6 5 4 3 2 1 0|7 6 5 4 3 2 1 0|7 ...
//        +---------------+---------------+-
//   bit   0 0 0 0 0 0 0 0 0 0 1 1 1 1 1 1 1
//         0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 ...
//
// A bitstream.Reader supports reading bits from a stream of bytes supplied by
// an io.Reader.
//
// Reader example (leaving out error checking):
//
//   input := strings.NewReader("\xa9") // == 1010 1001
//   br := bitstream.NewReader(input)
//   var hi, mid, lo uint64
//   br.Read(1, &hi)   // hi  == 1
//   br.Read(3, &mid)  // mid == 2
//   br.Read(4, &lo)   // lo  == 9
//
package bitstream

import (
	"encoding/binary"
	"errors"
	"io"
)

// A Reader supports reading groups of 0..64 bits from the data supplied by
// an io.Reader.
type Reader struct {
	r io.Reader // source of additional input

	// The low-order bits of buf holds data read from r but not yet delivered to
	// the reader.  The bottom nb bits of buf are valid, the rest is garbage.
	buf uint64
	nb  uint8 // 0 ≤ nb ≤ 64
}

// ErrCountRange is returned when a bit count is < 0 or > 64.
var ErrCountRange = errors.New("count is out of range")

// Read reads the next (up to) count bits from the reader.  If v != nil, the
// bits are copied into *v, where they occupy the low-order count bits.  In any
// case, the number of bits read is returned.  It is an error if count < 0 or
// count > 64.
//
// If err == nil, n == count.
// If err == io.EOF, 0 ≤ n < count.
// For any other error, n == 0.
//
func (r *Reader) Read(count int, v *uint64) (n int, err error) {
	if count < 0 || count > 64 {
		return 0, ErrCountRange
	}
	ucount := uint8(count)

	out := r.buf & ((1 << r.nb) - 1)
	if ucount <= r.nb {
		// We have enough already buffered to satisfy this request.
		if v != nil {
			*v = out >> (r.nb - ucount)
		}
		r.nb -= ucount
		return count, nil
	}

	nbits := r.nb // how many bits we have copied out

	// Read in 8 more bytes to refill r.buf.  If this fails for any reason
	// except reaching EOF, we return the error without consuming anything.
	//
	// To simplify decoding, the buffer is pre-padded with zeroes.  On a short
	// read, we use the padding to zero-fill the slice passed to the decoder.

	buf := make([]byte, 16) // |...8 zeroes...|...8 buffer bytes...|
	nr, err := io.ReadFull(r.r, buf[8:])
	switch err {
	case nil, io.EOF, io.ErrUnexpectedEOF:
		// Despite the name, ErrUnexpectedEOF is not unexpected here; it just
		// means we got a short read.  We'll treat that as EOF if we wind up
		// having to short the caller.
		err = nil

		r.buf = binary.BigEndian.Uint64(buf[nr:])
		r.nb = 8 * uint8(nr)

		nleft := ucount - nbits // how many bits we still need to copy
		if nleft > r.nb {
			nleft = r.nb
			err = io.EOF // report a short return
		}

		out = (out << nleft) | (r.buf >> (r.nb - nleft))
		r.nb -= nleft
		nbits += nleft

	default:
		return 0, err
	}

	if v != nil {
		*v = out
	}
	return int(nbits), err
}

// NewReader returns a bitstream reader that consumes data from r.
func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

/*
Design notes:


*/
