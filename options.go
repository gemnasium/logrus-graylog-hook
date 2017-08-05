package graylog

import (
	"os"

	"github.com/sirupsen/logrus"
)

// Options the additional options
type Options struct {
	Extra         map[string]interface{}
	Host          string
	Level         logrus.Level
	Blacklist     map[string]bool
	BufSize       uint
	CompressType  CompressType
	CompressLevel int
	Timeout       int
}

// LogOption define the functions you can used to change the options
type LogOption func(*Options)

func getDefaultGraylogOptions() *Options {
	logOptions := &Options{
		Extra:         make(map[string]interface{}),
		Level:         logrus.DebugLevel,
		Blacklist:     make(map[string]bool),
		BufSize:       defaultBufSize,
		CompressType:  defaultCompression,
		CompressLevel: defaultCompressionLevel,
		Timeout:       defaultTimeout,
	}
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}
	logOptions.Host = host
	return logOptions
}

// WithExtra apply some additional fields you want it to pass to graylog server
func WithExtra(extra map[string]interface{}) LogOption {
	return func(ops *Options) {
		if nil == extra {
			return
		}
		for k, value := range extra {
			ops.Extra[k] = value
		}
	}
}

// WithHost set the current host
func WithHost(host string) LogOption {
	return func(ops *Options) {
		ops.Host = host
	}
}

// WithLevel allow to set the logrus level
func WithLevel(l logrus.Level) LogOption {
	return func(ops *Options) {
		ops.Level = l
	}
}

// WithBlackList allow to apply a list of fields we don't want to send to graylog
func WithBlackList(blacklist []string) LogOption {
	return func(ops *Options) {
		for _, item := range blacklist {
			ops.Blacklist[item] = true
		}
	}
}

// WithBufSize allow to change the default buffer size, the size of buffer channel
func WithBufSize(bufSize uint) LogOption {
	return func(ops *Options) {
		ops.BufSize = bufSize
	}
}

// WithCompressType allow to change
func WithCompressType(ct CompressType) LogOption {
	return func(ops *Options) {
		ops.CompressType = ct
	}
}

// WithCompressLevel allow to change the default compress level
func WithCompressLevel(l int) LogOption {
	return func(ops *Options) {
		ops.CompressLevel = l
	}
}

// WithTimeout allow to change the default timeout
func WithTimeout(timeout int) LogOption {
	return func(ops *Options) {
		ops.Timeout = timeout
	}
}
