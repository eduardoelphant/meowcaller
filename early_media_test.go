package meowcaller

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/purpshell/meowcaller/relay"
	waBinary "go.mau.fi/whatsmeow/binary"
)

// testRelayNode is a relaylatency-style stanza carrying a <relay> block.
var testRelayNode = waBinary.Node{Tag: "relaylatency", Content: []waBinary.Node{{
	Tag: "relay",
	Content: []waBinary.Node{
		{Tag: "key", Content: []byte("relaykey")},
		{Tag: "token", Attrs: waBinary.Attrs{"id": "0"}, Content: []byte("token")},
		{Tag: "te2", Attrs: waBinary.Attrs{"relay_name": "a", "token_id": "0"}, Content: []byte{10, 0, 0, 1, 0x0d, 0x96}},
	},
}}}

// packetKinds counts what a fake relay channel was sent.
func packetKinds(c *fakeRelayConn) (stun, rtcp, rtpCount, other int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.sent {
		switch relay.ClassifyRelayPacket(p) {
		case relay.RelayPacketStun:
			stun++
		case relay.RelayPacketRtcp:
			rtcp++
		case relay.RelayPacketRtp:
			rtpCount++
		default:
			other++
		}
	}
	return
}

// ringingWithFakeRelay returns a ringing inbound engine in the given mode whose
// relay dials hand out one recorded fake channel.
func ringingWithFakeRelay(t *testing.T, mode EarlyMediaMode) (*engine, *Call, func() *fakeRelayConn, func() int) {
	t.Helper()
	eng, call, _ := testRingingInbound(t, testRelayData("a"))
	eng.c.earlyMedia = mode
	var mu sync.Mutex
	var conn *fakeRelayConn
	dials := 0
	eng.dialRelay = func(addr *net.UDPAddr) (relayConn, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		conn = newFakeRelayConn(addr.String())
		return conn, nil
	}
	t.Cleanup(func() { eng.finishCall("CID", "test") })
	return eng, call, func() *fakeRelayConn {
			mu.Lock()
			defer mu.Unlock()
			return conn
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return dials
		}
}

func TestEarlyMediaListenSendsNoRTPOrSRTCPBeforeAnswer(t *testing.T) {
	eng, call, conn, _ := ringingWithFakeRelay(t, EarlyMediaListen)

	eng.maybeStartMedia("CID")
	waitFor(t, "allocate on the relay", func() bool {
		c := conn()
		if c == nil {
			return false
		}
		stun, _, _, _ := packetKinds(c)
		return stun > 0
	})
	time.Sleep(400 * time.Millisecond) // several 60 ms audio ticks
	if _, rtcp, rtpN, _ := packetKinds(conn()); rtcp != 0 || rtpN != 0 {
		t.Fatalf("before Answer: %d RTCP and %d RTP packets sent, want none", rtcp, rtpN)
	}

	if err := eng.answer(call); err != nil {
		t.Fatalf("answer: %v", err)
	}
	waitFor(t, "RTP and SRTCP after Answer", func() bool {
		_, rtcp, rtpN, _ := packetKinds(conn())
		return rtcp > 0 && rtpN > 0
	})
	// The first sender report follows our first RTP.
	c := conn()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.sent {
		switch relay.ClassifyRelayPacket(p) {
		case relay.RelayPacketRtcp:
			t.Fatal("first SRTCP went out before the first RTP")
		case relay.RelayPacketRtp:
			return
		}
	}
}

func TestEarlyMediaListenCallEndedWhileRingingClosesRelay(t *testing.T) {
	eng, _, conn, _ := ringingWithFakeRelay(t, EarlyMediaListen)

	eng.maybeStartMedia("CID")
	waitFor(t, "relay connected", func() bool { return conn() != nil })
	eng.finishCall("CID", "caller hung up")
	waitFor(t, "relay channel closed after the call ended", func() bool { return conn().closed.Load() })
	if _, rtcp, rtpN, _ := packetKinds(conn()); rtcp != 0 || rtpN != 0 {
		t.Fatalf("ended ringing call sent %d RTCP and %d RTP packets", rtcp, rtpN)
	}
}

