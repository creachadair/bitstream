package bitstream

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"bitbucket.org/creachadair/bitstream"
)

// A bit stream for testing, constructed from the concatenation of the binary
// encodings of the integers 0 ≤ i < 16.
//
//  0 1 10 11 100 101 110 111 1000 1001 1010 1011 1100 1101 1110 1111 00 0100
//  \----------/\---------/\--------/\--------/\--------/\--------/\--------/
//
// When bit-reversed:
//  = 0111 0110   1011 1010  0100 0111 0101 0110 1100 1111 1101 1110 0010 0011
//  = 7    6      B    A     4    7    5    6    C    F    D    E    2    3
const testStream = "\x76\xBA\x47\x56\xCF\xDE\x23"

func TestReader(t *testing.T) {
	in := strings.NewReader(testStream)
	r := NewReader(in)

	// Each "test" is a number of bits to read.  The desired value of the read
	// is the index of the test (i.e., for test[i] we want the value i).
	tests := []int{1, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4}

	for want, n := range tests {
		var got uint64
		nr, err := r.ReadBits(n, &got)
		t.Logf("ReadBits(%d, &v) :: nr=%d, err=%v, value=%d", n, nr, err, got)
		if err != nil {
			t.Errorf("ReadBits(%d, &v): unexpected error: %v", n, err)
			continue
		}
		if nr != n {
			t.Errorf("ReadBits(%d, &v): read %d bits, wanted %d", nr, n)
		}
		if got != uint64(want) {
			t.Errorf("ReadBits(%d, &v): got value %d, want %d", n, got, want)
		}
	}

	// There should be six bits left in the reader; make sure we correctly
	// handle the boundary case.
	const askFor, bitsLeft, wantValue = 30, 6, 4
	var got uint64
	nr, err := r.ReadBits(askFor, &got) // ask for more than there are
	if err != io.EOF {
		t.Errorf("ReadBits(%d, &v): got err=%v, wanted io.EOF", askFor, err)
	}
	if nr != bitsLeft {
		t.Errorf("ReadBits(%d, &v): got %d bits, wanted %d", askFor, nr, bitsLeft)
	}
	if got != wantValue {
		t.Errorf("ReadBits(%d, &v): got value %d, want %d", askFor, got, wantValue)
	}
}

// errReader is a fake io.Reader that returns all the data from a fixed
// string, and any subsequent reads receive an error.
type errReader struct {
	buf *strings.Reader
	err error
}

func newErrReader(s string, err error) io.Reader {
	return &errReader{
		buf: strings.NewReader(s),
		err: err,
	}
}

func (e *errReader) Read(data []byte) (int, error) {
	if e.buf == nil {
		return 0, e.err
	}
	nr, err := e.buf.Read(data)
	if err == io.EOF {
		e.buf = nil // all done reading; fail after this
	}
	return nr, err
}

func TestReaderErrors(t *testing.T) {
	in := newErrReader("blah", errors.New("bogus"))
	r := NewReader(in)

	var got uint64

	// Bounds checking on the count value.
	if nr, err := r.ReadBits(-1, &got); err == nil {
		t.Errorf("Read(-1, &v): got nr=%d, value=%d; wanted error", nr, got)
	}
	if nr, err := r.ReadBits(65, &got); err == nil {
		t.Errorf("Read(65, &v): got nr=%d, value=%d; wanted error", nr, got)
	}

	// A short read returns the available data and io.EOF.
	nr, err := r.ReadBits(64, &got)
	if err != io.EOF {
		t.Error("Read(64, &v): got error %v, want %v", err, io.EOF)
	}
	if nr != 32 {
		t.Errorf("Read(64, &v): read %d bits, wanted %d", nr, 32)
	}

	// An error other than io.EOF does not consume any data.
	// This test mucks with the internals to simulate a failure.
	if nr, err := r.ReadBits(4, &got); err == nil {
		t.Errorf("Read(4, &v): got nr=%d, value=%d; wanted error", nr, got)
	} else if err.Error() != "bogus" {
		t.Error("Read(4, &v): unexpected error %v", err)
	}
}

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// Each "test" is a number of bits to write.  The value to write is the
	// index of the test (i.e., for test[i] we write the value i).
	// This should produce testStream.
	tests := []int{1, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4}

	for value, bits := range tests {
		got, err := w.WriteBits(bits, uint64(value))
		t.Logf("Write(%d, %v) :: nw=%d, err=%v", bits, value, got, err)
		if err != nil {
			t.Errorf("Write(%d, %d): unexpected error: %v", bits, value, err)
			continue
		}
		if got != bits {
			t.Errorf("Write(%d, %d): wrote %d bits, wanted %d", bits, value, got, bits)
		}
	}

	// Write the 6-bit coda...
	const codaBits, codaValue = 6, 4
	if nw, err := w.WriteBits(codaBits, codaValue); err != nil {
		t.Errorf("Write(%d, %d): unexpected error: %v", codaBits, codaValue, err)
	} else if nw != codaBits {
		t.Errorf("Write(%d, %d): wrote %d bits, wanted 6", codaBits, codaValue, nw)
	}

	// Flush out the stream and make sure we got the predicted value.
	if err := w.Flush(); err != nil {
		t.Errorf("Flush: unexpected error: %v", err)
	}

	if got := buf.String(); got != testStream {
		t.Errorf("Final result: got %q, want %q", got, testStream)
	}
}

// errWriter is a fake io.Writer that always returns an error.
type errWriter string

func (e errWriter) Write([]byte) (int, error) { return 0, errors.New(string(e)) }

