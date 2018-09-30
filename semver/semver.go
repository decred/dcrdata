// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package semver

import "fmt"

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

func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}
