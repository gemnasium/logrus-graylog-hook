package graylog

import (
	"strings"
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/SocialCodeInc/go-gelf/gelf"
)

const SyslogInfoLevel = 6

func TestWritingToUDP(t *testing.T) {
	r, err := gelf.NewReader("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewReader: %s", err)
	}
	hook := NewGraylogHook(r.Addr(), "test_facility", map[string]interface{}{"foo": "bar"})
	msgData := "test message\nsecond line"

	log := logrus.New()
	log.Hooks.Add(hook)
	log.WithField("withField", "1").Info(msgData)

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

	if msg.Line != 23 { // Update this if code is updated above
		t.Errorf("msg.Line: expected %d, got %d", 25, msg.Line)
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