func TestWriterErrors(t *testing.T) {
	fail := errWriter("bogus")
	w := NewWriter(fail)

	// Bounds checking on the count value.
	if nw, err := w.WriteBits(-1, 0); err == nil {
		t.Errorf("Write(-1, 0): got nw=%d; wanted error", nw)
	}
	if nw, err := w.WriteBits(65, 0); err == nil {
		t.Errorf("Write(65, 0): got nw=%d; wanted error", nw)
	}

	const okBits = 5
	const failBits = 64 - 5
	const writeValue = 31

	// Writing without overflowing the internal buffer should not give an error.
	if nw, err := w.WriteBits(okBits, writeValue); err != nil {
		t.Errorf("Write(%d, %d): unexpected error: %v", okBits, writeValue, err)
	} else if nw != 5 {
		t.Errorf("Write(%d, %d): wrote %d bits, wanted %d", okBits, writeValue, nw, okBits)
	}
	saveBuf := w.buf
	saveBits := w.nb

	// Writing enough to trigger a "real" write should give back an error.
	if nw, err := w.WriteBits(failBits, 0); err == nil {
		t.Errorf("Write(%d, 0): got %d, expected error", failBits, nw)
	}

	// Having gotten that error, the state of the system should be unchanged.
	if w.buf != saveBuf {
		t.Errorf("Write error clobbered the buffer: got %x, want %x", w.buf, saveBuf)
	}
	if w.nb != saveBits {
		t.Errorf("Write error clobbered the bit count: got %d, want %d", w.nb, saveBits)
	}
}

func TestReadBytes(t *testing.T) {
	const original = "0123456789abcd"

	// When encoded, original will have its bits fipped.
	input := flipBits([]byte(original))

	for _, want := range []string{"", "0", "012", "0123456789", original} {
		in := bytes.NewReader(input)
		r := NewReader(in)

		buf := make([]byte, len(want))
		nr, err := r.Read(buf)
		if err != nil {
			t.Errorf("ReadBytes(r, #%d): unexpected error: %v", len(buf), err)
		}
		if nr != len(want) {
			t.Errorf("ReadBytes(r, #%d): got %d bytes, wanted %d", len(buf), nr, len(want))
		}
		if got := string(buf[:nr]); got != want {
			t.Errorf("ReadBytes(r, #%d): got %q, want %q", len(buf), got, want)
		}
	}

	// Make sure that requesting more than the stream has returns io.EOF and
	// consumes the entire input.
	in := bytes.NewReader(input)
	r := NewReader(in)
	buf := make([]byte, len(input)+10)
	nr, err := r.Read(buf)
	if err != io.EOF {
		t.Errorf("ReadBytes(r, #%d): got error %v, wanted %v", err, io.EOF)
	}
	if nr != len(input) {
		t.Errorf("ReadBytes(r, #%d): got %d bytes, wanted %d", len(buf), nr, len(input))
	}
	if got := string(buf[:nr]); got != original {
		t.Errorf("ReadBytes(r, #%d): got %q, want %q", len(buf), got, original)
	}
}

func TestWriteBytes(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	const input = "Tis true, tis pity; and pity tis, tis true."
	nw, err := w.Write([]byte(input))
	if err != nil {
		t.Errorf("WriteBytes(w, %q): unexpected error: %v", input, err)
	}
	if nw != len(input) {
		t.Errorf("WriteBytes(w, %q): wrote %d bits, wanted %d", input, nw, len(input))
	}
	if err := w.Flush(); err != nil {
		t.Errorf("Flush failed: unexpected error: %v", err)
	}

	// When encoded, the input will have its bits flipped.
	encoded := flipBits([]byte(input))
	if got := buf.Bytes(); !bytes.Equal(got, encoded) {
		t.Errorf("Final result: got %q, want %q", string(got), string(encoded))
	}
}

func TestRoundTrip(t *testing.T) {
	for _, input := range []string{"", "a", "ab", "abc", "abcdefghijklmopqrstuv", "01a23"} {
		var buf bytes.Buffer
		w := NewWriter(&buf)
		nw, err := w.Write([]byte(input))
		if err != nil {
			t.Errorf("Write %q failed: %v", input, err)
			continue
		}
		if nw != len(input) {
			t.Errorf("Write %q: wrote %d bytes, wanted %d", input, nw, len(input))
		}
		if err := w.Flush(); err != nil {
			t.Errorf("Flush failed: %v", err)
		}

		r := NewReader(&buf)
		got := make([]byte, len(input))
		nr, err := r.Read(got)
		if err != nil {
			t.Errorf("Read failed: %v", err)
			continue
		}
		if nr != len(input) {
			t.Errorf("Read returned %d bytes, wanted %d", nr, len(input))
		}
		if string(got) != input {
			t.Errorf("Read: got %q, want %q", string(got), input)
		}
	}
}

func ExampleReadBits() {
	input := strings.NewReader("\xa9") // == 1010 1001
	br := bitstream.NewReader(input)
	var hi, mid, lo uint64
	br.ReadBits(1, &hi)  // hi  == 1
	br.ReadBits(3, &mid) // mid == 2
	br.ReadBits(4, &lo)  // lo  == 9
	fmt.Printf("hi=%d mid=%d lo=%d", hi, mid, lo)
	// Output: hi=1 mid=2 lo=9
}

func ExampleWriteBits() {
	var output bytes.Buffer
	bw := bitstream.NewWriter(&output)
	bw.WriteBits(2, 1)
	bw.WriteBits(4, 0)
	bw.WriteBits(2, 1)
	bw.Flush()
	fmt.Print(output.String())
	// Output: A
}
