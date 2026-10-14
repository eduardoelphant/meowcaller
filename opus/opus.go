// Package opus is a minimal cgo binding to libopus for WhatsApp calls: mono float
// encode and decode at one sample rate. Elphant fork addition, not part of meowcaller.
package opus

/*
#cgo pkg-config: opus
#include <opus.h>
static int enc_set_bitrate(OpusEncoder *e, opus_int32 v) { return opus_encoder_ctl(e, OPUS_SET_BITRATE(v)); }
static int enc_set_complexity(OpusEncoder *e, opus_int32 v) { return opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(v)); }
static int enc_set_dtx(OpusEncoder *e, opus_int32 v) { return opus_encoder_ctl(e, OPUS_SET_DTX(v)); }
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

const maxPacketBytes = 1275

// Encoder encodes mono float32 frames.
type Encoder struct {
	enc *C.OpusEncoder
	out []byte
}

// NewEncoder creates a VoIP encoder. Frames passed to Encode must be a valid Opus
// frame size for sampleRate (960 samples is 60 ms at 16 kHz).
func NewEncoder(sampleRate, bitrate, complexity int, dtx bool) (*Encoder, error) {
	var cerr C.int
	enc := C.opus_encoder_create(C.opus_int32(sampleRate), 1, C.OPUS_APPLICATION_VOIP, &cerr)
	if cerr != C.OPUS_OK || enc == nil {
		return nil, fmt.Errorf("opus: encoder create: %d", int(cerr))
	}
	e := &Encoder{enc: enc, out: make([]byte, maxPacketBytes)}
	runtime.SetFinalizer(e, func(e *Encoder) { C.opus_encoder_destroy(e.enc) })
	d := C.opus_int32(0)
	if dtx {
		d = 1
	}
	if C.enc_set_bitrate(enc, C.opus_int32(bitrate)) != C.OPUS_OK ||
		C.enc_set_complexity(enc, C.opus_int32(complexity)) != C.OPUS_OK ||
		C.enc_set_dtx(enc, d) != C.OPUS_OK {
		return nil, fmt.Errorf("opus: encoder ctl failed")
	}
	return e, nil
}

// Encode encodes one frame and returns a new packet slice.
func (e *Encoder) Encode(pcm []float32) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("opus: empty frame")
	}
	n := C.opus_encode_float(e.enc, (*C.float)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)),
		(*C.uchar)(unsafe.Pointer(&e.out[0])), C.opus_int32(len(e.out)))
	runtime.KeepAlive(pcm)
	if n < 0 {
		return nil, fmt.Errorf("opus: encode: %d", int(n))
	}
	return append([]byte(nil), e.out[:n]...), nil
}

// Decoder decodes packets to mono float32.
type Decoder struct {
	dec *C.OpusDecoder
	buf []float32
}

// NewDecoder creates a decoder; output holds up to 120 ms per packet.
func NewDecoder(sampleRate int) (*Decoder, error) {
	var cerr C.int
	dec := C.opus_decoder_create(C.opus_int32(sampleRate), 1, &cerr)
	if cerr != C.OPUS_OK || dec == nil {
		return nil, fmt.Errorf("opus: decoder create: %d", int(cerr))
	}
	d := &Decoder{dec: dec, buf: make([]float32, sampleRate*120/1000)}
	runtime.SetFinalizer(d, func(d *Decoder) { C.opus_decoder_destroy(d.dec) })
	return d, nil
}

// Decode returns the decoded samples, or nil when the packet is empty or invalid.
func (d *Decoder) Decode(pkt []byte) []float32 {
	if len(pkt) == 0 {
		return nil
	}
	n := C.opus_decode_float(d.dec, (*C.uchar)(unsafe.Pointer(&pkt[0])), C.opus_int32(len(pkt)),
		(*C.float)(unsafe.Pointer(&d.buf[0])), C.int(len(d.buf)), 0)
	runtime.KeepAlive(pkt)
	if n <= 0 {
		return nil
	}
	return append([]float32(nil), d.buf[:n]...)
}
