package graylog

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"sync"
)

// CompressType What compression type the writer should use when sending messages
// to the graylog2 server
type CompressType int

const (
	// CompressGzip Gzpi
	CompressGzip CompressType = iota
	// CompressZlib Zlib
	CompressZlib
	// NoCompress nothings
	NoCompress
)

// CompressionPool provide the compression writer as need
type CompressionPool interface {
	Get() interface{}
	Put(c interface{})
}

type compressionPool struct {
	compressType     CompressType
	compressionLevel int // one of the consts from compress/flate
	p                *sync.Pool
}

func newCompressionPool(t CompressType, level int) (CompressionPool, error) {
	switch t {
	case CompressGzip, CompressZlib, NoCompress:
	default:
		return nil, fmt.Errorf("unknown compression type %d", t)
	}
	return &compressionPool{
		compressType:     t,
		compressionLevel: level,
		p: &sync.Pool{
			New: func() interface{} {
				var zBuf bytes.Buffer
				var zw io.WriteCloser
				var err error
				switch t {
				case CompressGzip:
					zw, err = gzip.NewWriterLevel(&zBuf, level)
					if nil != err {
						fmt.Printf("Error:fail to create gzip writer,err:%s\n", err)
						return nil
					}
				case CompressZlib:
					zw, err = zlib.NewWriterLevel(&zBuf, level)
					if nil != err {
						fmt.Printf("Error:fail to create zlib writer,err:%s\n", err)
						return nil
					}
				case NoCompress:
					zw = bufferedWriter{buffer: &zBuf}
				}
				return zw
			},
		},
	}, nil
}
func (cp *compressionPool) Get() interface{} {
	return nil
}
func (cp *compressionPool) Put(c interface{}) {
	cp.p.Put(c)
}
