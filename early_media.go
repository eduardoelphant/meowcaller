package meowcaller

import (
	"sync"
	"sync/atomic"
)

// EarlyMediaMode controls what an inbound 1:1 call does on the relays while it
// is still ringing (before Answer).
type EarlyMediaMode int

const (
	// EarlyMediaOff starts media only on Answer: nothing is sent to the relays
	// while ringing. Default.
	EarlyMediaOff EarlyMediaMode = iota
	// EarlyMediaListen connects the relays while ringing and sends only what
	// receiving needs (DTLS/SCTP/datachannel setup, STUN allocate, the relay
	// consent ping and keepalive, binding-success replies). No RTP and no SRTCP
	// leave until Answer; inbound packets are still received and decoded.
	EarlyMediaListen
	// EarlyMediaFull runs full media while ringing: relays, silence RTP and SRTCP
	// sender reports (upstream PR #43 behaviour).
	EarlyMediaFull
)

func (m EarlyMediaMode) String() string {
	switch m {
	case EarlyMediaOff:
		return "off"
	case EarlyMediaListen:
		return "listen"
	case EarlyMediaFull:
		return "full"
	}
	return "unknown"
}

// WithEarlyMedia sets the early-media mode for inbound 1:1 calls. The default
// is EarlyMediaOff.
func WithEarlyMedia(mode EarlyMediaMode) Option {
	return func(c *config) { c.earlyMedia = mode }
}

// mediaSendGate holds back media (RTP, SRTCP) until it is opened. A nil gate is
// open.
type mediaSendGate struct {
	open   atomic.Bool
	once   sync.Once
	opened chan struct{}
}

func newClosedMediaSendGate() *mediaSendGate {
	return &mediaSendGate{opened: make(chan struct{})}
}

// Open lets media through; it reports whether this call opened it.
func (g *mediaSendGate) Open() bool {
	if g == nil {
		return false
	}
	first := false
	g.once.Do(func() {
		g.open.Store(true)
		close(g.opened)
		first = true
	})
	return first
}

func (g *mediaSendGate) IsOpen() bool {
	return g == nil || g.open.Load()
}

// Opened is closed when the gate opens; nil for an open (nil) gate.
func (g *mediaSendGate) Opened() <-chan struct{} {
	if g == nil {
		return nil
	}
	return g.opened
}

// gatedMediaSender sends media packets through f only while the gate is open;
// closed-gate packets are dropped and reported as sent so senders keep running.
type gatedMediaSender struct {
	f    relayMediaSender
	gate *mediaSendGate
}

func (s gatedMediaSender) Send(data []byte) (int, error) {
	if !s.gate.IsOpen() {
		return len(data), nil
	}
	return s.f.Send(data)
}
