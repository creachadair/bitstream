package bitstream

import (
	"encoding/binary"
	"io"
)

// Read reads an arbitrary number of bytes from a bitstream.Reader.  It
// implements io.Reader, so it returns the total number of bytes read.  If r
// did not contain a round number of bytes, the final byte is padded with
// zeroes in its low-order bits.
func (r *Reader) Read(data []byte) (int, error) {
	var (
		v     uint64 // value read from the stream
		nread int    // total bytes read
		err   error

		pos = 0               // byte offset into data
		buf = make([]byte, 8) // temporary buffer for decoding
	)
	for pos < len(data) && err != io.EOF {
		next := pos + 8
		if next > len(data) {
			next = len(data)
		}
		want := 8 * (next - pos)
		var nbits int
		nbits, err = r.ReadBits(want, &v)
		v <<= 64 - uint(nbits) // zero-fill the low-order bits
		if err != nil && err != io.EOF {
			return nread, err
		}

		// Unpack the value into the temporary buffer.  We can't safely unpack
		// directly into data because it might not have enough room.
		binary.BigEndian.PutUint64(buf, v)
		ncopied := bitsToBytes(nbits)
		copy(data[pos:], buf[:ncopied])
		pos = next
		nread += ncopied
	}
	return nread, err
}

// Write writes an an arbitrary number of bytes to a bitstream.Writer.  It
// implements io.Writer, so it returns the total number of bytes written,
// rounded up.  This function does not flush w.
func (w *Writer) Write(data []byte) (int, error) {
	pos := 0   // offset into data of next unwritten byte
	nbits := 0 // number of bits written

	// Handle all the full-size chunks, if any.
	for pos+8 < len(data) {
		v := binary.BigEndian.Uint64(data[pos:])
		nw, err := w.WriteBits(64, v)
		if err != nil {
			return bitsToBytes(nbits), err
		}
		nbits += nw
		pos += 8
	}

	// Handle any leftovers.
	if pos < len(data) {
		var v uint64
		for _, b := range data[pos:] {
			v = (v << 8) | uint64(b)
		}
		nw, err := w.WriteBits(8*len(data[pos:]), v)
		if err != nil {
			return bitsToBytes(nbits), err
		}
		nbits += nw
	}

	return bitsToBytes(nbits), nil
}

func bitsToBytes(n int) int { return (n + 7) / 8 }
