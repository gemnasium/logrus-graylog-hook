package graylog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/SocialCodeInc/go-gelf/gelf"
)

// Set graylog.BufSize = <value> _before_ calling NewGraylogHook
// Once the buffer is full, logging will start blocking, waiting for slots to
// be available in the queue.
var BufSize uint = 8192

// GraylogHook to send logs to a logging service compatible with the Graylog API and the GELF format.
type GraylogHook struct {
	Extra       map[string]interface{}
	gelfLogger  *gelf.Writer
	buf         chan graylogEntry
	wg          sync.WaitGroup
	mu          sync.RWMutex
	synchronous bool
}

// Graylog needs file and line params
type graylogEntry struct {
	*logrus.Entry
	file string
	line int
}

// NewGraylogHook creates a hook to be added to an instance of logger.
func NewGraylogHook(addr string, extra map[string]interface{}) *GraylogHook {
	g, err := gelf.NewWriter(addr)
	if err != nil {
		logrus.WithField("err", err).Info("Can't create Gelf logger")
	}
	hook := &GraylogHook{
		Extra:       extra,
		gelfLogger:  g,
		synchronous: true,
	}
	return hook
}

// NewAsyncGraylogHook creates a hook to be added to an instance of logger.
// The hook created will be asynchronous, and it's the responsibility of the user to call the Flush method
// before exiting to empty the log queue.
func NewAsyncGraylogHook(addr string, extra map[string]interface{}) *GraylogHook {
	g, err := gelf.NewWriter(addr)
	if err != nil {
		logrus.WithField("err", err).Info("Can't create Gelf logger")
	}
	hook := &GraylogHook{
		Extra:      extra,
		gelfLogger: g,
		buf:        make(chan graylogEntry, BufSize),
	}
	go hook.fire() // Log in background
	return hook
}

// Fire is called when a log event is fired.
// We assume the entry will be altered by another hook,
// otherwise we might logging something wrong to Graylog
func (hook *GraylogHook) Fire(entry *logrus.Entry) error {
	hook.mu.RLock() // Claim the mutex as a RLock - allowing multiple go routines to log simultaneously
	defer hook.mu.RUnlock()

	// get caller file and line here, it won't be available inside the goroutine
	// 1 for the function that called us.
	file, line := getCallerIgnoringLogMulti(1)

	newData := make(map[string]interface{})
	for k, v := range entry.Data {
		newData[k] = v
	}

	newEntry := &logrus.Entry{entry.Logger, newData, entry.Time, entry.Level, entry.Message}
	gEntry := graylogEntry{newEntry, file, line}

	if hook.synchronous {
		hook.sendEntry(gEntry)
	} else {
		hook.wg.Add(1)
		hook.buf <- gEntry
	}

	return nil
}

// Flush waits for the log queue to be empty.
// This func is meant to be used when the hook was created with NewAsyncGraylogHook.
func (hook *GraylogHook) Flush() {
	hook.mu.Lock() // claim the mutex as a Lock - we want exclusive access to it
	defer hook.mu.Unlock()

	hook.wg.Wait()
}

// fire will loop on the 'buf' channel, and write entries to graylog
func (hook *GraylogHook) fire() {
	for {
		entry := <-hook.buf // receive new entry on channel
		hook.sendEntry(entry)
		hook.wg.Done()
	}
}

// sendEntry sends an entry to graylog synchronously
func (hook *GraylogHook) sendEntry(entry graylogEntry) {
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}

	w := hook.gelfLogger

	// remove trailing and leading whitespace
	p := bytes.TrimSpace([]byte(entry.Message))

	// If there are newlines in the message, use the first line
	// for the short message and set the full message to the
	// original input.  If the input has no newlines, stick the
	// whole thing in Short.
	short := p
	full := []byte("")
	if i := bytes.IndexRune(p, '\n'); i > 0 {
		short = p[:i]
		full = p
	}

	level := int32(entry.Level) + 2 // logrus levels are lower than syslog by 2

	// Don't modify entry.Data directly, as the entry will used after this hook was fired
	extra := map[string]interface{}{}
	// Merge extra fields
	for k, v := range hook.Extra {
		k = fmt.Sprintf("_%s", k) // "[...] every field you send and prefix with a _ (underscore) will be treated as an additional field."
		extra[k] = v
	}
	for k, v := range entry.Data {
		extraK := fmt.Sprintf("_%s", k) // "[...] every field you send and prefix with a _ (underscore) will be treated as an additional field."
		if k == logrus.ErrorKey {
			asError, isError := v.(error)
			_, isMarshaler := v.(json.Marshaler)
			if isError && !isMarshaler {
				extra[extraK] = newMarshalableError(asError)
			} else {
				extra[extraK] = v
			}
		} else {
			extra[extraK] = v
		}
	}

	m := gelf.Message{
		Version:  "1.1",
		Host:     host,
		Short:    string(short),
		Full:     string(full),
		TimeUnix: time.Now().Unix(),
		Level:    level,
		File:     entry.file,
		Line:     entry.line,
		Extra:    extra,
	}

	if err := w.WriteMessage(&m); err != nil {
		fmt.Println(err)
	}

}

// Levels returns the available logging levels.
func (hook *GraylogHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
		logrus.DebugLevel,
	}
}

// getCaller returns the filename and the line info of a function
// further down in the call stack.  Passing 0 in as callDepth would
// return info on the function calling getCallerIgnoringLog, 1 the
// parent function, and so on.  Any suffixes passed to getCaller are
// path fragments like "/pkg/log/log.go", and functions in the call
// stack from that file are ignored.
func getCaller(callDepth int, suffixesToIgnore ...string) (file string, line int) {
	// bump by 1 to ignore the getCaller (this) stackframe
	callDepth++
outer:
	for {
		var ok bool
		_, file, line, ok = runtime.Caller(callDepth)
		if !ok {
			file = "???"
			line = 0
			break
		}

		for _, s := range suffixesToIgnore {
			if strings.HasSuffix(file, s) {
				callDepth++
				continue outer
			}
		}
		break
	}
	return
}

func getCallerIgnoringLogMulti(callDepth int) (string, int) {
	// the +1 is to ignore this (getCallerIgnoringLogMulti) frame
	return getCaller(callDepth+1, "logrus/hooks.go", "logrus/entry.go", "logrus/logger.go", "logrus/exported.go", "asm_amd64.s")
}
