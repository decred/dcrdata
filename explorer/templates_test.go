package explorer

import (
	"testing"
)

func TestPrefixPath(t *testing.T) {
	funcs := makeTemplateFuncMap(nil)

	prefixPath, ok := funcs["prefixPath"]
	if !ok {
		t.Fatalf(`Template function map does not contain "prefixPath".`)
	}

	prefixPathFn, ok := prefixPath.(func(prefix, path string) string)
	if !ok {
		t.Fatalf(`Template function "prefixPath" is not of type "func(prefix, path string) string".`)
	}

	testData := []struct {
		prefix string
		path   string
		out    string
	}{
		{"", "", ""},
		{"", "/", "/"},
		{"/", "", "/"},
		{"/", "/path", "/path"},
		{"/", "path", "/path"},
		{"//", "//", "/"},
		{"//", "/", "/"},
		{"/", "//", "/"},
		{"/", "/", "/"},
		{"", "/path", "/path"},
		{"stuff", "/", "stuff/"},
		{"stuff", "", "stuff"},
		{"/things", "", "/things"},
		{"/insight", "/api/status", "/insight/api/status"},
		{"/insight", "api/status", "/insight/api/status"},
		{"/insight/", "api/status", "/insight/api/status"},
		{"/insight/", "/api/status", "/insight/api/status"},
		{"insight", "api/status", "insight/api/status"},
	}

	for i := range testData {
		actual := prefixPathFn(testData[i].prefix, testData[i].path)
		if actual != testData[i].out {
			t.Errorf(`prefixPathFn("%s", "%s") returned "%s", expected "%s"`,
				testData[i].prefix, testData[i].path, actual, testData[i].out)
		}
	}
}

func TestHashStart(t *testing.T) {
	funcs := makeTemplateFuncMap(nil)

	hashStart, ok := funcs["hashStart"]
	if !ok {
		t.Fatalf(`Template function map does not contain "hashStart".`)
	}

	hashStartFn, ok := hashStart.(func(hash string) string)
	if !ok {
		t.Fatalf(`Template function "hashStart" is not of type "func(hash string) string".`)
	}

	testData := []struct {
		in  string
		out string
	}{
		{"2769040088594af8581b2f5c", "2769040088594af858"},
		{"948004e47d1578365b7b2b7b57512360b688e35ecd04f2e709f967000efdd6e9", "948004e47d1578365b7b2b7b57512360b688e35ecd04f2e709f967000e"},
		{"1234567", "1"},
		{"123456", ""},
		{"12345", ""},
		{"", ""},
	}

	for i := range testData {
		actual := hashStartFn(testData[i].in)
		if actual != testData[i].out {
			t.Errorf(`hashStart("%s") returned "%s", expected "%s"`,
				testData[i].in, actual, testData[i].out)
		}
	}
}

func TestHashEnd(t *testing.T) {
	funcs := makeTemplateFuncMap(nil)

	hashEnd, ok := funcs["hashEnd"]
	if !ok {
		t.Fatalf(`Template function map does not contain "hashEnd".`)
	}

	hashEndFn, ok := hashEnd.(func(hash string) string)
	if !ok {
		t.Fatalf(`Template function "hashEnd" is not of type "func(hash string) string".`)
	}

	testData := []struct {
		in  string
		out string
	}{
		{"2769040088594af8581b2f5c", "1b2f5c"},
		{"948004e47d1578365b7b2b7b57512360b688e35ecd04f2e709f967000efdd6e9", "fdd6e9"},
		{"1234567", "234567"},
		{"123456", "123456"},
		{"12345", "12345"},
		{"", ""},
	}

	for i := range testData {
		actual := hashEndFn(testData[i].in)
		if actual != testData[i].out {
			t.Errorf(`hashEnd("%s") returned "%s", expected "%s"`,
				testData[i].in, actual, testData[i].out)
		}
	}
}

