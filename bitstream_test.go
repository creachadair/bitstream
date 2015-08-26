package bitstream

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// A bit stream for testing, constructed by concatenating the binary encodings
// of the integers 0 ≤ i < 16, plus a 6-bit tail with value 16.
//   v  vv   vvv   vvv    vvvv    vvvv    vvvv    vvvv
//  01101110010111011110001001101010111100110111101111010000
//  ^ ^^  ^^^   ^^^   ^^^^    ^^^^    ^^^^    ^^^^    ^^^^^^
//
// In hex, MSB first:
//  0110 1110 0101 1101 1110 0010 0110 1010 1111 0011 0111 1011 1101 0000
//  6    E    5    D    E    2    6    A    F    3    7    B    D    0
//
// In hex, LSB first:
//  0111 0110 1011 1010 0100 0111 0101 0110 1100 1111 1101 1110 0000 1011
//  7    6    B    A    4    7    5    6    C    F    D    E    0    B
const msbTestStream = "\x6e\x5d\xe2\x6a\xf3\x7b\xd0"
const lsbTestStream = "\x76\xba\x47\x56\xcf\xde\x0b"

func flipped(s string) string { return string(flipBits([]byte(s))) }

func TestReader(t *testing.T) {
	rmsb := NewReader(strings.NewReader(msbTestStream), MSBFirst)
	rlsb := NewReader(strings.NewReader(lsbTestStream), LSBFirst)

	// Each "test" is a number of bits to read.  The desired value of the read
	// is the index of the test (i.e., for test[i] we want the value i).
	tests := []int{1, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 6}

	for _, r := range []*Reader{rmsb, rlsb} {
		for want, n := range tests {
			var got uint64
			nr, err := r.ReadBits(n, &got)
			t.Logf("ReadBits(%d, &v) :: nr=%d, err=%v, value=%d", n, nr, err, got)
			if err != nil {
				t.Errorf("ReadBits(%d, &v): unexpected error: %v", n, err)
				continue
			}
			if nr != n {
				t.Errorf("ReadBits(%d, &v): read %d bits, wanted %d", n, nr, n)
			}
			if got != uint64(want) {
				t.Errorf("ReadBits(%d, &v): got value %d, want %d", n, got, want)
			}
		}
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
	r := NewReader(newErrReader("blah", errors.New("bogus")))

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
		t.Errorf("Read(64, &v): got error %v, want %v", err, io.EOF)
	}
	if nr != 32 {
		t.Errorf("Read(64, &v): read %d bits, wanted %d", nr, 32)
	}

	// An error other than io.EOF does not consume any data.
	// This test mucks with the internals to simulate a failure.
	if nr, err := r.ReadBits(4, &got); err == nil {
		t.Errorf("Read(4, &v): got nr=%d, value=%d; wanted error", nr, got)
	} else if err.Error() != "bogus" {
		t.Errorf("Read(4, &v): unexpected error %v", err)
	}
}

func TestWriter(t *testing.T) {
	var mbuf, lbuf bytes.Buffer
	wmsb := NewWriter(&mbuf, MSBFirst)
	wlsb := NewWriter(&lbuf, LSBFirst)

	// Each "test" is a number of bits to write.  The value to write is the
	// index of the test (i.e., for test[i] we write the value i).
	tests := []int{1, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 6}

	for _, w := range []*Writer{wmsb, wlsb} {
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
		// Flush out the stream and make sure we got the predicted value.
		if err := w.Flush(); err != nil {
			t.Errorf("Flush: unexpected error: %v", err)
		}
	}
	if got := mbuf.String(); got != msbTestStream {
		t.Errorf("Write MSB first: got %q, want %q", got, msbTestStream)
	}
	if got := lbuf.String(); got != lsbTestStream {
		t.Errorf("Write LSB first: got %q, want %q", got, lsbTestStream)
	}
}

// errWriter is a fake io.Writer that always returns an error.
type errWriter string

func (e errWriter) Write([]byte) (int, error) { return 0, errors.New(string(e)) }

func TestWriterErrors(t *testing.T) {
	fail := errWriter("bogus")
	w := NewWriter(fail, LSBFirst)

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
	const baseValue = "0123456789abcd"
	tests := []struct {
		input string
		opt   Option
	}{
		{baseValue, MSBFirst},
		{flipped(baseValue), LSBFirst},
	}
	for _, test := range tests {
		buf := make([]byte, 2*len(test.input))
		for n := 0; n < 2*len(test.input); n++ { // × 2 to ensure it works for long reads
			r := NewReader(strings.NewReader(test.input), test.opt)
			nr, err := r.Read(buf[:n])
			if err != nil {
				if n < len(test.input) || err != io.EOF {
					t.Errorf("ReadBytes(r, #%d) from %q: unexpected error: %v", n, test.input, err)
				}
			}
			want := baseValue
			if n < len(baseValue) {
				want = want[:n]
			}
			if got := string(buf[:nr]); got != want {
				t.Errorf("ReadBytes(r, #%d) from %q: got %q, want %q", n, test.input, got, want)
			}
		}
	}
}

func TestWriteBytes(t *testing.T) {
	var mbuf, lbuf bytes.Buffer
	wmsb := NewWriter(&mbuf, MSBFirst)
	wlsb := NewWriter(&lbuf, LSBFirst)

	const input = "Tis true, tis pity; and pity tis, tis true."
	for _, w := range []*Writer{wmsb, wlsb} {
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
	}
	if got := mbuf.String(); got != input {
		t.Errorf("Write MSB first: got %q, want %q", got, input)
	}
	if got, want := lbuf.String(), flipped(input); got != want {
		t.Errorf("Write LSB first: got %q, want %q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, input := range []string{"", "a", "ab", "abc", "abcdefghijklmopqrstuv", "01a23"} {
		for _, opt := range []Option{MSBFirst, LSBFirst} {
			var buf bytes.Buffer

			// Write the input out to the buffer.
			w := NewWriter(&buf, opt)
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

			// Read the buffered input back with the same settings.
			r := NewReader(&buf, opt)
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
}

func ExampleReadBits() {
	input := strings.NewReader("\xa9") // == 1010 1001
	br := NewReader(input)
	var hi, mid, lo uint64
	br.ReadBits(1, &hi)  // hi  == 1
	br.ReadBits(3, &mid) // mid == 2
	br.ReadBits(4, &lo)  // lo  == 9
	fmt.Printf("hi=%d mid=%d lo=%d", hi, mid, lo)
	// Output: hi=1 mid=2 lo=9
}

func ExampleWriteBits() {
	var output bytes.Buffer
	bw := NewWriter(&output)
	bw.WriteBits(2, 1)
	bw.WriteBits(4, 0)
	bw.WriteBits(2, 1)
	bw.Flush()
	fmt.Print(output.String())
	// Output: A
}
