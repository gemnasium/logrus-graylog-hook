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
	Get(buf *bytes.Buffer) io.WriteCloser
	Put(c io.WriteCloser)
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
				}
				return zw
			},
		},
	}, nil
}
func (cp *compressionPool) Get(buf *bytes.Buffer) io.WriteCloser {
	switch cp.compressType {
	case CompressGzip:
		gzipw := cp.p.Get().(*gzip.Writer)
		gzipw.Reset(buf)
		return gzipw
	case CompressZlib:
		zlibw := cp.p.Get().(*zlib.Writer)
		zlibw.Reset(buf)
		return zlibw
	case NoCompress:
		return &bufferedWriter{buffer: buf}
	default:
		return nil
	}

}
func (cp *compressionPool) Put(c io.WriteCloser) {
	switch cp.compressType {
	case CompressGzip, CompressZlib:
		cp.p.Put(c)
	}
}
