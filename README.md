# Bitstream

http://godoc.org/github.com/creachadair/bitstream

Package `bitstream` is a library for reading and writing streams of bits.

A `bitstream.Reader` supports reading variable-width bit fields sequentially
out of a stream of bytes supplied by an
[`io.Reader`](http://godoc.org/io#Reader).

A `bitstream.Writer` supports writing variable-width bit fields sequentially to
a stream of bytes consumed by an [`io.Writer`](http://godoc.org/io#Writer).

These types are useful for processing data that are not divided on even byte
boundaries, such as compressed or bit-packed data.  This package only supports
sequential processing, not random-access.

Bit values are exchanged as `uint64` values, with the data packed into the
low-order bits of the word.
