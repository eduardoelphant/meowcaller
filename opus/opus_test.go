//go:build cgo

package opus

import (
	"math"
	"testing"
)

func TestRoundTripKeepsFrameLength(t *testing.T) {
	enc, err := NewEncoder(16000, 24000, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewDecoder(16000)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]float32, 960)
	for i := range pcm {
		pcm[i] = 0.3 * float32((i%40)-20) / 20
	}
	pkt, err := enc.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkt) == 0 || len(pkt) > 1275 {
		t.Fatalf("packet length %d out of range", len(pkt))
	}
	if out := dec.Decode(pkt); len(out) != 960 {
		t.Fatalf("decoded %d samples, want 960", len(out))
	}
}

func TestDecodeGarbageReturnsNil(t *testing.T) {
	dec, err := NewDecoder(16000)
	if err != nil {
		t.Fatal(err)
	}
	if out := dec.Decode([]byte{0xff, 0xff, 0xff}); out != nil {
		t.Fatalf("want nil for an invalid packet, got %d samples", len(out))
	}
}

func TestPacketSamplesReportsFrameLength(t *testing.T) {
	enc, err := NewEncoder(16000, 24000, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := enc.Encode(make([]float32, 960))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := PacketSamples(pkt, 16000); err != nil || n != 960 {
		t.Fatalf("PacketSamples = %d, %v; want 960", n, err)
	}
	if _, err := PacketSamples(nil, 16000); err == nil {
		t.Fatal("PacketSamples accepted an empty packet")
	}
}

func TestStereoEncoderKeepsBothChannels(t *testing.T) {
	enc, err := NewEncoderChannels(16000, 2, 48000, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]float32, 2*960)
	for i := 0; i < 960; i++ {
		pcm[2*i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
		pcm[2*i+1] = 0.3 * float32(math.Sin(2*math.Pi*1300*float64(i)/16000))
	}
	pkt, err := enc.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	if pkt[0]&0x04 == 0 {
		t.Fatalf("TOC 0x%02x: stereo flag not set", pkt[0])
	}
	if n, err := PacketSamples(pkt, 16000); err != nil || n != 960 {
		t.Fatalf("packet holds %d samples per channel (%v), want 960", n, err)
	}
}

func TestEncoderChannelsRejectsBadInput(t *testing.T) {
	if _, err := NewEncoderChannels(16000, 3, 24000, 5, false); err == nil {
		t.Fatal("3 channels must fail")
	}
	enc, err := NewEncoderChannels(16000, 2, 48000, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(make([]float32, 961)); err == nil {
		t.Fatal("an odd stereo frame must fail")
	}
}
