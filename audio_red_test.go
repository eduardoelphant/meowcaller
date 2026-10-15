package meowcaller

import (
	"bytes"
	"errors"
	"testing"

	"github.com/purpshell/meowcaller/rtp"
)

// redHeader builds one 4-byte RFC 2198 redundant block header.
func redHeader(pt uint8, offset uint16, length int) []byte {
	word := uint32(offset)<<10 | uint32(length)
	return []byte{0x80 | pt, byte(word >> 16), byte(word >> 8), byte(word)}
}

func TestParseREDPrimaryOnly(t *testing.T) {
	blocks, err := parseRED([]byte{0x78, 1, 2, 3})
	if err != nil {
		t.Fatalf("parseRED: %v", err)
	}
	if len(blocks) != 1 || !blocks[0].Primary || blocks[0].PT != 120 || !bytes.Equal(blocks[0].Data, []byte{1, 2, 3}) {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestParseREDRedundantAndPrimary(t *testing.T) {
	payload := append(redHeader(120, 960, 3), 0x78)
	payload = append(payload, 9, 9, 9)    // redundant copy of the previous frame
	payload = append(payload, 1, 2, 3, 4) // primary
	blocks, err := parseRED(payload)
	if err != nil {
		t.Fatalf("parseRED: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	if r := blocks[0]; r.Primary || r.PT != 120 || r.TSOffset != 960 || !bytes.Equal(r.Data, []byte{9, 9, 9}) {
		t.Fatalf("redundant block = %+v", r)
	}
	if p := blocks[1]; !p.Primary || p.PT != 120 || !bytes.Equal(p.Data, []byte{1, 2, 3, 4}) {
		t.Fatalf("primary block = %+v", p)
	}
	if primary, err := redPrimary(payload); err != nil || !bytes.Equal(primary, []byte{1, 2, 3, 4}) {
		t.Fatalf("redPrimary = %v, %v", primary, err)
	}
}

func TestParseREDMalformed(t *testing.T) {
	cases := map[string]struct {
		payload []byte
		want    error
	}{
		"empty":            {nil, errREDEmpty},
		"truncated header": {[]byte{0xf8, 0x00}, errREDHeaderShort},
		"no final header":  {redHeader(120, 960, 0), errREDHeaderShort},
		"length overrun":   {append(append(redHeader(120, 960, 50), 0x78), 1, 2, 3), errREDBlockOverrun},
		"empty primary":    {append(append(redHeader(120, 960, 2), 0x78), 1, 2), errREDNoPrimary},
		"header only":      {[]byte{0x78}, errREDNoPrimary},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRED(tc.payload); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDecodeAudioUnwrapsREDPrimary(t *testing.T) {
	callKey := iota32()
	self := mediaTestJID("111111111111111", 0)
	peer := mediaTestJID("222222222222222", 0)
	dec := &recordingParticipantDecoder{}
	registry, err := newParticipantReceiveRegistry("CID", callKey, self.String(), peer.String(), func() participantAudioDecoder { return dec })
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	payload := append(append(redHeader(120, 960, 2), 0x78), 9, 9, 7, 8, 6)
	packet, _ := protectParticipantRTP(t, callKey, self, peer, 0, rtpPayloadTypeAudioRED, payload)

	audio, ok := registry.DecodeAudio(packet)
	if !ok || audio.Dropped {
		t.Fatalf("DecodeAudio ok=%v dropped=%v", ok, audio.Dropped)
	}
	if len(dec.payloads) != 1 || !bytes.Equal(dec.payloads[0], []byte{7, 8, 6}) {
		t.Fatalf("decoder got %v, want only the primary [7 8 6]", dec.payloads)
	}
	if audio.Timestamp != 3000 || len(audio.PCM) != 1 || audio.PCM[0] != 7 {
		t.Fatalf("audio = ts %d pcm %v, want ts 3000 and the primary decoded", audio.Timestamp, audio.PCM)
	}
}

func TestDecodeAudioDropsMalformedRED(t *testing.T) {
	callKey := iota32()
	self := mediaTestJID("111111111111111", 0)
	peer := mediaTestJID("222222222222222", 0)
	dec := &recordingParticipantDecoder{}
	registry, err := newParticipantReceiveRegistry("CID", callKey, self.String(), peer.String(), func() participantAudioDecoder { return dec })
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	payload := append(append(redHeader(120, 960, 40), 0x78), 1, 2, 3)
	packet, _ := protectParticipantRTP(t, callKey, self, peer, 0, rtpPayloadTypeAudioRED, payload)

	audio, ok := registry.DecodeAudio(packet)
	if !ok || !audio.Dropped {
		t.Fatalf("DecodeAudio ok=%v dropped=%v, want an authenticated drop", ok, audio.Dropped)
	}
	if len(dec.payloads) != 0 {
		t.Fatalf("decoder called for a malformed RED payload: %v", dec.payloads)
	}
}

func TestDecodeAudioLeavesPT120Untouched(t *testing.T) {
	callKey := iota32()
	self := mediaTestJID("111111111111111", 0)
	peer := mediaTestJID("222222222222222", 0)
	dec := &recordingParticipantDecoder{}
	registry, err := newParticipantReceiveRegistry("CID", callKey, self.String(), peer.String(), func() participantAudioDecoder { return dec })
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	payload := []byte{0xf8, 1, 2, 3} // would parse as a RED header if PT 120 were unwrapped
	packet, _ := protectParticipantRTP(t, callKey, self, peer, 0, 120, payload)
	if _, ok := registry.DecodeAudio(packet); !ok {
		t.Fatal("PT 120 packet not decoded")
	}
	if len(dec.payloads) != 1 || !bytes.Equal(dec.payloads[0], payload) {
		t.Fatalf("decoder got %v, want the raw PT 120 payload", dec.payloads)
	}
}

func TestPT122IsAudio(t *testing.T) {
	if classifyMediaPayload(rtp.RtpHeader{PayloadType: 122}) != mediaPayloadAudio {
		t.Fatal("PT 122 not classified as audio")
	}
}

func TestPayloadFormatLoggedOncePerType(t *testing.T) {
	r := &participantReceiveRegistry{}
	if !r.firstPayloadOfType(122) || r.firstPayloadOfType(122) {
		t.Fatal("PT 122 format should log exactly once")
	}
	if !r.firstPayloadOfType(121) {
		t.Fatal("PT 121 format should log once on its own")
	}
}
