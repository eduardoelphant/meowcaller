package meowcaller

import (
	"errors"
	"testing"

	"github.com/purpshell/meowcaller/opus"
	"github.com/purpshell/meowcaller/signaling"
)

func TestSetCodecTogglesMlowCapability(t *testing.T) {
	t.Cleanup(func() { _ = SetCodec("mlow") })
	if !opus.Available {
		t.Skip("built without cgo; see TestSetCodecOpusWithoutCgoFallsBackToMlow")
	}
	if err := SetCodec("opus"); err != nil {
		t.Fatal(err)
	}
	for name, blob := range map[string][]byte{
		"offer": signaling.CapabilityOffer, "video": signaling.CapabilityVideoOffer, "preaccept": signaling.CapabilityPreaccept,
	} {
		if blob[5]&0x80 != 0 {
			t.Fatalf("%s still advertises use_mlow_codec_v1", name)
		}
	}
	if !opusMode {
		t.Fatal("opus mode not set")
	}
	if err := SetCodec("mlow"); err != nil {
		t.Fatal(err)
	}
	if signaling.CapabilityOffer[5]&0x80 == 0 || opusMode {
		t.Fatal("mlow mode not restored")
	}
	if err := SetCodec("speex"); err == nil {
		t.Fatal("want an error for an unknown codec")
	}
}

func TestSetCodecOpusWithoutCgoFallsBackToMlow(t *testing.T) {
	if opus.Available {
		t.Skip("built with cgo and libopus")
	}
	t.Cleanup(func() { _ = SetCodec("mlow") })
	if err := SetCodec("opus"); !errors.Is(err, ErrOpusUnavailable) {
		t.Fatalf("SetCodec(opus) err = %v, want ErrOpusUnavailable", err)
	}
	if opusMode {
		t.Fatal("opus mode set without libopus")
	}
	if signaling.CapabilityOffer[5]&0x80 == 0 {
		t.Fatal("MLow capability cleared without libopus")
	}
	if _, err := newOpusEncoder(); err == nil {
		t.Fatal("newOpusEncoder succeeded without cgo")
	}
}
