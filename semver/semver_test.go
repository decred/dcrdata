package semver

import (
	"testing"
)

func TestString(t *testing.T) {
	ver := NewSemver(3, 2, 8)
	if ver.String() != "3.2.8" {
		t.Errorf("Incorrect semver formatting: %v", ver)
	}
}

func TestCompatible(t *testing.T) {
	required := NewSemver(3, 2, 8)

	// Equal is compatible
	testver := NewSemver(3, 2, 8)
	if !Compatible(required, testver) {
		t.Errorf("Versions %v and %v should be compatible.", required, testver)
	}

	// Higher patch is compatible
	testver = NewSemver(3, 2, 9)
	if !Compatible(required, testver) {
		t.Errorf("Versions %v and %v should be compatible.", required, testver)
	}

	// Lower patch is incompatible
	testver = NewSemver(3, 2, 7)
	if Compatible(required, testver) {
		t.Errorf("Versions %v and %v should be incompatible.", required, testver)
	}

	// Higher minor is compatible
	testver = NewSemver(3, 3, 0)
	if !Compatible(required, testver) {
		t.Errorf("Versions %v and %v should be compatible.", required, testver)
	}

	// Lower minor is incompatible
	testver = NewSemver(3, 1, 0)
	if Compatible(required, testver) {
		t.Errorf("Versions %v and %v should be incompatible.", required, testver)
	}
}

func TestAnyCompatible(t *testing.T) {
	compatibleVersions := []Semver{
		NewSemver(3, 2, 0),
		NewSemver(4, 0, 0),
	}

	// One equal is compatible
	testver := NewSemver(3, 2, 0)
	if !AnyCompatible(compatibleVersions, testver) {
		t.Errorf("Versions %v and one of %v should be compatible.",
			testver, compatibleVersions)
	}

	// Other equal is compatible
	testver = NewSemver(4, 0, 0)
	if !AnyCompatible(compatibleVersions, testver) {
		t.Errorf("Versions %v and one of %v should be compatible.",
			testver, compatibleVersions)
	}

	// Neither compatible is incompatible
	testver = NewSemver(3, 1, 0)
	if AnyCompatible(compatibleVersions, testver) {
		t.Errorf("Versions %v and one of %v should not be compatible.",
			testver, compatibleVersions)
	}
}
