package eventbus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/eventbus"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := eventbus.NewBus()
	ch, cancel := bus.Subscribe(8)
	defer cancel()

	ev := eventbus.NewEvent(eventbus.TypeServiceStarted, "test", map[string]string{"service": "foo"})
	bus.Publish(ev)

	select {
	case got := <-ch:
		if got.Type != eventbus.TypeServiceStarted {
			t.Errorf("got type %q want %q", got.Type, eventbus.TypeServiceStarted)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBusSlowSubscriberDrops(t *testing.T) {
	bus := eventbus.NewBus()
	// Subscriber with buffer of 1.
	ch, cancel := bus.Subscribe(1)
	defer cancel()

	// Publish 3 events; only 1 fits in the buffer.
	for i := 0; i < 3; i++ {
		bus.Publish(eventbus.NewEvent(eventbus.TypeDiskWarn, "test", nil))
	}

	// Exactly 1 event should be in the channel (the rest were dropped).
	if len(ch) != 1 {
		t.Errorf("expected 1 buffered event, got %d", len(ch))
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := eventbus.NewBus()
	ch1, cancel1 := bus.Subscribe(4)
	ch2, cancel2 := bus.Subscribe(4)
	defer cancel1()
	defer cancel2()

	bus.Publish(eventbus.NewEvent(eventbus.TypeDiskCritical, "test", nil))

	for _, ch := range []<-chan eventbus.Event{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Error("timeout waiting for event on subscriber")
		}
	}
}

func TestBusCancelRemovesSubscriber(t *testing.T) {
	bus := eventbus.NewBus()
	_, cancel := bus.Subscribe(4)
	cancel()
	// Publish after cancel must not panic.
	bus.Publish(eventbus.NewEvent(eventbus.TypeServiceFailed, "test", nil))
}

// --- Server integration tests ---

func startTestServer(t *testing.T) (sockPath string, bus *eventbus.Bus, stop func()) {
	t.Helper()
	// Create temp dir under /tmp (not the default TempDir) to keep the path
	// short enough for macOS's 104-byte Unix socket limit.
	dir, err := os.MkdirTemp("/tmp", "ne")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sockPath = filepath.Join(dir, "s.sock")
	bus = eventbus.NewBus()

	ctx, cancel := context.WithCancel(context.Background())
	srv := eventbus.NewServer(sockPath, bus, nil) // nil log: server guards log calls

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	// Wait for socket to appear.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return sockPath, bus, func() {
		cancel()
		<-errCh
	}
}

func TestServerSubscribeReceivesEvents(t *testing.T) {
	sockPath, bus, stop := startTestServer(t)
	defer stop()

	// Connect as subscriber.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, `{"subscribe":true}`+"\n")

	// Publish an event.
	time.Sleep(20 * time.Millisecond) // ensure subscriber is registered
	bus.Publish(eventbus.NewEvent(eventbus.TypeServiceStarted, "test", map[string]string{"service": "gateway"}))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	dec := json.NewDecoder(conn)
	var got eventbus.Event
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if got.Type != eventbus.TypeServiceStarted {
		t.Errorf("got type %q want %q", got.Type, eventbus.TypeServiceStarted)
	}
}

func TestServerPublishFromExternal(t *testing.T) {
	sockPath, bus, stop := startTestServer(t)
	defer stop()

	// Subscribe via bus directly.
	ch, cancel := bus.Subscribe(4)
	defer cancel()

	// Publish via socket.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	ev := eventbus.NewEvent(eventbus.TypeDiskWarn, "external", map[string]float64{"used_pct": 88.5})
	data, _ := json.Marshal(ev)
	fmt.Fprintf(conn, "%s\n", data)
	conn.Close()

	select {
	case got := <-ch:
		if got.Type != eventbus.TypeDiskWarn {
			t.Errorf("got type %q", got.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for externally published event")
	}
}
