package errors

import (
	"errors"
)

var (
	As     = errors.As
	Is     = errors.Is
	New    = errors.New
	Msg    = errors.New
	Unwrap = errors.Unwrap
)

type Unwrappable interface {
	error
	Unwrap() error
}

func Wrap(contextMessage string, causeErr error) error {
	return &errorWrap{contextMessage, causeErr}
}

var _ Unwrappable = &errorWrap{}

type errorWrap struct {
	msg string
	err error
}

func (e errorWrap) Error() string {
	if e.msg != "" {
		if e.err != nil {
			return e.msg + ": " + e.err.Error()
		}
	}
	return e.msg
}

func (e errorWrap) Unwrap() error {
	return e.err
}

var ErrUnimplemented = Msg("unimplemented")
