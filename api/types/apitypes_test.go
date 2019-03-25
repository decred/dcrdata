package types

import (
	"reflect"
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

// TestTimeAPI_MarshalJSON ensures that (*TimeAPI).MarshalJSON returns a UNIX
// time stamp as a JSON integer, not a quoted string.
func TestTimeAPI_MarshalJSON(t *testing.T) {
	tests := []struct {
		testName string
		timeAPI  TimeAPI
		want     []byte
		wantErr  bool
	}{
		{
			testName: "ok",
			timeAPI:  NewTimeAPIFromUNIX(1454954400),
			want:     []byte(`1454954400`),
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			got, err := tt.timeAPI.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("TimeAPI.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TimeAPI.MarshalJSON() = %v, want %v", string(got), string(tt.want))
			}
		})
	}

}

// TestTimeAPI_UnmarshalJSON ensures that (*TimeAPI).UnmarshalJSON works with a
// JSON integer value, not a quoted string.
func TestTimeAPI_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		testName string
		data     []byte
		wantErr  bool
	}{
		{
			testName: "ok",
			data:     []byte(`1454954400`), // Correct text has no quotes
			wantErr:  false,
		},
		{
			testName: "bad",
			data:     []byte(`"1454954400"`), // Text with quotes is a JSON string, not an integer
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			var td TimeAPI
			if err := td.UnmarshalJSON(tt.data); (err != nil) != tt.wantErr {
				t.Errorf("TimeAPI.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			t.Logf("UNIX: %d, formatted: %s", td.UNIX(), td)
		})
	}
}

// TestTimeAPI_MarshalUnmarshalJSON ensures a round trip marshal-unmarshal is
// successful.
func TestTimeAPI_MarshalUnmarshalJSON(t *testing.T) {
	tests := []struct {
		testName string
		timeAPI  TimeAPI
		want     []byte
		wantErr  bool
	}{
		{
			testName: "ok",
			timeAPI:  NewTimeAPIFromUNIX(1454954400),
			want:     []byte(`1454954400`),
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			// Marshal TimeAPI.
			got, err := tt.timeAPI.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("TimeAPI.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TimeAPI.MarshalJSON() = %v, want %v", string(got), string(tt.want))
			}

			// Unmarshal the result.
			var td TimeAPI
			if err = td.UnmarshalJSON(got); err != nil {
				t.Errorf("TimeAPI.UnmarshalJSON() failed: %v", err)
			}
			// Ensure the round trip was successful.
			if !reflect.DeepEqual(td, tt.timeAPI) {
				t.Errorf("TimeAPI.UnmarshalJSON() = %v, want %v", td, tt.timeAPI)
			}
		})
	}

}
