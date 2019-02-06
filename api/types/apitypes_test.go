package types

import (
	"testing"
)

func TestScriptClass_String(t *testing.T) {
	numClasses := len(scriptClassToName)

	tests := []struct {
		testName string
		sc       ScriptClass
		want     string
	}{
		{"ScriptClassPubKey", ScriptClassPubKey, scriptClassToName[ScriptClassPubKey]},
		{"ScriptClassNonStandard", ScriptClassNonStandard, scriptClassToName[ScriptClassNonStandard]},
		{"ScriptClassBADBAD", ScriptClassNonStandard + ScriptClass(numClasses+1), ScriptClassInvalid.String()},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			if got := tt.sc.String(); got != tt.want {
				t.Errorf("%s.String() = %v, want %v", tt.testName, got, tt.want)
			}
		})
	}
}

func TestScriptClassFromName(t *testing.T) {
	tests := []struct {
		testName string
		sc       string
		want     ScriptClass
	}{
		{"badName", "monkey", ScriptClassInvalid},
		{"invalid", "invalid", ScriptClassInvalid},
		{"OK", "pubkey", ScriptClassPubKey},
		{"OK#2", "nulldata", ScriptClassNullData},
		{"nonstandard is not invalid", "nonstandard", ScriptClassNonStandard},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			if got := ScriptClassFromName(tt.sc); got != tt.want {
				t.Errorf("ScriptClassFromName(%s) = %v, want %v", tt.sc, got, tt.want)
			}
		})
	}
}

func TestIsValidScriptClass(t *testing.T) {
	tests := []struct {
		testName string
		sc       string
		want     bool
	}{
		{"badName", "monkey", false},
		{"invalid is invalid", "invalid", false},
		{"OK", "pubkey", true},
		{"nonstandard is not invalid", "nonstandard", true},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			if got := IsValidScriptClass(tt.sc); got != tt.want {
				t.Errorf("ScriptClassFromName(%s) = %v, want %v", tt.sc, got, tt.want)
			}
		})
	}
}

func TestIsNullDataScript(t *testing.T) {
	tests := []struct {
		testName string
		name     string
		want     bool
	}{
		{"it is", "nulldata", true},
		{"it is not", "blahdata", false},
		{"it had better be", ScriptClassNullData.String(), true},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			if got := IsNullDataScript(tt.name); got != tt.want {
				t.Errorf("IsNullDataScript() = %v, want %v", got, tt.want)
			}
		})
	}
}
