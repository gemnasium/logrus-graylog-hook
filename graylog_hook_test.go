package graylog

import (
	"compress/flate"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	pkgerrors "github.com/pkg/errors"
)

const SyslogInfoLevel = 6
const SyslogErrorLevel = 7

func TestWritingToUDP(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), map[string]interface{}{"foo": "bar"})
	hook.Host = "testing.local"
	hook.Blacklist([]string{"filterMe"})
	msgData := "test message\nsecond line"

	log := logrus.New()
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)
	log.WithFields(logrus.Fields{"withField": "1", "filterMe": "1"}).Info(msgData)

	msg, err := r.ReadMessage()

	if err != nil {
		t.Errorf("ReadMessage: %s", err)
	}

	if msg.Short != "test message" {
		t.Errorf("msg.Short: expected %s, got %s", msgData, msg.Full)
	}

	if msg.Full != msgData {
		t.Errorf("msg.Full: expected %s, got %s", msgData, msg.Full)
	}

	if msg.Level != SyslogInfoLevel {
		t.Errorf("msg.Level: expected: %d, got %d)", SyslogInfoLevel, msg.Level)
	}

	if msg.Host != "testing.local" {
		t.Errorf("Host should match (exp: testing.local, got: %s)", msg.Host)
	}

	if len(msg.Extra) != 2 {
		t.Errorf("wrong number of extra fields (exp: %d, got %d) in %v", 2, len(msg.Extra), msg.Extra)
	}

	fileExpected := "graylog_hook_test.go"
	if !strings.HasSuffix(msg.File, fileExpected) {
		t.Errorf("msg.File: expected %s, got %s", fileExpected,
			msg.File)
	}

	lineExpected := 34 // Update this if code is updated above
	if msg.Line != lineExpected {
		t.Errorf("msg.Line: expected %d, got %d", lineExpected, msg.Line)
	}

	if len(msg.Extra) != 2 {
		t.Errorf("wrong number of extra fields (exp: %d, got %d) in %v", 2, len(msg.Extra), msg.Extra)
	}

	extra := map[string]interface{}{"foo": "bar", "withField": "1"}

	for k, v := range extra {
		// Remember extra fileds are prefixed with "_"
		if msg.Extra["_"+k].(string) != extra[k].(string) {
			t.Errorf("Expected extra '%s' to be %#v, got %#v", k, v, msg.Extra["_"+k])
		}
	}
}

func testErrorLevelReporting(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), map[string]interface{}{"foo": "bar"})
	msgData := "test message\nsecond line"

	log := logrus.New()
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)

	log.Error(msgData)

	msg, err := r.ReadMessage()

	if err != nil {
		t.Errorf("ReadMessage: %s", err)
	}

	if msg.Short != "test message" {
		t.Errorf("msg.Short: expected %s, got %s", msgData, msg.Full)
	}

	if msg.Full != msgData {
		t.Errorf("msg.Full: expected %s, got %s", msgData, msg.Full)
	}

	if msg.Level != SyslogErrorLevel {
		t.Errorf("msg.Level: expected: %d, got %d)", SyslogErrorLevel, msg.Level)
	}
}

func TestJSONErrorMarshalling(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), map[string]interface{}{})

	log := logrus.New()
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)

	log.WithError(errors.New("sample error")).Info("Testing sample error")

	msg, err := r.ReadMessage()
	if err != nil {
		t.Errorf("ReadMessage: %s", err)
	}

	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("Marshaling json: %s", err)
	}

	errSection := regexp.MustCompile(`"_error":"sample error"`)
	if !errSection.MatchString(string(encoded)) {
		t.Errorf("Expected error message to be encoded into message")
	}
}

func TestParallelLogging(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), nil)
	asyncHook := NewAsyncGraylogHook(r.Addr(), nil)

	log := logrus.New()
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)
	log.Hooks.Add(asyncHook)

	quit := make(chan struct{})
	defer close(quit)

	panicked := false

	recordPanic := func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}

	go func() {
		// Start draining messages from GELF
		go func() {
			defer recordPanic()
			for {
				select {
				case <-quit:
					return
				default:
					r.ReadMessage()
				}
			}
		}()

		// Log into our hook in parallel
		for i := 0; i < 10; i++ {
			go func() {
				defer recordPanic()
				for {
					select {
					case <-quit:
						return
					default:
						log.Info("Logging")
					}
				}
			}()
		}
	}()

	// Let them all do their thing for a while
	time.Sleep(100 * time.Millisecond)
	if panicked {
		t.Fatalf("Logging in parallel caused a panic")
	}
}

func TestSetWriter(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), nil)

	w := hook.Writer()
	w.CompressionLevel = flate.BestCompression
	hook.SetWriter(w)

	if hook.Writer().CompressionLevel != flate.BestCompression {
		t.Error("Writer was not set correctly")
	}

	if hook.SetWriter(nil) == nil {
		t.Error("Setting a nil writter should raise an error")
	}
}

func TestWithInvalidGraylogAddr(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "localhost:0")
	if err != nil {
		panic(err)
	}
	logrus.SetOutput(ioutil.Discard)
	hook := NewGraylogHook(addr.String(), nil)

	log := logrus.New()
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)

	// Should not panic
	log.WithError(errors.New("sample error")).Info("Testing sample error")
}

func TestStackTracer(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), map[string]interface{}{})

	log := logrus.New()
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)

	stackErr := pkgerrors.New("sample error")

	log.WithError(stackErr).Info("Testing sample error")

	msg, err := r.ReadMessage()
	if err != nil {
		t.Errorf("ReadMessage: %s", err)
	}

	fileExpected := "graylog_hook_test.go"
	if !strings.HasSuffix(msg.File, fileExpected) {
		t.Errorf("msg.File: expected %s, got %s", fileExpected,
			msg.File)
	}

	lineExpected := 257 // Update this if code is updated above
	if msg.Line != lineExpected {
		t.Errorf("msg.Line: expected %d, got %d", lineExpected, msg.Line)
	}

	stacktraceI, ok := msg.Extra[StackTraceKey]
	if !ok {
		t.Error("Stack Trace not found in result")
	}
	stacktrace, ok := stacktraceI.(string)
	if !ok {
		t.Error("Stack Trace is not a string")
	}
	stacktraceRE := regexp.MustCompile(`^
.+/logrus-graylog-hook(%2ev2)?.TestStackTracer
	/.+/logrus-graylog-hook(.v2)?/graylog_hook_test.go:\d+
testing.tRunner
	/.*/testing.go:\d+
runtime.*
	/.*/runtime/.*:\d+$`)
	if !stacktraceRE.MatchString(stacktrace) {
		t.Errorf("Stack Trace not as expected. Got:\n%s\n", stacktrace)
	}
}
