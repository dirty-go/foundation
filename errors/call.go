package errors

type CallError interface {
	error
	CallError()
}

func IsCallError(e error) bool {
	v, ok := e.(CallError)
	return ok && v != nil
}
