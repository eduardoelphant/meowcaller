//go:build !cgo

package opus

import "errors"

// Available reports whether this build links libopus (cgo builds only).
const Available = false

// ErrUnavailable is returned by every constructor in builds without cgo.
var ErrUnavailable = errors.New("opus: built without cgo")

// Encoder is a stub in builds without cgo; NewEncoder always fails.
type Encoder struct{}

// NewEncoder always returns ErrUnavailable in builds without cgo.
func NewEncoder(sampleRate, bitrate, complexity int, dtx bool) (*Encoder, error) {
	return nil, ErrUnavailable
}

// Encode always returns ErrUnavailable in builds without cgo.
func (e *Encoder) Encode(pcm []float32) ([]byte, error) {
	return nil, ErrUnavailable
}

// PacketSamples always returns ErrUnavailable in builds without cgo.
func PacketSamples(pkt []byte, sampleRate int) (int, error) {
	return 0, ErrUnavailable
}

// Decoder is a stub in builds without cgo; NewDecoder always fails.
type Decoder struct{}

// NewDecoder always returns ErrUnavailable in builds without cgo.
func NewDecoder(sampleRate int) (*Decoder, error) {
	return nil, ErrUnavailable
}

// Decode always returns nil in builds without cgo.
func (d *Decoder) Decode(pkt []byte) []float32 {
	return nil
}
