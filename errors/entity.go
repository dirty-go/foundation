package errors

type EntityError interface {
	error
	EntityIdentifier() string
}

func Ent(entityIdentifier string, err error) EntityError {
	return &entityError{
		identifier: entityIdentifier,
		err:        err,
	}
}

func EntMsg(entityIdentifier string, errMsg string) EntityError {
	return &entityError{
		identifier: entityIdentifier,
		err:        Msg(errMsg),
	}
}

type entityError struct {
	identifier string
	err        error
}

var (
	_ error       = &entityError{}
	_ Unwrappable = &entityError{}
	_ EntityError = &entityError{}
)

func (e entityError) Error() string {
	var errMsg string
	if e.err != nil {
		errMsg = e.err.Error()
	}
	if e.identifier != "" {
		if errMsg != "" {
			return e.identifier + ": " + errMsg
		}
		return e.identifier + " invalid"
	}
	if errMsg != "" {
		return "entity " + errMsg
	}
	return "invalid entity"
}

func (e entityError) Unwrap() error { return &e }

func (e entityError) EntityIdentifier() string { return e.identifier }
