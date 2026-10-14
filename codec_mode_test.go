package meowcaller

import (
	"testing"

	"github.com/purpshell/meowcaller/signaling"
)

func TestSetCodecTogglesMlowCapability(t *testing.T) {
	t.Cleanup(func() { _ = SetCodec("mlow") })
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
