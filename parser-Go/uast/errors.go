package uast

import "fmt"

// ParseError is a file-level (recoverable) error produced while building the
// UAST: the offending file is recorded and the build continues with the
// remaining files. Request-level failures (bad arguments, unreadable roots) are
// returned as a plain error by the api package instead.
type ParseError struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

func (e ParseError) Error() string {
	if e.File == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

// recordError appends a file-level error attributed to the file currently being
// built. It never terminates the build.
func (u *Builder) recordError(message string) {
	u.errs = append(u.errs, ParseError{File: u.currentFile, Message: message})
}

// Errors returns a copy of the file-level errors collected during Build.
func (u *Builder) Errors() []ParseError {
	out := make([]ParseError, len(u.errs))
	copy(out, u.errs)
	return out
}
