// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package semver

import (
	"fmt"
	"regexp"
	"strconv"
)

// NewSemver returns a new Semver with the version major.minor.patch
func NewSemver(major, minor, patch uint32) Semver {
	return Semver{major, minor, patch}
}

// Semver models a semantic version (semver) major.minor.patch
type Semver struct {
	major, minor, patch uint32
}

// Compatible decides if the actual version is compatible with the required one.
func Compatible(required, actual Semver) bool {
	switch {
	case required.major != actual.major:
		return false
	case required.minor > actual.minor:
		return false
	case required.minor == actual.minor && required.patch > actual.patch:
		return false
	default:
		return true
	}
}

// AnyCompatible checks if the version is compatible with any versions in a
// slice of versions.
func AnyCompatible(compatible []Semver, actual Semver) (isApiCompat bool) {
	for _, v := range compatible {
		if Compatible(v, actual) {
			isApiCompat = true
			break
		}
	}
	return
}

func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}

// Split returns the major, minor and patch version.
func (s *Semver) Split() (uint32, uint32, uint32) {
	return s.major, s.minor, s.patch
}

// ParseVersionStr makes a *Semver from a version string (e.g. v3.1.0, 5.3.2,
// 7.3, etc.).  The "v" prefix is optional, as are the minor and patch versions.
func ParseVersionStr(ver string) (*Semver, error) {
	var v, m, p int
	var err error

	// If this matches a string, there will be 3 submatches in addition to the
	// matched string in the result of FindStringSubmatch. Otherwise, there will
	// result will be an empty slice.
	re := regexp.MustCompile(`^v?(\d+)\.?(\d*)\.?(\d*)$`)
	subs := re.FindStringSubmatch(ver)
	if len(subs) != 4 {
		return nil, fmt.Errorf("invalid version string")
	}

	// Matched the string and captured 3 substrings. Parse each substring, some
	// of which may be empty. Empty substrings are treated as a 0.

	// patch
	if len(subs[3]) > 0 {
		p, err = strconv.Atoi(subs[3])
		if err != nil {
			return nil, err
		}
	}

	// minor
	if len(subs[2]) > 0 {
		m, err = strconv.Atoi(subs[2])
		if err != nil {
			return nil, err
		}
	}

	// major
	if len(subs[1]) > 0 {
		v, err = strconv.Atoi(subs[1])
		if err != nil {
			return nil, err
		}
	}

	s := NewSemver(uint32(v), uint32(m), uint32(p))
	return &s, nil
}
