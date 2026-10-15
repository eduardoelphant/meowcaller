package meowcaller

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// testRingingInbound builds an engine holding one ringing inbound 1:1 call with a
// callKey and relay, sends recorded in the returned slice.
func testRingingInbound(t *testing.T, rd *relayData) (*engine, *Call, func() []waBinary.Node) {
	t.Helper()
	wa := whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	c := &Client{wa: wa, log: zerolog.Nop()}
	c.eng = newEngine(c)
	var mu sync.Mutex
	var sent []waBinary.Node
	c.eng.sendCallNode = func(_ context.Context, node waBinary.Node) error {
		mu.Lock()
		sent = append(sent, node)
		mu.Unlock()
		return nil
	}
	call := &Call{eng: c.eng, id: "CID", peer: peerJID(), phase: CallPhaseRinging}
	c.eng.calls["CID"] = &engineCall{
		call:      call,
		direction: CallDirectionIncoming,
		from:      peerJID(),
		creator:   creatorJID(),
		callKey:   make([]byte, 32),
		selfLID:   creatorJID().String(),
		peerLID:   peerJID().String(),
		relay:     rd,
	}
	return c.eng, call, func() []waBinary.Node {
		mu.Lock()
		defer mu.Unlock()
		return append([]waBinary.Node(nil), sent...)
	}
}

func latencyProbes(names ...string) *events.CallRelayLatency {
	ev := &events.CallRelayLatency{}
	ev.CallID = "CID"
	ev.From = peerJID()
	ev.CallCreator = creatorJID()
	rl := waBinary.Node{Tag: "relaylatency"}
	var kids []waBinary.Node
	for _, n := range names {
		kids = append(kids, waBinary.Node{
			Tag:     "te",
			Attrs:   waBinary.Attrs{"relay_name": n, "latency": "33554442"},
			Content: []byte{10, 0, 0, 1, 0x0d, 0x96},
		})
	}
	rl.Content = kids
	ev.Data = &rl
	return ev
}

func echoedRelayNames(nodes []waBinary.Node) []string {
	var names []string
	for _, n := range nodes {
		if rl := findChild(&n, "relaylatency"); rl != nil {
			if te := findChild(rl, "te"); te != nil {
				names = append(names, te.AttrGetter().OptionalString("relay_name"))
			}
		}
	}
	return names
}

func TestLatencyEchoesOnlyRelaysMediaBound(t *testing.T) {
	// A later relay block names b and c, but media bound only a.
	eng, _, sent := testRingingInbound(t, testRelayData("a", "b", "c"))
	eng.calls["CID"].mediaRelays = map[string]bool{"a": true}

	eng.onRelayLatency(latencyProbes("a", "b", "c"))

	if got := echoedRelayNames(sent()); len(got) != 1 || got[0] != "a" {
		t.Fatalf("echoed %v, want [a]", got)
	}
}

func TestLatencyEchoesOfferedRelaysBeforeMedia(t *testing.T) {
	eng, _, sent := testRingingInbound(t, testRelayData("a", "b"))

	eng.onRelayLatency(latencyProbes("a", "b", "edge"))

	if got := echoedRelayNames(sent()); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("echoed %v, want [a b]", got)
	}
}

func TestMediaRelaysNarrowToConnectedRelays(t *testing.T) {
	rd := testRelayData("a", "b")
	eng, _, sent := testRingingInbound(t, rd)
	eng.c.earlyMedia = EarlyMediaFull
	badAddr := rd.endpoints[1].addresses[0].ipv4
	eng.dialRelay = func(addr *net.UDPAddr) (relayConn, error) {
		if addr.IP.String() == badAddr {
			return nil, errors.New("unreachable")
		}
		return newFakeRelayConn(addr.String()), nil
	}
	t.Cleanup(func() { eng.finishCall("CID", "test") })

	eng.maybeStartMedia("CID")
	waitFor(t, "media relays narrowed to the connected relay", func() bool {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		m := eng.calls["CID"]
		return m != nil && len(m.mediaRelays) == 1 && m.mediaRelays["a"]
	})

	eng.onRelayLatency(latencyProbes("a", "b"))
	if got := echoedRelayNames(sent()); len(got) != 1 || got[0] != "a" {
		t.Fatalf("echoed %v, want [a]", got)
	}
}

func TestMediaExitWhileRingingAllowsRestart(t *testing.T) {
	eng, call, _ := testRingingInbound(t, testRelayData("a"))
	eng.c.earlyMedia = EarlyMediaFull
	var dials int
	var mu sync.Mutex
	eng.dialRelay = func(*net.UDPAddr) (relayConn, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		return nil, errors.New("unreachable")
	}
	t.Cleanup(func() { eng.finishCall("CID", "test") })
	notStarted := func() bool {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		m := eng.calls["CID"]
		return m != nil && !m.started && m.cancel == nil
	}

	eng.maybeStartMedia("CID")
	waitFor(t, "started reset after media failed while ringing", notStarted)

	if err := eng.answer(call); err != nil {
		t.Fatalf("answer: %v", err)
	}
	waitFor(t, "Answer to start media again", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return dials == 2
	})
	// Answered now: a second failure keeps started set.
	time.Sleep(50 * time.Millisecond)
	waitFor(t, "second media run to end", func() bool {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		m := eng.calls["CID"]
		return m != nil && m.started && m.answered
	})
}