func TestHashStartEnd(t *testing.T) {
	funcs := makeTemplateFuncMap(nil)

	hashStart, ok := funcs["hashStart"]
	if !ok {
		t.Fatalf(`Template function map does not contain "hashStart".`)
	}

	hashStartFn, ok := hashStart.(func(hash string) string)
	if !ok {
		t.Fatalf(`Template function "hashStart" is not of type "func(hash string) string".`)
	}

	hashEnd, ok := funcs["hashEnd"]
	if !ok {
		t.Fatalf(`Template function map does not contain "hashEnd".`)
	}

	hashEndFn, ok := hashEnd.(func(hash string) string)
	if !ok {
		t.Fatalf(`Template function "hashEnd" is not of type "func(hash string) string".`)
	}

	testData := []struct {
		in         string
		out1, out2 string
	}{
		{"2769040088594af8581b2f5c", "2769040088594af858", "1b2f5c"},
		{"948004e47d1578365b7b2b7b57512360b688e35ecd04f2e709f967000efdd6e9", "948004e47d1578365b7b2b7b57512360b688e35ecd04f2e709f967000e", "fdd6e9"},
		{"123456789ab", "12345", "6789ab"},
		{"123456789abc", "123456", "789abc"},
		{"123456789abcd", "1234567", "89abcd"},
		{"1234567", "1", "234567"},
		{"123456", "", "123456"},
		{"12345", "", "12345"},
		{"1", "", "1"},
		{"", "", ""},
	}

	for i := range testData {
		actualStart := hashStartFn(testData[i].in)
		actualEnd := hashEndFn(testData[i].in)
		if actualStart+actualEnd != testData[i].in {
			t.Errorf(`hashStart+hashEnd("%s") returned "%s" ("%s"+"%s"), expected "%s"`,
				testData[i].in, actualStart+actualEnd, actualStart, actualEnd, testData[i].in)
		}
		if actualStart != testData[i].out1 {
			t.Errorf(`hashStart("%s") returned "%s", expected "%s"`,
				testData[i].in, actualStart, testData[i].out1)
		}
		if actualEnd != testData[i].out2 {
			t.Errorf(`hashEnd("%s") returned "%s", expected "%s"`,
				testData[i].in, actualEnd, testData[i].out2)
		}
	}
}

func TestAmountAsDecimalPartsTrimmed(t *testing.T) {

	in := []struct {
		amt    int64
		n      int64
		commas bool
	}{
		{314159000, 2, false},
		{76543210000, 2, false},
		{766432100000, 2, true},
		{654321000, 1, false},
		{987654321, 8, false},
		{987654321, 2, false},
		{90765432100, 2, false},
		{9076543200000, 2, false},
		{907654320, 7, false},
		{1234590700, 2, false},
		{100000000, 2, false},
		{314159000, 2, false},
		{14159000, 2, false},
		{314159000, 7, true},
		{300000000, 7, true},
		{301000000, 1, true},
		{300000000, 0, true},
		{987654321, 11, false},
		{987654321237, 11, false},
	}

	expected := []struct {
		whole, frac, tail string
	}{
		{"3", "14", ""},
		{"765", "43", ""},
		{"7,664", "32", ""},
		{"6", "5", ""},
		{"9", "87654321", ""},
		{"9", "87", ""},
		{"907", "65", ""},
		{"90765", "43", ""},
		{"9", "0765432", ""},
		{"12", "34", ""},
		{"1", "", "00"},
		{"3", "14", ""},
		{"0", "14", ""},
		{"3", "14159", "00"},
		{"3", "", "0000000"},
		{"3", "", "0"},
		{"3", "", ""},
		{"9", "87654321", ""},
		{"9876", "54321237", ""},
	}

	for i := range in {
		out := amountAsDecimalPartsTrimmed(in[i].amt, in[i].n, in[i].commas)
		if out[0] != expected[i].whole || out[1] != expected[i].frac ||
			out[2] != expected[i].tail {
			t.Errorf("amountAsDecimalPartsTrimmed failed for "+
				"%d (%d decimals, commas=%v). Got %s.%s%s, expected %s.%s%s.",
				in[i].amt, in[i].n, in[i].commas,
				out[0], out[1], out[2],
				expected[i].whole, expected[i].frac, expected[i].tail)
		}

	}

}
