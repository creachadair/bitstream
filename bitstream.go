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
//   br.ReadBits(1, &hi)   // hi  == 1
//   br.ReadBits(3, &mid)  // mid == 2
//   br.ReadBits(4, &lo)   // lo  == 9
//
// A bitstream.Writer supports writing bits to a stream of bytes consumed by an
// io.Writer.
//
// Writer example (leaving out error checking):
//
//   var output bytes.Buffer
//   bw := bitstream.NewWriter(&output)
//   bw.WriteBits(1, 1)
//   bw.WriteBits(3, 2)
//   bw.WriteBits(4, 9)
//   // buf.String() == "\xa9"
//
package bitstream

import (
	"encoding/binary"
	"errors"
	"io"
)

// A Reader supports reading groups of 0 to 64 bits from the data supplied by
// an io.Reader.
//
// The primary interface to a bitstream.Reader is the ReadBits method, but as a
// convenience a *Reader also itself implements io.Reader.
type Reader struct {
	r io.Reader // source of additional input

	// The low-order nb bits of buf hold data read from r but not yet delivered
	// to the reader.  Any bits with index ≥ nb are garbage.
	buf uint64
	nb  uint8 // 0 ≤ nb ≤ 64
}

// ErrCountRange is returned when a bit count is < 0 or > 64.
var ErrCountRange = errors.New("count is out of range")

// ReadBits reads the next (up to) count bits from the reader.  If v != nil,
// the bits are copied into *v, where they occupy the low-order count bits.  In
// any case, the number of bits read is returned.  It is an error if count < 0
// or count > 64.
//
// If err == nil, n == count.
// If err == io.EOF, 0 ≤ n < count.
// For any other error, n == 0.
//
func (r *Reader) ReadBits(count int, v *uint64) (n int, err error) {
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

// A Writer supports writing groups of 0 to 64 bits to an underlying io.Writer.
// Writes are buffered, so the caller must call Flush when finished to ensure
// everything has been written out.
//
// The primary interface to a bitstream.Writer is the WriteBits method, but as
// a convenience a *Writer also implements io.Writer.
type Writer struct {
	w io.Writer

	// The low-order nb bits of buf hold the bits that have been received by
	// calls to Write but not yet delivered to w.  We maintain the invariant
	// that buf always has < 64 bits; when it reaches 64 we write immediately.
	// Any bits with index ≥ nb are garbage.
	buf uint64
	nb  uint8 // 0 ≤ nb < 64
}

// WriteBits appends the low-order count bits of v to the stream, and returns
// the number of bits written.  It is an error if count < 0 or count > 64.
//
// Any other error is the result of a call to the underlying io.Writer.  When
// that occurs, the write is abandoned and no bits are added to the stream.
// For diagnostic purposes, the returned count is the byte count returned by
// the failed io.Writer, but it is effectively 0 for the bitstream.
func (w *Writer) WriteBits(count int, v uint64) (int, error) {
	if count < 0 || count > 64 {
		return 0, ErrCountRange
	}
	ucount := uint8(count)

	// Shift in as much of the input as possible.  There is always at least one
	// bit free in the buffer.
	n2copy := 64 - w.nb
	if n2copy > ucount {
		n2copy = ucount
	}
	nleft := ucount - n2copy // how many bits remain in v after packing
	out := w.buf<<n2copy | v>>nleft
	nused := w.nb + n2copy // how many bits of out are in-use

	// If the buffer is full, send it to the underlying writer.  If that
	// succeeds, we definitely have room for any remaining bits.
	if nused == 64 {
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, out)
		nw, err := w.w.Write(buf)
		if err != nil {
			return nw, err // write failed; don't update anything
		}
		out = v
		nused = nleft
	}

	w.buf = out
	w.nb = nused
	return count, nil
}

// Padding returns the number 0 ≤ n < 8 of additional bits that would have to
// be written to w to ensure that the output is an even number of 8-bit bytes.
func (w *Writer) Padding() int {
	if p := w.nb % 8; p != 0 {
		return int(8 - p)
	}
	return 0
}

// Flush writes any unwritten data remaining in w to the underlying writer.  If
// the data remaining do not comprise a round number of bytes, they are padded
// with zeroes to the next byte boundary.
func (w *Writer) Flush() error {
	if w.nb != 0 {
		out := w.buf << uint(w.Padding())
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, out)

		// When flushing, the buffer may not be full; so skip any leading bytes
		// that are not part of the padded output.
		skip := 8 - (w.nb+7)/8
		if _, err := w.w.Write(buf[skip:]); err != nil {
			return err
		}
		w.nb = 0
	}
	return nil
}

// NewWriter returns a bitstream writer that delivers output to w.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }
