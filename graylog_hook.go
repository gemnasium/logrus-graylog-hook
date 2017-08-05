package graylog

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	stackTraceKey                = "_stacktrace"
	defaultBufSize          uint = 8192
	defaultCompression           = CompressGzip
	defaultCompressionLevel      = flate.BestSpeed
	defaultTimeout               = 500 //500ms
)

// Hook to send logs to a logging service compatible with the Graylog API and the GELF format.
type Hook struct {
	options    *Options
	gelfLogger *Writer
	buf        chan graylogEntry
	wg         *sync.WaitGroup
	closeChan  chan struct{}
}

// Graylog needs file and line params
type graylogEntry struct {
	*logrus.Entry
	file string
	line int
}

// NewGraylogHook create a hook to be added to an instance of logger
func NewGraylogHook(addr string, ops ...LogOption) *Hook {
	logOption := getDefaultGraylogOptions()
	if nil != ops {
		for _, o := range ops {
			o(logOption)
		}
	}
	g, err := NewWriter(addr, logOption.CompressType, logOption.CompressLevel)
	if err != nil {
		logrus.WithError(err).Error("Can't create Gelf logger")
		return nil
	}

	hook := &Hook{
		options:    logOption,
		gelfLogger: g,
		buf:        make(chan graylogEntry, logOption.BufSize),
		wg:         &sync.WaitGroup{},
		closeChan:  make(chan struct{}),
	}
	hook.wg.Add(1)
	go hook.fire()
	return hook
}

// Fire is called when a log event is fired.
// We assume the entry will be altered by another hook,
// otherwise we might logging something wrong to Graylog
func (hook *Hook) Fire(entry *logrus.Entry) error {

	// get caller file and line here, it won't be available inside the goroutine
	// 1 for the function that called us.
	file, line := getCallerIgnoringLogMulti(1)

	newData := make(map[string]interface{})
	for k, v := range entry.Data {
		newData[k] = v
	}

	newEntry := &logrus.Entry{
		Logger:  entry.Logger,
		Data:    newData,
		Time:    entry.Time,
		Level:   entry.Level,
		Message: entry.Message,
	}
	gEntry := graylogEntry{newEntry, file, line}

	select {
	case <-hook.closeChan: // when someone request to exit, then we drop the
		fmt.Printf("logentry:%#v\n", entry)
	case hook.buf <- gEntry:
	case <-time.After(time.Duration(hook.options.Timeout) * time.Millisecond): // when it is timeout
		fmt.Printf("GaylogHook: timeout , fail to process log entry")
	}

	return nil
}

// Flush waits for the log queue to be empty.
// This func is meant to be used when the hook was created with NewAsyncGraylogHook.
func (hook *Hook) Flush() {
	// close the close chan will stop further logentry get into
	close(hook.closeChan)
	close(hook.buf)
	hook.wg.Wait()
}

// fire will loop on the 'buf' channel, and write entries to graylog
func (hook *Hook) fire() {
	defer hook.wg.Done()
	for {
		entry, more := <-hook.buf // receive new entry on channel
		if !more {
			// channel closed
			return
		}
		hook.sendEntry(entry)
	}
}

// sendEntry sends an entry to graylog synchronously
func (hook *Hook) sendEntry(entry graylogEntry) {
	if hook.gelfLogger == nil {
		fmt.Println("Can't connect to Graylog")
		return
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
	for k, v := range hook.options.Extra {
		k = fmt.Sprintf("_%s", k) // "[...] every field you send and prefix with a _ (underscore) will be treated as an additional field."
		extra[k] = v
	}
	for k, v := range entry.Data {
		// blacklist field, drop it
		if hook.options.Blacklist[k] {
			continue
		}

		extraK := fmt.Sprintf("_%s", k) // "[...] every field you send and prefix with a _ (underscore) will be treated as an additional field."
		if k == logrus.ErrorKey {
			asError, isError := v.(error)
			_, isMarshaler := v.(json.Marshaler)
			if isError && !isMarshaler {
				extra[extraK] = newMarshalableError(asError)
			} else {
				extra[extraK] = v
			}
			if stackTrace := extractStackTrace(asError); stackTrace != nil {
				extra[stackTraceKey] = fmt.Sprintf("%+v", stackTrace)
				file, line := extractFileAndLine(stackTrace)
				if file != "" && line != 0 {
					entry.file = file
					entry.line = line
				}
			}
		} else {
			extra[extraK] = v
		}

	}

	m := Message{
		Version:  "1.1",
		Host:     hook.options.Host,
		Short:    string(short),
		Full:     string(full),
		TimeUnix: float64(time.Now().UnixNano()/1000000) / 1000.,
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
func (hook *Hook) Levels() []logrus.Level {
	levels := []logrus.Level{}
	for _, level := range logrus.AllLevels {
		if level <= hook.options.Level {
			levels = append(levels, level)
		}
	}
	return levels
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
