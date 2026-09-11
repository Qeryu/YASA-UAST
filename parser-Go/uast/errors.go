package uast

import "fmt"

// Severity classifies a ParseError: an error blocks product emission for a
// single-file request, while a warning is reported but does not discard the
// (possibly partial) product.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Kind is a machine-readable reason for a ParseError. The set is shared by the
// builder (unsupported_node) and the api package (parse_error/read_error/
// no_gomod/no_packages).
type Kind string

const (
	KindParseError      Kind = "parse_error"
	KindReadError       Kind = "read_error"
	KindUnsupportedNode Kind = "unsupported_node"
	KindNoGoMod         Kind = "no_gomod"
	KindNoPackages      Kind = "no_packages"
)

// ParseError is a file-level (recoverable) error produced while building the
// UAST: the offending file is recorded and the build continues with the
// remaining files. Request-level failures (bad arguments, unreadable roots) are
// returned as a plain error by the api package instead.
type ParseError struct {
	File     string   `json:"file"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
	Kind     Kind     `json:"kind"`
}

func (e ParseError) Error() string {
	if e.File == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

// recordError appends a file-level entry attributed to the file currently being
// built. It never terminates the build.
func (u *Builder) recordError(severity Severity, kind Kind, message string) {
	u.errs = append(u.errs, ParseError{
		File:     u.currentFile,
		Message:  message,
		Severity: severity,
		Kind:     kind,
	})
}

// Errors returns a copy of the file-level errors collected during Build.
func (u *Builder) Errors() []ParseError {
	out := make([]ParseError, len(u.errs))
	copy(out, u.errs)
	return out
}
