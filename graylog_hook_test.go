package graylog

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/SocialCodeInc/go-gelf/gelf"
)

const SyslogInfoLevel = 6
const SyslogErrorLevel = 7

func TestWritingToUDP(t *testing.T) {
	r, err := gelf.NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), "test_facility", map[string]interface{}{"foo": "bar"})
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

	if msg.Facility != "test_facility" {
		t.Errorf("msg.Facility: expected %#v, got %#v)", "test_facility", msg.Facility)
	}

	if len(msg.Extra) != 2 {
		t.Errorf("wrong number of extra fields (exp: %d, got %d) in %v", 2, len(msg.Extra), msg.Extra)
	}

	fileExpected := "graylog_hook_test.go"
	if !strings.HasSuffix(msg.File, fileExpected) {
		t.Errorf("msg.File: expected %s, got %s", fileExpected,
			msg.File)
	}

	if msg.Line != 31 { // Update this if code is updated above
		t.Errorf("msg.Line: expected %d, got %d", 28, msg.Line)
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
	r, err := gelf.NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), "test_facility", map[string]interface{}{"foo": "bar"})
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
	r, err := gelf.NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), "test_facility", map[string]interface{}{})

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
	r, err := gelf.NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), "test_facility", nil)
	asyncHook := NewAsyncGraylogHook(r.Addr(), "test_facility", nil)

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
