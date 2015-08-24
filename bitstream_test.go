package bitstream

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReader(t *testing.T) {
	// Input bit stream:
	// 0 1 10 11 100 101 110 111 1000 1001 1010 1011 1100 1101 1110 1111 00 0100
	// == 0110 1110 0101 1101 1110 0010 0110 1010 1111 0011 0111 1011 1100 0100
	// == 6    E    5    D    E    2    6    A    F    3    7    B    C    4
	in := strings.NewReader("\x6E\x5D\xE2\x6A\xF3\x7B\xC4")
	r := NewReader(in)

	// Each "test" is a number of bits to read.  The desired value of the read
	// is the index of the test (i.e., for test[i] we want the value i).
	tests := []int{1, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4}

	for want, n := range tests {
		var got uint64
		nr, err := r.Read(n, &got)
		t.Logf("Read(%d, &v) :: nr=%d, err=%v, value=%d", n, nr, err, got)
		if err != nil {
			t.Errorf("Read(%d, &v): unexpected error: %v", n, err)
			continue
		}
		if nr != n {
			t.Errorf("Read(%d, &v): read %d bits, wanted %d", nr, n)
		}
		if got != uint64(want) {
			t.Errorf("Read(%d, &v): got value %d, want %d", got, want)
		}
	}

	// There should be six bits left in the reader; make sure we correctly
	// handle the boundary case.
	const askFor, bitsLeft, wantValue = 30, 6, 4
	var got uint64
	nr, err := r.Read(askFor, &got) // ask for more than there are
	if err != io.EOF {
		t.Errorf("Read(%d, &v): got err=%v, wanted io.EOF", askFor, err)
	}
	if nr != bitsLeft {
		t.Errorf("Read(%d, &v): got %d bits, wanted %d", askFor, nr, bitsLeft)
	}
	if got != wantValue {
		t.Errorf("Read(%d, &v): got value %d, want %d", askFor, got, wantValue)
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
	if nr, err := r.Read(-1, &got); err == nil {
		t.Errorf("Read(-1, &v): got nr=%d, value=%d; wanted error", nr, got)
	}
	if nr, err := r.Read(65, &got); err == nil {
		t.Errorf("Read(65, &v): got nr=%d, value=%d; wanted error", nr, got)
	}

	// A short read returns the available data and io.EOF.
	nr, err := r.Read(64, &got)
	if err != io.EOF {
		t.Error("Read(64, &v): got error %v, want %v", err, io.EOF)
	}
	if nr != 32 {
		t.Errorf("Read(64, &v): read %d bits, wanted %d", nr, 32)
	}

	// An error other than io.EOF does not consume any data.
	// This test mucks with the internals to simulate a failure.
	if nr, err := r.Read(4, &got); err == nil {
		t.Errorf("Read(4, &v): got nr=%d, value=%d; wanted error", nr, got)
	} else if err.Error() != "bogus" {
		t.Error("Read(4, &v): unexpected error %v", err)
	}
}

func TestWriter(t *testing.T) {
	// Expected output bit stream:
	// 0 1 10 11 100 101 110 111 1000 1001 1010 1011 1100 1101 1110 1111 00 0100
	// == 0110 1110 0101 1101 1110 0010 0110 1010 1111 0011 0111 1011 1100 0100
	// == 6    E    5    D    E    2    6    A    F    3    7    B    C    4
	const want = "\x6E\x5D\xE2\x6A\xF3\x7B\xC4"
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// Each "test" is a number of bits to write.  The value to write is the
	// index of the test (i.e., for test[i] we write the value i).
	tests := []int{1, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4}

	for value, bits := range tests {
		got, err := w.Write(bits, uint64(value))
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
	if nw, err := w.Write(codaBits, codaValue); err != nil {
		t.Errorf("Write(%d, %d): unexpected error: %v", codaBits, codaValue, err)
	} else if nw != codaBits {
		t.Errorf("Write(%d, %d): wrote %d bits, wanted 6", codaBits, codaValue, nw)
	}

	// Flush out the stream and make sure we got the predicted value.
	if err := w.Flush(); err != nil {
		t.Errorf("Flush: unexpected error: %v", err)
	}

	if got := buf.String(); got != want {
		t.Errorf("Final result: got %q, want %q", got, want)
	}
}

// errWriter is a fake io.Writer that always returns an error.
type errWriter string

func (e errWriter) Write([]byte) (int, error) { return 0, errors.New(string(e)) }

func TestWriterErrors(t *testing.T) {
	fail := errWriter("bogus")
	w := NewWriter(fail)

	// Bounds checking on the count value.
	if nw, err := w.Write(-1, 0); err == nil {
		t.Errorf("Write(-1, 0): got nw=%d; wanted error", nw)
	}
	if nw, err := w.Write(65, 0); err == nil {
		t.Errorf("Write(65, 0): got nw=%d; wanted error", nw)
	}

	const okBits = 5
	const failBits = 64 - 5
	const writeValue = 31

	// Writing without overflowing the internal buffer should not give an error.
	if nw, err := w.Write(okBits, writeValue); err != nil {
		t.Errorf("Write(%d, %d): unexpected error: %v", okBits, writeValue, err)
	} else if nw != 5 {
		t.Errorf("Write(%d, %d): wrote %d bits, wanted %d", okBits, writeValue, nw, okBits)
	}
	saveBuf := w.buf
	saveBits := w.nb

	// Writing enough to trigger a "real" write should give back an error.
	if nw, err := w.Write(failBits, 0); err == nil {
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
