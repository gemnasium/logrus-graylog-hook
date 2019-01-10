package graylog

import (
	"compress/flate"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
		t.Errorf("wrong number of extra fields (exp: %d, got %d) in %v", 5, len(msg.Extra), msg.Extra)
	}

	fileExpected := ""
	if msg.File != fileExpected {
		t.Errorf("msg.File: expected %s, got %s", fileExpected,
			msg.File)
	}

	lineExpected := 0
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
	log.SetReportCaller(true)
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

	lineExpected := 261 // Update this if code is updated above
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

	// Run the test for stack trace only in stable versions
	if !strings.Contains(runtime.Version(), "devel") {
		stacktraceRE := regexp.MustCompile(`^
(.+)?logrus-graylog-hook(/v3)?.TestStackTracer
	(/|[A-Z]:/).+/logrus-graylog-hook(.v3)?/graylog_hook_test.go:\d+
testing.tRunner
	(/|[A-Z]:/).*/testing.go:\d+
runtime.*
	(/|[A-Z]:/).*/runtime/.*:\d+$`)

		if !stacktraceRE.MatchString(stacktrace) {
			t.Errorf("Stack Trace not as expected. Got:\n%s\n", stacktrace)
		}
	}
}

func TestLogrusLevelToSylog(t *testing.T) {
	// Syslog constants
	const (
		LOG_EMERG   = 0 /* system is unusable */
		LOG_ALERT   = 1 /* action must be taken immediately */
		LOG_CRIT    = 2 /* critical conditions */
		LOG_ERR     = 3 /* error conditions */
		LOG_WARNING = 4 /* warning conditions */
		LOG_NOTICE  = 5 /* normal but significant condition */
		LOG_INFO    = 6 /* informational */
		LOG_DEBUG   = 7 /* debug-level messages */
	)

	if logrusLevelToSylog(logrus.TraceLevel) != LOG_DEBUG {
		t.Error("logrusLevelToSylog(TraceLevel) != LOG_DEBUG")
	}

	if logrusLevelToSylog(logrus.DebugLevel) != LOG_DEBUG {
		t.Error("logrusLevelToSylog(DebugLevel) != LOG_DEBUG")
	}

	if logrusLevelToSylog(logrus.InfoLevel) != LOG_INFO {
		t.Error("logrusLevelToSylog(InfoLevel) != LOG_INFO")
	}

	if logrusLevelToSylog(logrus.WarnLevel) != LOG_WARNING {
		t.Error("logrusLevelToSylog(WarnLevel) != LOG_WARNING")
	}

	if logrusLevelToSylog(logrus.ErrorLevel) != LOG_ERR {
		t.Error("logrusLevelToSylog(ErrorLevel) != LOG_ERR")
	}

	if logrusLevelToSylog(logrus.FatalLevel) != LOG_CRIT {
		t.Error("logrusLevelToSylog(FatalLevel) != LOG_CRIT")
	}

	if logrusLevelToSylog(logrus.PanicLevel) != LOG_ALERT {
		t.Error("logrusLevelToSylog(PanicLevel) != LOG_ALERT")
	}
}

func TestReportCallerEnabled(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), map[string]interface{}{})
	hook.Host = "testing.local"
	msgData := "test message\nsecond line"

	log := logrus.New()
	log.SetReportCaller(true)
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)
	log.Info(msgData)

	msg, err := r.ReadMessage()

	if err != nil {
		t.Errorf("ReadMessage: %s", err)
	}

	fileField, ok := msg.Extra["_file"]
	if !ok {
		t.Error("_file field not present in extra fields")
	}

	fileGot, ok := fileField.(string)
	if !ok {
		t.Error("_file field is not a string")
	}

	fileExpected := "graylog_hook_test.go"
	if !strings.HasSuffix(fileGot, fileExpected) {
		t.Errorf("msg.Extra[\"_file\"]: expected %s, got %s", fileExpected, fileGot)
	}

	lineField, ok := msg.Extra["_line"]
	if !ok {
		t.Error("_line field not present in extra fields")
	}

	lineGot, ok := lineField.(float64)
	if !ok {
		t.Error("_line dowes not have the correct type")
	}

	lineExpected := 359 // Update this if code is updated above
	if msg.Line != lineExpected {
		t.Errorf("msg.Extra[\"_line\"]: expected %d, got %d", lineExpected, int(lineGot))
	}

	functionField, ok := msg.Extra["_function"]
	if !ok {
		t.Error("_function field not present in extra fields")
	}

	functionGot, ok := functionField.(string)
	if !ok {
		t.Error("_function field is not a string")
	}

	functionExpected := "TestReportCallerEnabled"
	if !strings.HasSuffix(functionGot, functionExpected) {
		t.Errorf("msg.Extra[\"_function\"]: expected %s, got %s", functionExpected, functionGot)
	}

	gelfFileExpected := "graylog_hook_test.go"
	if !strings.HasSuffix(msg.File, gelfFileExpected) {
		t.Errorf("msg.File: expected %s, got %s", gelfFileExpected,
			msg.File)
	}

	gelfLineExpected := 359 // Update this if code is updated above
	if msg.Line != lineExpected {
		t.Errorf("msg.Line: expected %d, got %d", gelfLineExpected, msg.Line)
	}
}

func TestReportCallerDisabled(t *testing.T) {
	r, err := NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), map[string]interface{}{})
	hook.Host = "testing.local"
	msgData := "test message\nsecond line"

	log := logrus.New()
	log.SetReportCaller(false)
	log.Out = ioutil.Discard
	log.Hooks.Add(hook)
	log.Info(msgData)

	msg, err := r.ReadMessage()

	if err != nil {
		t.Errorf("ReadMessage: %s", err)
	}

	if _, ok := msg.Extra["_file"]; ok {
		t.Error("_file field should not present in extra fields")
	}

	if _, ok := msg.Extra["_line"]; ok {
		t.Error("_line field should not present in extra fields")
	}

	if _, ok := msg.Extra["_function"]; ok {
		t.Error("_function field should not present in extra fields")
	}

	// if reportCaller is disabled (this is the default setting) the File and Line field should have the default values
	// corresponding to the types. "" and 0 respectively.
	gelfFileExpected := ""
	if msg.File != gelfFileExpected {
		t.Errorf("msg.File: expected %s, got %s", gelfFileExpected, msg.File)
	}

	gelfLineExpected := 0
	if msg.Line != gelfLineExpected {
		t.Errorf("msg.Line: expected %d, got %d", gelfLineExpected, msg.Line)
	}
}
