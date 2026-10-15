package meowcaller

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/purpshell/meowcaller/mlow"
	"github.com/rs/zerolog"
)

// Elphant fork: RFC 2198 RED on inbound audio. WhatsApp Web callers in Opus mode
// were seen sending PT 122 instead of 120: same 960-sample timestamp step, a
// 49-byte first packet and a 92-byte second one (primary plus one redundant copy).
// whatsapp-rust names PT 121 RTP_PAYLOAD_TYPE_MLOW_RED; PT 122 is taken to be the
// Opus counterpart. The primary (last) block goes to the same decoder as PT 120.
//
// Unverified: the exact block layout. Both layouts are tried: WhatsApp's own
// SplitRed (mlow.DepackSplitRed, 2-byte redundant headers) and RFC 2198 (4-byte
// headers). A layout counts only if its headers and block lengths account for
// the whole payload; SplitRed is preferred when both do, and the other layout's
// primary is tried if the preferred one does not decode. For a primary-only
// packet the two layouts are identical (one header byte). The Debug "inbound
// audio payload format" line logs both parses and the layout chosen.
//
// PT 121 is left on its existing path (raw payload to the decoder): the MLow
// decoder does WhatsApp's own SplitRed depacketization when redundancy is set.
const rtpPayloadTypeAudioRED uint8 = 122

var (
	errREDEmpty        = errors.New("red: empty payload")
	errREDHeaderShort  = errors.New("red: block header truncated")
	errREDBlockOverrun = errors.New("red: block lengths exceed payload")
	errREDNoPrimary    = errors.New("red: empty primary block")
)

// redBlock is one RFC 2198 block. Data is a subslice of the parsed payload.
type redBlock struct {
	PT       uint8
	TSOffset uint16 // 0 for the primary block
	Data     []byte
	Primary  bool
}

// parseRED splits an RFC 2198 payload into its blocks, redundant blocks first
// and the primary last. Each redundant header is 4 bytes (F=1|PT, 14-bit
// timestamp offset, 10-bit length); the final header is 1 byte (F=0|PT) and the
// primary block takes the rest of the payload.
func parseRED(p []byte) ([]redBlock, error) {
	if len(p) == 0 {
		return nil, errREDEmpty
	}
	type hdr struct {
		pt     uint8
		offset uint16
		length int
	}
	var hdrs []hdr
	cur := 0
	for {
		if cur >= len(p) {
			return nil, errREDHeaderShort
		}
		b0 := p[cur]
		if b0&0x80 == 0 {
			break
		}
		if cur+4 > len(p) {
			return nil, errREDHeaderShort
		}
		word := uint32(p[cur+1])<<16 | uint32(p[cur+2])<<8 | uint32(p[cur+3])
		hdrs = append(hdrs, hdr{
			pt:     b0 & 0x7f,
			offset: uint16(word >> 10),
			length: int(word & 0x3ff),
		})
		cur += 4
	}
	primaryPT := p[cur] & 0x7f
	cur++
	blocks := make([]redBlock, 0, len(hdrs)+1)
	for _, h := range hdrs {
		if cur+h.length > len(p) {
			return nil, errREDBlockOverrun
		}
		blocks = append(blocks, redBlock{PT: h.pt, TSOffset: h.offset, Data: p[cur : cur+h.length]})
		cur += h.length
	}
	if cur >= len(p) {
		return nil, errREDNoPrimary
	}
	blocks = append(blocks, redBlock{PT: primaryPT, Data: p[cur:], Primary: true})
	return blocks, nil
}

const (
	redLayoutSplitRed = "splitred"
	redLayoutRFC2198  = "rfc2198"
)

// redCandidate is one layout's reading of a RED payload's primary block.
type redCandidate struct {
	layout  string
	primary []byte
}

// redPrimaryCandidates returns the primary block under every layout whose
// headers and lengths account for the whole payload: SplitRed first, then
// RFC 2198 when it reads a different primary.
func redPrimaryCandidates(p []byte) []redCandidate {
	var out []redCandidate
	if frames, err := mlow.DepackSplitRed(p); err == nil && len(frames) > 0 {
		if main := frames[len(frames)-1]; main.IsMain && len(main.Data) > 0 {
			out = append(out, redCandidate{layout: redLayoutSplitRed, primary: main.Data})
		}
	}
	if primary, err := redPrimary(p); err == nil {
		if len(out) == 0 || !bytes.Equal(out[0].primary, primary) {
			out = append(out, redCandidate{layout: redLayoutRFC2198, primary: primary})
		}
	}
	return out
}

// decodeREDPrimary decodes the primary block of a RED payload, trying the
// layouts in preference order and moving on when a primary does not decode
// (nil PCM). It returns the PCM, the layout used, and false when no layout
// parses.
func decodeREDPrimary(dec participantAudioDecoder, p []byte) ([]float32, string, bool) {
	candidates := redPrimaryCandidates(p)
	if len(candidates) == 0 {
		return nil, "", false
	}
	for _, c := range candidates {
		if pcm := dec.Decode(c.primary); pcm != nil {
			return pcm, c.layout, true
		}
	}
	return nil, candidates[0].layout, true
}

// redPrimary returns the primary block of an RFC 2198 payload.
func redPrimary(p []byte) ([]byte, error) {
	blocks, err := parseRED(p)
	if err != nil {
		return nil, err
	}
	return blocks[len(blocks)-1].Data, nil
}

// describeREDBlocks renders a parse result for the format Debug line.
func describeREDBlocks(blocks []redBlock, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		kind := "red"
		if b.Primary {
			kind = "primary"
		}
		parts = append(parts, fmt.Sprintf("%s pt=%d ts_off=%d len=%d", kind, b.PT, b.TSOffset, len(b.Data)))
	}
	return strings.Join(parts, "; ")
}

// logAudioPayloadFormat logs, at Debug, the first bytes of a decrypted audio
// payload (audio data, not keys) and how the RED parsers read it.
func logAudioPayloadFormat(log zerolog.Logger, pt uint8, ssrc uint32, payload []byte, chosen string) {
	head := payload
	if len(head) > 8 {
		head = head[:8]
	}
	blocks, err := parseRED(payload)
	splitRed := "error"
	if frames, splitErr := mlow.DepackSplitRed(payload); splitErr == nil {
		lens := make([]string, 0, len(frames))
		for _, f := range frames {
			lens = append(lens, fmt.Sprintf("%d", len(f.Data)))
		}
		splitRed = fmt.Sprintf("%d frames, lens [%s]", len(frames), strings.Join(lens, " "))
	} else {
		splitRed = "error: " + splitErr.Error()
	}
	log.Debug().
		Uint8("payload_type", pt).
		Uint32("ssrc", ssrc).
		Int("payload_bytes", len(payload)).
		Str("head_hex", hex.EncodeToString(head)).
		Str("rfc2198", describeREDBlocks(blocks, err)).
		Str("splitred", splitRed).
		Str("chosen_layout", chosen).
		Msg("inbound audio payload format")
}
