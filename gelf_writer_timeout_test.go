package graylog

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func TestHTTPWriterDefaultTimeout(t *testing.T) {
	w, err := NewWriter("http://127.0.0.1:1/gelf")
	if err != nil {
		t.Fatalf("NewWriter: %s", err)
	}
	if got := w.(HTTPWriter).httpClient.Timeout; got != DefaultHTTPTimeout {
		t.Errorf("default HTTP timeout: expected %s, got %s", DefaultHTTPTimeout, got)
	}

	w, err = NewWriter("http://127.0.0.1:1/gelf", WithTimeout(0))
	if err != nil {
		t.Fatalf("NewWriter: %s", err)
	}
	if got := w.(HTTPWriter).httpClient.Timeout; got != 0 {
		t.Errorf("WithTimeout(0) should disable the HTTP timeout, got %s", got)
	}
}

func TestHTTPWriterTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		<-release
		rw.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	defer close(release)

	w, err := NewWriter(server.URL, WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewWriter: %s", err)
	}

	start := time.Now()
	err = w.WriteMessage(&Message{Version: "1.1", Short: "slow"})
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("WriteMessage took %s, timeout was not applied", elapsed)
	}
}

func TestTCPDialTimeout(t *testing.T) {
	// 10.255.255.1 is not routable, so the connection attempt hangs until
	// the dial timeout fires.
	start := time.Now()
	_, err := NewWriter("tcp://10.255.255.1:12201", WithTimeout(100*time.Millisecond))
	if err == nil {
		t.Skip("unexpectedly connected to a non-routable address")
	}
	if !isTimeout(err) {
		t.Skipf("network fails fast here, can't exercise the dial timeout: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("dial took %s, timeout was not applied", elapsed)
	}
}

func TestTCPWriteTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %s", err)
	}
	defer listener.Close()

	// Accept the connection but never read from it, so the socket buffers
	// fill up and writes block.
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetReadBuffer(1024)
		}
		accepted <- conn
	}()

	g, err := NewWriter("tcp://"+listener.Addr().String(), WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("NewWriter: %s", err)
	}
	w := g.(*LowLevelProtocolWriter)
	w.CompressionType = NoCompress
	if tc, ok := w.conn.(*net.TCPConn); ok {
		tc.SetWriteBuffer(1024)
	}
	defer func() {
		select {
		case conn := <-accepted:
			conn.Close()
		default:
		}
	}()

	// Random hex so the payload stays large; ~300KB fits in 255 chunks.
	payload := make([]byte, 150*1024)
	if _, err := io.ReadFull(rand.Reader, payload); err != nil {
		t.Fatal(err)
	}
	msg := &Message{Version: "1.1", Short: hex.EncodeToString(payload)}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		err = w.WriteMessage(msg)
		if err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("writes never blocked; expected a write timeout")
	}
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got %v", err)
	}
}

func TestUDPWriteDeadlineDisabledByDefault(t *testing.T) {
	r, err := NewUDPReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPReader: %s", err)
	}
	g, err := NewWriter(r.Addr())
	if err != nil {
		t.Fatalf("NewWriter: %s", err)
	}
	if got := g.(*LowLevelProtocolWriter).Timeout; got != 0 {
		t.Errorf("UDP writer should have no timeout by default, got %s", got)
	}
	if err := g.WriteMessage(&Message{Version: "1.1", Short: "hello"}); err != nil {
		t.Fatalf("WriteMessage: %s", err)
	}
	msg, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %s", err)
	}
	if msg.Short != "hello" {
		t.Errorf("msg.Short: expected hello, got %s", msg.Short)
	}
}

func TestHookWithTimeoutOption(t *testing.T) {
	r, err := NewUDPReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), nil, WithTimeout(time.Second))
	if got := hook.Writer().(*LowLevelProtocolWriter).Timeout; got != time.Second {
		t.Errorf("hook writer timeout: expected 1s, got %s", got)
	}

	asyncHook := NewAsyncGraylogHook(r.Addr(), nil, WithTimeout(time.Second))
	if got := asyncHook.Writer().(*LowLevelProtocolWriter).Timeout; got != time.Second {
		t.Errorf("async hook writer timeout: expected 1s, got %s", got)
	}
}

func TestHookNotNilWhenWriterFails(t *testing.T) {
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	hook := NewGraylogHook("tcp://127.0.0.1:1", nil, WithTimeout(100*time.Millisecond))
	if hook == nil {
		t.Fatal("NewGraylogHook must not return nil when the writer can't be created")
	}

	log := logrus.New()
	log.Out = io.Discard
	log.AddHook(hook) // must not panic
	log.Info("dropped")
}
