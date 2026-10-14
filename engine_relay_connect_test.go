package meowcaller

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeRelayConn is a relayConn whose Recv blocks until Close (or a queued packet).
type fakeRelayConn struct {
	name      string
	closed    atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	in        chan []byte

	mu   sync.Mutex
	sent [][]byte
}

func newFakeRelayConn(name string) *fakeRelayConn {
	return &fakeRelayConn{name: name, done: make(chan struct{}), in: make(chan []byte, 16)}
}

func (f *fakeRelayConn) Send(data []byte) (int, error) {
	if f.closed.Load() {
		return 0, errors.New("closed")
	}
	f.mu.Lock()
	f.sent = append(f.sent, append([]byte(nil), data...))
	f.mu.Unlock()
	return len(data), nil
}

func (f *fakeRelayConn) Recv(buf []byte) (int, error) {
	select {
	case p := <-f.in:
		return copy(buf, p), nil
	case <-f.done:
		return 0, errors.New("closed")
	}
}

func (f *fakeRelayConn) Close() error {
	f.closeOnce.Do(func() {
		f.closed.Store(true)
		close(f.done)
	})
	return nil
}

func (f *fakeRelayConn) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testRelayData(names ...string) *relayData {
	rd := &relayData{relayTokens: [][]byte{[]byte("token")}, relayKeyASCII: []byte("relaykey")}
	for i, name := range names {
		rd.endpoints = append(rd.endpoints, relayEndpoint{
			relayName: name,
			addresses: []relayAddress{{ipv4: "10.0.0." + string(rune('1'+i)), port: 3478}},
		})
	}
	return rd
}

func testRelayEngine() *engine {
	return newEngine(&Client{log: zerolog.Nop()})
}

func TestConnectOneRelayClosesLateChannelAfterCancel(t *testing.T) {
	e := testRelayEngine()
	release := make(chan struct{})
	late := newFakeRelayConn("late")
	e.dialRelay = func(*net.UDPAddr) (relayConn, error) {
		<-release
		return late, nil
	}
	rd := testRelayData("r1")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, _, err := e.connectOneRelay(ctx, rd, &rd.endpoints[0], [9]uint32{})
		errc <- err
	}()
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("connectOneRelay err = %v, want context.Canceled", err)
	}
	close(release)
	waitFor(t, "late channel to be closed", late.closed.Load)
}

func TestConnectOneRelayClosesLateChannelAfterTimeout(t *testing.T) {
	old := relayConnectTimeout
	relayConnectTimeout = 20 * time.Millisecond
	t.Cleanup(func() { relayConnectTimeout = old })
	e := testRelayEngine()
	release := make(chan struct{})
	late := newFakeRelayConn("late")
	e.dialRelay = func(*net.UDPAddr) (relayConn, error) {
		<-release
		return late, nil
	}
	rd := testRelayData("r1")
	if _, _, err := e.connectOneRelay(context.Background(), rd, &rd.endpoints[0], [9]uint32{}); err == nil {
		t.Fatal("connectOneRelay succeeded past its timeout")
	}
	close(release)
	waitFor(t, "late channel to be closed", late.closed.Load)
}

func TestConnectAndAllocateAllSkipsDialsAfterCancel(t *testing.T) {
	e := testRelayEngine()
	var dials atomic.Int32
	e.dialRelay = func(*net.UDPAddr) (relayConn, error) {
		dials.Add(1)
		return newFakeRelayConn("x"), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.connectAndAllocateAll(ctx, testRelayData("r1", "r2", "r3"), [9]uint32{}, true, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := dials.Load(); n != 0 {
		t.Fatalf("dialed %d relays after cancel, want 0", n)
	}
}

func TestConnectAndAllocateAllClosesChannelsWhenCancelledMidConnect(t *testing.T) {
	e := testRelayEngine()
	release := make(chan struct{})
	var mu sync.Mutex
	var conns []*fakeRelayConn
	var started sync.WaitGroup
	started.Add(3)
	e.dialRelay = func(*net.UDPAddr) (relayConn, error) {
		started.Done()
		<-release
		c := newFakeRelayConn("x")
		mu.Lock()
		conns = append(conns, c)
		mu.Unlock()
		return c, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := e.connectAndAllocateAll(ctx, testRelayData("r1", "r2", "r3"), [9]uint32{}, true, true)
		errc <- err
	}()
	started.Wait()
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	close(release)
	waitFor(t, "all three late channels closed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(conns) != 3 {
			return false
		}
		for _, c := range conns {
			if !c.closed.Load() {
				return false
			}
		}
		return true
	})
}

func TestConnectAndAllocateAllConnectsInParallel(t *testing.T) {
	e := testRelayEngine()
	var arrived sync.WaitGroup
	arrived.Add(2)
	allIn := make(chan struct{})
	go func() { arrived.Wait(); close(allIn) }()
	var mu sync.Mutex
	conns := map[string]*fakeRelayConn{}
	e.dialRelay = func(addr *net.UDPAddr) (relayConn, error) {
		arrived.Done()
		select {
		case <-allIn: // both dials are in flight at once
		case <-time.After(time.Second):
			return nil, errors.New("dials ran one after another")
		}
		c := newFakeRelayConn(addr.String())
		mu.Lock()
		conns[addr.String()] = c
		mu.Unlock()
		return c, nil
	}
	f, err := e.connectAndAllocateAll(context.Background(), testRelayData("r1", "r2"), [9]uint32{}, true, true)
	if err != nil {
		t.Fatalf("connectAndAllocateAll: %v", err)
	}
	defer f.Close()
	if len(f.chans) != 2 || f.names[0] != "r1" || f.names[1] != "r2" {
		t.Fatalf("fanout names = %v, want [r1 r2]", f.names)
	}
	for addr, c := range conns {
		if c.sentCount() != 1 {
			t.Fatalf("relay %s got %d sends, want one allocate", addr, c.sentCount())
		}
	}
}

func TestFanoutRecvUnblocksOnCancel(t *testing.T) {
	a, b := newFakeRelayConn("a"), newFakeRelayConn("b")
	f := newRelayFanout([]relayConn{a, b}, [][]byte{nil, nil}, []string{"a", "b"})
	ctx, cancel := context.WithCancel(context.Background())
	defer closeFanoutOnCancel(ctx, f)()
	errc := make(chan error, 1)
	go func() {
		_, err := f.Recv(make([]byte, 64))
		errc <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Recv returned a packet after cancel, want an error")
		}
	case <-time.After(time.Second):
		t.Fatal("Recv still blocked 1 s after cancel")
	}
	if !a.closed.Load() || !b.closed.Load() {
		t.Fatal("relay channels left open after cancel")
	}
}
