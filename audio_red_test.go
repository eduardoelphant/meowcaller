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

// splitRedPayload builds a WhatsApp SplitRed payload: one 2-byte redundant
// header (0x80|code, size), the 1-byte main marker, the redundant block, the main.
func splitRedPayload(code uint8, red, main []byte) []byte {
	p := []byte{0x80 | code, byte(len(red)), 0x01}
	p = append(p, red...)
	return append(p, main...)
}

// rejectingDecoder returns nil (decode failure) for payloads starting with a
// rejected byte, and one sample holding the first byte otherwise.
type rejectingDecoder struct {
	reject   byte
	payloads [][]byte
}

func (d *rejectingDecoder) Decode(payload []byte) []float32 {
	d.payloads = append(d.payloads, append([]byte(nil), payload...))
	if len(payload) == 0 || payload[0] == d.reject {
		return nil
	}
	return []float32{float32(payload[0])}
}

func TestREDSplitRedLayoutYieldsPrimary(t *testing.T) {
	payload := splitRedPayload(3, []byte{9, 9, 9}, []byte{0x41, 0x42, 0x43, 0x44})
	// RFC 2198 cannot read this one (its 10-bit length overruns), so only SplitRed counts.
	if _, err := parseRED(payload); err == nil {
		t.Fatal("test payload unexpectedly valid as RFC 2198")
	}
	c := redPrimaryCandidates(payload)
	if len(c) != 1 || c[0].layout != redLayoutSplitRed || !bytes.Equal(c[0].primary, []byte{0x41, 0x42, 0x43, 0x44}) {
		t.Fatalf("candidates = %+v, want SplitRed primary only", c)
	}
}

func TestREDRFC2198LayoutStillWorks(t *testing.T) {
	payload := append(append(redHeader(120, 960, 3), 0x78), 9, 9, 9, 1, 2, 3, 4)
	c := redPrimaryCandidates(payload)
	if len(c) != 1 || c[0].layout != redLayoutRFC2198 || !bytes.Equal(c[0].primary, []byte{1, 2, 3, 4}) {
		t.Fatalf("candidates = %+v, want RFC 2198 primary only", c)
	}
}

// bothLayoutsPayload is a strict RFC 2198 packet (PT 120, ts offset 960) that
// also parses as SplitRed: offset byte 0x0f reads as a 15-byte redundant size
// and 0x00 as the main marker. RFC 2198 reads primary 0x41..0x4c; SplitRed
// reads main [0x4b 0x4c].
var bothLayoutsPayload = append(append(redHeader(120, 960, 3), 0x78, 9, 9, 9),
	0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c)

func TestREDPrefersStrictRFC2198WhenBothValid(t *testing.T) {
	c := redPrimaryCandidates(bothLayoutsPayload)
	if len(c) != 2 || c[0].layout != redLayoutRFC2198 || c[1].layout != redLayoutSplitRed {
		t.Fatalf("candidates = %+v, want RFC 2198 then SplitRed", c)
	}
	dec := &rejectingDecoder{reject: 0xff}
	pcm, layout, ok := decodeREDPrimary(dec, bothLayoutsPayload)
	if !ok || layout != redLayoutRFC2198 || len(pcm) != 1 || pcm[0] != 0x41 || len(dec.payloads) != 1 {
		t.Fatalf("decode = %v %q %v (%d decodes), want the RFC 2198 primary in one decode", pcm, layout, ok, len(dec.payloads))
	}
}

func TestREDFallsBackToOtherLayoutOnDecodeFailure(t *testing.T) {
	dec := &rejectingDecoder{reject: 0x41} // the RFC 2198 primary does not decode
	pcm, layout, ok := decodeREDPrimary(dec, bothLayoutsPayload)
	if !ok || layout != redLayoutSplitRed || len(pcm) != 1 || pcm[0] != 0x4b {
		t.Fatalf("decode = %v %q %v, want the SplitRed main", pcm, layout, ok)
	}
}

func TestRFC2198WithImplausibleHeaderIsNotACandidate(t *testing.T) {
	for name, hdr := range map[string][]byte{
		"pt differs":       redHeader(121, 960, 1),
		"offset not frame": redHeader(120, 500, 1),
		"zero offset":      redHeader(120, 0, 1),
	} {
		payload := append(append(hdr, 0x78, 9), 1, 2, 3)
		for _, c := range redPrimaryCandidates(payload) {
			if c.layout == redLayoutRFC2198 {
				t.Fatalf("%s: RFC 2198 accepted: %+v", name, c)
			}
		}
	}
}

func TestDecodeAudioUnwrapsSplitRedPT122(t *testing.T) {
	callKey := iota32()
	self := mediaTestJID("111111111111111", 0)
	peer := mediaTestJID("222222222222222", 0)
	dec := &recordingParticipantDecoder{}
	registry, err := newParticipantReceiveRegistry("CID", callKey, self.String(), peer.String(), func() participantAudioDecoder { return dec })
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	payload := splitRedPayload(3, []byte{9, 9, 9}, []byte{0x41, 0x42, 0x43})
	packet, _ := protectParticipantRTP(t, callKey, self, peer, 0, rtpPayloadTypeAudioRED, payload)
	audio, ok := registry.DecodeAudio(packet)
	if !ok || audio.Dropped || len(dec.payloads) != 1 || !bytes.Equal(dec.payloads[0], []byte{0x41, 0x42, 0x43}) {
		t.Fatalf("ok=%v dropped=%v decoder got %v, want the SplitRed main", ok, audio.Dropped, dec.payloads)
	}
}
