package graylog

import (
	"encoding/json"
	"errors"

	pkgerrors "github.com/pkg/errors"
)

// newMarshalableError builds an error which encodes its error message into JSON
func newMarshalableError(err error) *marshalableError {
	return &marshalableError{err}
}

// a marshalableError is an error that can be encoded into JSON
type marshalableError struct {
	err error
}

// MarshalJSON implements json.Marshaler for marshalableError
func (m *marshalableError) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.err.Error())
}

type causer interface {
	Cause() error
}

type stackTracer interface {
	StackTrace() pkgerrors.StackTrace
}

func extractStackTrace(err error) pkgerrors.StackTrace {
	var tracer stackTracer
	for {
		var st stackTracer
		if errors.As(err, &st) {
			tracer = st
		}

		var cause causer
		if errors.As(err, &cause) {
			err = cause.Cause()
			continue
		}
		break
	}
	if tracer == nil {
		return nil
	}
	return tracer.StackTrace()
}
