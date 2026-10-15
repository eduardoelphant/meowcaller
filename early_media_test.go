package meowcaller

import (
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
}

func TestEarlyMediaOffStartsMediaOnAnswer(t *testing.T) {
	eng, call, _, dials := ringingWithFakeRelay(t, EarlyMediaOff)

	eng.maybeStartMedia("CID")
	eng.onRelay("CID", &testRelayNode)
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