func TestAnswerDuringMediaExitRestartsMedia(t *testing.T) {
	// The interleaving: a run started while ringing is ending on its own; Answer
	// lands first, sees started=true and does not start media; the run's exit
	// then restarts media under the engine lock.
	eng, call, _, dials := ringingWithFakeRelay(t, EarlyMediaListen)
	m := eng.calls["CID"]
	mctx, cancel := context.WithCancel(context.Background())
	eng.mu.Lock()
	m.started = true
	m.cancel = cancel
	eng.mu.Unlock()

	if err := eng.answer(call); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if n := dials(); n != 0 {
		t.Fatalf("Answer dialed %d relays while the old run held started", n)
	}
	eng.allowMediaRestart(mctx, cancel, "CID", m, true)
	waitFor(t, "media restarted for the answered call", func() bool { return dials() == 1 })
}

func TestMediaExitAfterAnswerAtStartDoesNotRestart(t *testing.T) {
	eng, _, _, dials := ringingWithFakeRelay(t, EarlyMediaOff)
	m := eng.calls["CID"]
	mctx, cancel := context.WithCancel(context.Background())
	eng.mu.Lock()
	m.started = true
	m.answered = true
	eng.mu.Unlock()
	eng.allowMediaRestart(mctx, cancel, "CID", m, false)
	time.Sleep(50 * time.Millisecond)
	eng.mu.Lock()
	started := m.started
	eng.mu.Unlock()
	if dials() != 0 || !started {
		t.Fatalf("run started after Answer was restarted or reset: dials=%d started=%v", dials(), started)
	}
}

func TestEarlyMediaOffStartsMediaOnAnswer(t *testing.T) {
	eng, call, _, dials := ringingWithFakeRelay(t, EarlyMediaOff)

	eng.maybeStartMedia("CID")
	before := eng.calls["CID"].relay
	eng.onRelay("CID", &testRelayNode)
	eng.mu.Lock()
	after := eng.calls["CID"].relay
	eng.mu.Unlock()
	if after == before || len(after.endpoints) != 1 || after.endpoints[0].relayName != "a" {
		t.Fatalf("onRelay did not replace m.relay (before %p, after %p)", before, after)
	}
	time.Sleep(100 * time.Millisecond)
	if n := dials(); n != 0 {
		t.Fatalf("dialed %d relays while ringing in off mode, want 0", n)
	}
	eng.mu.Lock()
	started := eng.calls["CID"].started
	eng.mu.Unlock()
	if started {
		t.Fatal("media started while ringing in off mode")
	}

	if err := eng.answer(call); err != nil {
		t.Fatalf("answer: %v", err)
	}
	waitFor(t, "relay dial after Answer", func() bool { return dials() == 1 })
}

func TestEarlyMediaFullSendsRTPWhileRinging(t *testing.T) {
	eng, _, conn, _ := ringingWithFakeRelay(t, EarlyMediaFull)

	eng.maybeStartMedia("CID")
	waitFor(t, "RTP while ringing in full mode", func() bool {
		c := conn()
		if c == nil {
			return false
		}
		_, _, rtpN, _ := packetKinds(c)
		return rtpN > 0
	})
}

func TestDefaultEarlyMediaModeIsOff(t *testing.T) {
	if got := resolveConfig(nil).earlyMedia; got != EarlyMediaOff {
		t.Fatalf("default early media = %v, want off", got)
	}
	if got := resolveConfig([]Option{WithEarlyMedia(EarlyMediaListen)}).earlyMedia; got != EarlyMediaListen {
		t.Fatalf("WithEarlyMedia(listen) = %v", got)
	}
}
