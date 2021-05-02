// Package dbconfig defines the parameters and methods for the PostgreSQL tests.
package dbconfig

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
)

// Migrations enables a custom migration runner to be used to load data from a
// given *.sql file dump into the migrations runner method.
type Migrations interface {
	// Path returns the filepath to the *.sql dump file.
	Path() string
	// Runner executes the scanned individual queries one after another.
	Runner(query string) error
}

// CustomScanner enables a given *.sql file dump to be scanned and loaded
// into the respective query runner using the Runner method.
func CustomScanner(m Migrations) error {
	file, err := os.Open(m.Path())
	if err != nil {
		return err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(scanQueries)

	for scanner.Scan() {
		text := scanner.Text()
		if len(text) == 0 {
			continue
		}
		if err = m.Runner(text); err != nil {
			return fmt.Errorf("Runner(%s): %v", text, err)
		}
	}
	return scanner.Err()
}

// scanQueries scans individual token separated by semi-colons since the queries
// are separated by semi-colons.
func scanQueries(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, bufio.ErrFinalToken
	}
	if i := bytes.Index(data, []byte(";\n")); i >= 0 {
		return i + 1, deleteComments(data[:i]), nil
	}
	if atEOF {
		return len(data), deleteComments(data), nil
	}
	return 0, nil, nil
}

// Comments start with (--) and end with a new line.
func deleteComments(a []byte) []byte {
	// delete sql comment
	re := regexp.MustCompile(`--[^\n]*`)
	a = re.ReplaceAll(a, []byte{})

	// Remove duplicate tabs and space characters.
	re = regexp.MustCompile(`[\t\p{Zs}]{2,}`)
	a = re.ReplaceAll(a, []byte{})

	// delete "test_" from all table name references.
	re = regexp.MustCompile(`test_`)
	a = re.ReplaceAll(a, []byte{})

	// delete any leading or trailing spaces left.
	return bytes.TrimSpace(a)
}
