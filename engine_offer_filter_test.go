package meowcaller

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// testEngineForOffers builds an engine with the given offer filter whose send and
// callKey-decrypt paths are faked, returning the recorded sends and handler calls.
func testEngineForOffers(t *testing.T, filter func(*events.CallOffer) bool) (*engine, *[]waBinary.Node, *[]*Call) {
	t.Helper()
	cfg := resolveConfig([]Option{WithOfferFilter(filter)})
	wa := whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	c := &Client{wa: wa, log: zerolog.Nop(), offerFilter: cfg.offerFilter}
	c.eng = newEngine(c)
	var sent []waBinary.Node
	c.eng.sendCallNode = func(_ context.Context, node waBinary.Node) error {
		sent = append(sent, node)
		return nil
	}
	c.eng.decryptCallKey = func(context.Context, *events.CallOffer) ([]byte, error) {
		return make([]byte, 32), nil
	}
	var handled []*Call
	c.OnIncomingCall(func(call *Call) { handled = append(handled, call) })
	return c.eng, &sent, &handled
}

func directOffer() *events.CallOffer {
	ev := &events.CallOffer{}
	ev.CallID = "OFFERCID"
	ev.From = peerJID()
	ev.CallCreator = creatorJID()
	ev.Data = &waBinary.Node{
		Tag:   "offer",
		Attrs: waBinary.Attrs{"call-id": "OFFERCID", "call-creator": creatorJID()},
	}
	return ev
}

func groupOffer() *events.CallOffer {
	ev := directOffer()
	ev.Data.Content = []waBinary.Node{{
		Tag: "group_info",
		Attrs: waBinary.Attrs{
			"media": "audio", "transaction-id": "1", "connected-limit": "32",
		},
	}}
	return ev
}

func hasTag(nodes []waBinary.Node, tag string) bool {
	for _, n := range nodes {
		for _, k := range n.GetChildren() {
			if k.Tag == tag {
				return true
			}
		}
	}
	return false
}

func TestOfferFilterFalseIgnoresOffer(t *testing.T) {
	for name, mk := range map[string]func() *events.CallOffer{"direct": directOffer, "group": groupOffer} {
		t.Run(name, func(t *testing.T) {
			var seen int
			eng, sent, handled := testEngineForOffers(t, func(*events.CallOffer) bool { seen++; return false })
			eng.onOffer(mk())
			if seen != 1 {
				t.Fatalf("filter called %d times, want 1", seen)
			}
			if len(*sent) != 0 {
				t.Fatalf("sent %d nodes for a filtered offer, want none", len(*sent))
			}
			if len(*handled) != 0 {
				t.Fatal("OnIncomingCall called for a filtered offer")
			}
			if len(eng.calls) != 0 {
				t.Fatalf("engine kept %d calls for a filtered offer", len(eng.calls))
			}
		})
	}
}

func TestOfferFilterTrueKeepsBehaviour(t *testing.T) {
	for name, mk := range map[string]func() *events.CallOffer{"direct": directOffer, "group": groupOffer} {
		t.Run(name, func(t *testing.T) {
			eng, sent, handled := testEngineForOffers(t, func(*events.CallOffer) bool { return true })
			eng.onOffer(mk())
			if !hasTag(*sent, "preaccept") {
				t.Fatalf("no preaccept sent; sent %#v", *sent)
			}
			if len(*handled) != 1 || (*handled)[0].ID() != "OFFERCID" {
				t.Fatalf("OnIncomingCall calls = %d, want 1 for OFFERCID", len(*handled))
			}
			if eng.lookup("OFFERCID") == nil {
				t.Fatal("engine kept no state for an accepted offer")
			}
		})
	}
}

func TestNoOfferFilterKeepsBehaviour(t *testing.T) {
	eng, sent, handled := testEngineForOffers(t, nil)
	eng.onOffer(directOffer())
	if !hasTag(*sent, "preaccept") || len(*handled) != 1 {
		t.Fatalf("without a filter: preaccept=%v handled=%d", hasTag(*sent, "preaccept"), len(*handled))
	}
}
