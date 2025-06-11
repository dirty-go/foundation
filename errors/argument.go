package errors

type ArgumentError interface {
	ArgumentName() string
}

func Arg(argName string, err error, fields ...EntityError) error {
	return &argumentError{entityError{
		identifier: argName,
		err:        err,
	}, fields}
}

func ArgMsg(argName, errMsg string, fields ...EntityError) error {
	return &argumentError{entityError{
		identifier: argName,
		err:        Msg(errMsg),
	}, fields}
}

func ArgWrap(argName, contextMessage string, err error, fields ...EntityError) error {
	return &argumentError{entityError{
		identifier: argName,
		err:        Wrap(contextMessage, err),
	}, fields}
}

type argumentError struct {
	entityError
	fields []EntityError
}

var (
	_ error         = &argumentError{}
	_ Unwrappable   = &argumentError{}
	_ CallError     = &argumentError{}
	_ EntityError   = &argumentError{}
	_ ArgumentError = &argumentError{}
)

func (e argumentError) ArgumentName() string {
	return e.entityError.identifier
}

func (argumentError) CallError() {}

func (e argumentError) Error() string {
	if e.identifier != "" {
		if errMsg := e.err.Error(); errMsg != "" {
			return "arg " + e.identifier + ": " + errMsg
		}
		return "arg " + e.identifier + " invalid"
	}
	if e.err != nil {
		if errMsg := e.err.Error(); errMsg != "" {
			return "arg " + errMsg
		}
	}
	return "invalid arg"
}
