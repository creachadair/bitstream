package bitstream

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// This package relies on append growing slices by doubling their size.
// They're not required to do that by the spec, so if this test fails, it means
// they've changed the implementation and we must re-implement the behaviour.
func TestSliceGrowth(t *testing.T) {
	var s []int
	next := 1
	for i := 0; i < 800; i++ {
		if i == next {
			t.Logf("at i=%d, cap(s)=%d", i, cap(s))
			if cap(s) != next {
				t.Errorf("cap: got %d, want %d", cap(s), next)
			}
			next *= 2
		}
		s = append(s, i)
	}
	t.Logf("at exit, len(s)=%d, cap(s)=%d", len(s), cap(s))
	if cap(s) != next {
		t.Errorf("final cap: got %d, want %d", cap(s), next)
	}
}

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

type errReader string

func (e errReader) Read([]byte) (int, error) { return 0, errors.New(string(e)) }

func TestReaderErrors(t *testing.T) {
	in := strings.NewReader("blah")
	r := NewReader(in)

	var got uint64

	// Bounds checking on the count value.
	if nr, err := r.Read(-1, &got); err == nil {
		t.Errorf("Read(-1, &v): got nr=%d, value=%d; wanted error", nr, got)
	}
	if nr, err := r.Read(65, &got); err == nil {
		t.Errorf("Read(65, &v): got nr=%d, value=%d; wanted error", nr, got)
	}

	// An error other than io.EOF does not consume any data.
	// This test mucks with the internals to simulate a failure.
	r.r = errReader("bogus")
	if nr, err := r.Read(4, &got); err == nil {
		t.Errorf("Read(4, &v): got nr=%d, value=%d; wanted error", nr, got)
	} else if err.Error() != "bogus" {
		t.Error("Read(4, &v): unexpected error %v", err)
	}
	r.r = in // restore "sanity"

	// A short read returns the available data and io.EOF.
	nr, err := r.Read(64, &got)
	if err != io.EOF {
		t.Error("Read(64, &v): got error %v, want %v", err, io.EOF)
	}
	if nr != 32 {
		t.Errorf("Read(64, &v): read %d bits, wanted %d", nr, 32)
	}
}
