package meowcaller

import (
	"errors"
	"fmt"

	"github.com/purpshell/meowcaller/opus"
	"github.com/purpshell/meowcaller/signaling"
)

// Elphant fork: codec selection. In opus mode the client stops advertising capability
// 31 (use_mlow_codec_v1, bit 0x80 of byte 5) and answers with voip_settings that turn
// MLow off, so both sides use standard Opus on the same PT120 / 16 kHz / 60 ms RTP
// profile. Modelled on whatsapp-rust PR #1050 (wacore/src/stanza/call.rs).
var opusMode bool

var opusVoipSettings = []byte(`{"encode":{"use_mlow_codec_v1":"false"},"options":{"enable_48khz_rtp_clock":"false"}}`)

// ErrOpusUnavailable is returned by SetCodec("opus") in builds without cgo (no
// libopus). The process is then left in MLow mode, so signaling and media agree.
var ErrOpusUnavailable = errors.New("meowcaller: opus codec unavailable (built without cgo); using mlow")

// SetCodec selects "opus" or "mlow" for every client in the process. Call it once,
// before NewClient. Opus needs a cgo build linked to libopus; without it
// SetCodec("opus") selects MLow and returns ErrOpusUnavailable.
func SetCodec(name string) error {
	var err error
	switch name {
	case "opus":
		if opus.Available {
			opusMode = true
		} else {
			opusMode = false
			err = ErrOpusUnavailable
		}
	case "mlow":
		opusMode = false
	default:
		return fmt.Errorf("meowcaller: unknown codec %q", name)
	}
	setMlowCapability(!opusMode)
	return err
}

// setMlowCapability sets or clears capability 31 (use_mlow_codec_v1).
func setMlowCapability(mlow bool) {
	for _, blob := range [][]byte{signaling.CapabilityOffer, signaling.CapabilityVideoOffer, signaling.CapabilityPreaccept} {
		if mlow {
			blob[5] |= 0x80
		} else {
			blob[5] &^= 0x80
		}
	}
}

type audioEncoder interface {
	Encode(pcm []float32) ([]byte, error)
}

func newOpusEncoder() (audioEncoder, error) {
	return opus.NewEncoder(SampleRate, 24000, 5, true)
}

func newOpusDecoder() (participantAudioDecoder, error) {
	return opus.NewDecoder(SampleRate)
}
