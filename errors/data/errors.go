package data

type Error interface {
	error
	DataError() Error
}

type wrappingError struct {
	err error
}

func (e wrappingError) Error() string    { return e.err.Error() }
func (e wrappingError) DataError() Error { return &e }

var _ Error = &wrappingError{}

func Err(err error) error { return &wrappingError{err} }

type msgError struct {
	msg string
}

func (e msgError) Error() string    { return e.msg }
func (e msgError) DataError() Error { return &e }

var _ Error = &msgError{}

type malformedError struct {
	err error
}

func (e malformedError) DataError() Error { return &e }

var _ Error = &malformedError{}

func (e malformedError) Error() string {
	if e.err != nil {
		return "malformed: " + e.err.Error()
	}
	return "malformed"
}

func Malformed(err error) error {
	return &malformedError{err}
}

var (
	ErrEmpty           = &msgError{"empty"}
	ErrMalformed       = Malformed(nil)
	ErrTypeUnsupported = &msgError{"type unsupported"}
)
