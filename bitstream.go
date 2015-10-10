// Package bitstream is a library for manipulating bit streams.
//
// A stream represents an unbounded sequence of binary digits, indexed from 0.
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
//   bw.WriteBits(2, 1) // 01
//   bw.WriteBits(4, 0) //   0000
//   bw.WriteBits(2, 1) //       01
//   bw.Flush()
//   // output.String() == "A"
//
// When a stream is encoded as bytes for I/O, the bits may be packed into bytes
// either from most to least significant, or vice versa.  This behaviour can be
// controlled by the bitstream.MSBFirst and bitstream.LSBFirst options.
//
package bitstream

import (
	"encoding/binary"
	"errors"
	"io"
)

type options struct {
	flipBits func([]byte) []byte // bit inverter
}

// An Option configures the behaviour of a Reader or Writer.
type Option func(*options)

func newOptions(opts []Option) *options {
	o := options{flipBits: noFlip}
	for _, opt := range opts {
		opt(&o)
	}
	return &o
}

// MSBFirst is an Option to pack the bits of each byte from most to least
// significant, i.e., the bit sequence 0 1 0 0 1 1 0 1 would be packed into a
// single byte with value 0x4D.  This is the default if no options are given.
func MSBFirst(o *options) { o.flipBits = noFlip }

// LSBFirst is an Option to pack the bits of each byte from least to most
// significant, i.e., the bit sequence 0 1 0 0 1 1 0 1 would be packed into a
// single byte with value 0xB2.
func LSBFirst(o *options) { o.flipBits = flipBits }

// A Reader supports reading groups of 0 to 64 bits from the data supplied by
// an io.Reader.
//
// The primary interface to a bitstream.Reader is the ReadBits method, but as a
// convenience a *Reader also itself implements io.Reader.
type Reader struct {
	r    io.Reader           // source of additional input
	flip func([]byte) []byte // bit inverter

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

		r.buf = binary.BigEndian.Uint64(r.flip(buf[nr:]))
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
func NewReader(r io.Reader, opts ...Option) *Reader {
	o := newOptions(opts)
	return &Reader{r: r, flip: o.flipBits}
}

// A Writer supports writing groups of 0 to 64 bits to an underlying io.Writer.
// Writes are buffered, so the caller must call Flush when finished to ensure
// everything has been written out.
//
// The primary interface to a bitstream.Writer is the WriteBits method, but as
// a convenience a *Writer also implements io.Writer.
type Writer struct {
	w    io.Writer
	flip func([]byte) []byte // bit inverter

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
		nw, err := w.w.Write(w.flip(buf))
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
		if _, err := w.w.Write(w.flip(buf[skip:])); err != nil {
			return err
		}
		w.nb = 0
	}
	return nil
}

// NewWriter returns a bitstream writer that delivers output to w.
func NewWriter(w io.Writer, opts ...Option) *Writer {
	o := newOptions(opts)
	return &Writer{w: w, flip: o.flipBits}
}

// bitReverse maps each byte value to its bit reversal.
var bitReverse = []byte{
	0x00, 0x80, 0x40, 0xc0, 0x20, 0xa0, 0x60, 0xe0,
	0x10, 0x90, 0x50, 0xd0, 0x30, 0xb0, 0x70, 0xf0,
	0x08, 0x88, 0x48, 0xc8, 0x28, 0xa8, 0x68, 0xe8,
	0x18, 0x98, 0x58, 0xd8, 0x38, 0xb8, 0x78, 0xf8,
	0x04, 0x84, 0x44, 0xc4, 0x24, 0xa4, 0x64, 0xe4,
	0x14, 0x94, 0x54, 0xd4, 0x34, 0xb4, 0x74, 0xf4,
	0x0c, 0x8c, 0x4c, 0xcc, 0x2c, 0xac, 0x6c, 0xec,
	0x1c, 0x9c, 0x5c, 0xdc, 0x3c, 0xbc, 0x7c, 0xfc,
	0x02, 0x82, 0x42, 0xc2, 0x22, 0xa2, 0x62, 0xe2,
	0x12, 0x92, 0x52, 0xd2, 0x32, 0xb2, 0x72, 0xf2,
	0x0a, 0x8a, 0x4a, 0xca, 0x2a, 0xaa, 0x6a, 0xea,
	0x1a, 0x9a, 0x5a, 0xda, 0x3a, 0xba, 0x7a, 0xfa,
	0x06, 0x86, 0x46, 0xc6, 0x26, 0xa6, 0x66, 0xe6,
	0x16, 0x96, 0x56, 0xd6, 0x36, 0xb6, 0x76, 0xf6,
	0x0e, 0x8e, 0x4e, 0xce, 0x2e, 0xae, 0x6e, 0xee,
	0x1e, 0x9e, 0x5e, 0xde, 0x3e, 0xbe, 0x7e, 0xfe,
	0x01, 0x81, 0x41, 0xc1, 0x21, 0xa1, 0x61, 0xe1,
	0x11, 0x91, 0x51, 0xd1, 0x31, 0xb1, 0x71, 0xf1,
	0x09, 0x89, 0x49, 0xc9, 0x29, 0xa9, 0x69, 0xe9,
	0x19, 0x99, 0x59, 0xd9, 0x39, 0xb9, 0x79, 0xf9,
	0x05, 0x85, 0x45, 0xc5, 0x25, 0xa5, 0x65, 0xe5,
	0x15, 0x95, 0x55, 0xd5, 0x35, 0xb5, 0x75, 0xf5,
	0x0d, 0x8d, 0x4d, 0xcd, 0x2d, 0xad, 0x6d, 0xed,
	0x1d, 0x9d, 0x5d, 0xdd, 0x3d, 0xbd, 0x7d, 0xfd,
	0x03, 0x83, 0x43, 0xc3, 0x23, 0xa3, 0x63, 0xe3,
	0x13, 0x93, 0x53, 0xd3, 0x33, 0xb3, 0x73, 0xf3,
	0x0b, 0x8b, 0x4b, 0xcb, 0x2b, 0xab, 0x6b, 0xeb,
	0x1b, 0x9b, 0x5b, 0xdb, 0x3b, 0xbb, 0x7b, 0xfb,
	0x07, 0x87, 0x47, 0xc7, 0x27, 0xa7, 0x67, 0xe7,
	0x17, 0x97, 0x57, 0xd7, 0x37, 0xb7, 0x77, 0xf7,
	0x0f, 0x8f, 0x4f, 0xcf, 0x2f, 0xaf, 0x6f, 0xef,
	0x1f, 0x9f, 0x5f, 0xdf, 0x3f, 0xbf, 0x7f, 0xff,
}

// flipBits reverses the bits of each element of data in-place.
// Returns its argument.
func flipBits(data []byte) []byte {
	for i, b := range data {
		data[i] = bitReverse[int(b)]
	}
	return data
}

func noFlip(data []byte) []byte { return data }
