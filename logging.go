package meowcaller

import (
	"github.com/purpshell/meowcaller/diag"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types/events"
)

// Option configures optional, non-behavioral aspects of the call/media types —
// currently the diagnostic logger. The zero configuration logs nothing.
type Option func(*config)

type config struct {
	log         zerolog.Logger
	diag        *diag.Recorder
	offerFilter func(*events.CallOffer) bool
	earlyMedia  EarlyMediaMode
}

func resolveConfig(opts []Option) config {
	c := config{log: zerolog.Nop()}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// WithLogger sets the zerolog logger for debug/trace diagnostics. The library never
// configures logging itself; without this option the types are silent at zero cost.
// Pass the logger from a context, e.g. WithLogger(*zerolog.Ctx(ctx)).
func WithLogger(l zerolog.Logger) Option {
	return func(c *config) { c.log = l }
}

// WithDiagnostics attaches a developer-only *diag.Recorder that dumps exact,
// per-category call diagnostics (including raw secrets and media) to JSONL files.
// This is an opt-in maintainer carve-out from the library's sanitized logging and
// must never be enabled in production. Without it the recorder is nil and every
// diag emit is a no-op at zero cost.
func WithDiagnostics(rec *diag.Recorder) Option {
	return func(c *config) { c.diag = rec }
}

// WithOfferFilter installs a gate for inbound call offers (1:1 and group). When fn
// returns false for an offer, the Client ignores it completely: no preaccept is
// sent, the incoming-call handler is not called and no call state is kept, so
// another stack sharing the same WhatsApp session can handle that call. fn runs on
// whatsmeow's event goroutine and must return quickly.
func WithOfferFilter(fn func(*events.CallOffer) bool) Option {
	return func(c *config) { c.offerFilter = fn }
}
