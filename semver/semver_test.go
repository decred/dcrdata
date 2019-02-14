package semver

import (
	"reflect"
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

func TestParseVersionStr(t *testing.T) {
	tests := []struct {
		testName string
		ver      string
		want     *Semver
		wantErr  bool
	}{
		{"v3", "v3", &Semver{3, 0, 0}, false},
		{"v3.0", "v3.0", &Semver{3, 0, 0}, false},
		{"v3.0.0", "v3.0.0", &Semver{3, 0, 0}, false},
		{"v3..0", "v3..0", &Semver{3, 0, 0}, false},
		{"v3.6", "v3.6", &Semver{3, 6, 0}, false},
		{"v3.6.0", "v3.6.0", &Semver{3, 6, 0}, false},
		{"v3.10.0", "v3.10.0", &Semver{3, 10, 0}, false},
		{"3", "3", &Semver{3, 0, 0}, false},
		{"3.", "3.", &Semver{3, 0, 0}, false},
		{"3.7.", "3.7.", &Semver{3, 7, 0}, false},
		{"3.0", "3.0", &Semver{3, 0, 0}, false},
		{"3.10.1", "3.10.1", &Semver{3, 10, 1}, false},
		{"invalid major", "vq.10.12", nil, true},
		{"invalid minor", "v3.x.12", nil, true},
		{"invalid patch", "v3.10.-12", nil, true},
		{"empty", "", nil, true},
		{"bare v", "v", nil, true},
		{"v10.10.10", "v10.10.10", &Semver{10, 10, 10}, false},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			got, err := ParseVersionStr(tt.ver)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersionStr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseVersionStr() = %v, want %v", got, tt.want)
			}
		})
	}
}
