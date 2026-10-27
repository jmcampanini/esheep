// Package apperr classifies application failures separately from usage errors.
package apperr

// Error marks an application failure.
type Error struct {
	err error
}

// Error returns the underlying diagnostic.
func (e Error) Error() string { return e.err.Error() }

// Unwrap returns the underlying failure.
func (e Error) Unwrap() error { return e.err }

// Wrap marks a non-nil error as an application failure.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	return Error{err: err}
}
