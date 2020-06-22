package atmodem

import (
	"errors"
	"strconv"
	"strings"
)

// A valueParser parses well-typed values from a string slice input.
//
// After each parsing operation, the caller must invoke the Err method to
// determine if any input could not be parsed as the specified type.
type valueParser struct {
	ss  []string
	err error
}

// newValueParser constructs a valueParser from the input string slice. The string
// slice must contain at least one element or it will return an error.
func newValueParser(ss []string) (*valueParser, error) {
	if len(ss) == 0 {
		return nil, errors.New("atmodem: no key/value pair values provided for parsing")
	}

	// Remove any leading or trailing whitespace for parsing convenience.
	for i := range ss {
		ss[i] = strings.TrimSpace(ss[i])
	}

	return &valueParser{ss: ss}, nil
}

// Err returns the current parsing error, if there is one.
func (vp *valueParser) Err() error { return vp.err }

// Int parses the input as an integer.
func (vp *valueParser) Int() int {
	if vp.err != nil {
		return 0
	}

	// This access is safe due to the constructor bounds check.
	// TODO: parameterize the index?
	v, err := strconv.Atoi(vp.ss[0])
	if err != nil {
		vp.err = err
		return 0
	}

	return v
}

// String parses the input as a string with each slice value joined by spaces.
func (vp *valueParser) String() string {
	if vp.err != nil {
		return ""
	}

	return strings.Join(vp.ss, " ")
}
