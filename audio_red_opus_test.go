//go:build cgo

package meowcaller

import (
	"testing"

	"github.com/purpshell/meowcaller/opus"
	"github.com/rs/zerolog"
)

func TestPT122InOpusModeReachesOpusDecoder(t *testing.T) {
	if err := SetCodec("opus"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetCodec("mlow") })
	enc, err := opus.NewEncoder(SampleRate, 24000, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]float32, FrameSamples)
	for i := range pcm {
		pcm[i] = 0.3 * float32((i%40)-20) / 20
	}
	frame, err := enc.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	callKey := iota32()
	self := mediaTestJID("111111111111111", 0)
	peer := mediaTestJID("222222222222222", 0)
	var built participantAudioDecoder
	registry, err := newParticipantReceiveRegistry("CID", callKey, self.String(), peer.String(), func() participantAudioDecoder {
		built = newInboundAudioDecoder(zerolog.Nop())
		return built
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	if _, isOpus := built.(*opus.Decoder); !isOpus {
		t.Fatalf("inbound decoder in opus mode is %T, want *opus.Decoder", built)
	}
	for name, payload := range map[string][]byte{
		"rfc2198":  append(append(append(redHeader(120, 960, len(frame)), 0x78), frame...), frame...),
		"splitred": splitRedPayload(3, frame, frame),
	} {
		packet, _ := protectParticipantRTP(t, callKey, self, peer, 0, rtpPayloadTypeAudioRED, payload)
		audio, ok := registry.DecodeAudio(packet)
		if !ok || audio.Dropped || len(audio.PCM) != FrameSamples {
			t.Fatalf("%s: DecodeAudio ok=%v dropped=%v samples=%d, want %d", name, ok, audio.Dropped, len(audio.PCM), FrameSamples)
		}
	}
}
