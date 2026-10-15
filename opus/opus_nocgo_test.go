//go:build !cgo

package opus

import (
	"errors"
	"testing"
)

func TestStubReportsUnavailable(t *testing.T) {
	if Available {
		t.Fatal("stub build claims opus is available")
	}
	if _, err := NewEncoder(16000, 24000, 5, true); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("NewEncoder err = %v, want ErrUnavailable", err)
	}
	if _, err := NewDecoder(16000); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("NewDecoder err = %v, want ErrUnavailable", err)
	}
	if _, err := PacketSamples([]byte{1}, 16000); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("PacketSamples err = %v, want ErrUnavailable", err)
	}
	if out := (&Decoder{}).Decode([]byte{1, 2, 3}); out != nil {
		t.Fatal("stub Decode returned samples")
	}
	if _, err := (&Encoder{}).Encode(make([]float32, 960)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stub Encode err = %v, want ErrUnavailable", err)
	}
}
