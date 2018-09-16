package dbtypes

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestDetectCharset tests the functionality of DetectCharset function.
func TestDetectCharset(t *testing.T) {
	// testCases defines the input hexString as the Key and the value as the
	// expected output.
	testCases := map[string]CharsetType{
		"48656c6c6f2c204465637265642e": CharsetType{
			ContentType: "text/plain; charset=utf-8",
			Confidence:  46,
			Charset:     "ISO-8859-1",
			Language:    "it",
			Data:        []byte(`Hello, Decred`),
		},
		"e4b8ade59bbde582bbe5ad90e4b99fe5a49ae38082": CharsetType{
			ContentType: "text/plain; charset=utf-8",
			Confidence:  100,
			Charset:     "UTF-8",
			Language:    "en",
			Data:        []byte(`中国傻子也多。`),
		},
		"050005000000": CharsetType{
			ContentType: "application/octet-stream",
			Confidence:  80,
			Charset:     "UTF-32LE",
		},
		"04772c3b3fc59a9271d0e2b5a2fb727f441b37047dccb8860000000000000000972e0400": CharsetType{
			ContentType: "application/octet-stream",
			Charset:     "windows-1252",
			Language:    "pl",
			Confidence:  17,
		},
		"69a802000000000000000000000000000000000000000000000000003e101d08fc9c1fba": CharsetType{
			ContentType: "application/octet-stream",
			Charset:     "Shift_JIS",
			Language:    "ja",
			Confidence:  10,
		},
		"636861726c6579206c6f766573206865696469": CharsetType{
			ContentType: "text/plain; charset=utf-8",
			Confidence:  30,
			Charset:     "UTF-8",
			Language:    "en",
			Data:        []byte(`charley loves heidi`),
		},
		"dbc61e8e32e176ce50bfa4a30be03bf8b2bd44978de5d9ac00000000000000008d280400": CharsetType{
			ContentType: "application/octet-stream",
			Charset:     "windows-1252",
			Language:    "fr",
			Confidence:  17,
		},
		"e6b19fe58d93e5b094e38081e590b4e5bf8ce5af92e38081e69d8ee7ac91e69da520e698afe4b889e4b8aae4b8ade59bbde9aa97e5ad90": CharsetType{
			ContentType: "text/plain; charset=utf-8",
			Charset:     "UTF-8",
			Confidence:  100,
			Data:        []byte(`江卓尔、吴忌寒、李笑来 是三个中国骗子 `),
		},
	}

	for hexString, testData := range testCases {
		result, err := DetectCharset(hexString)
		if err != nil {
			t.Fatalf("expected no error back but was returned: %v", err)
		}

		if reflect.DeepEqual(*result, testData) {
			rData, _ := json.Marshal(*result)
			tData, _ := json.Marshal(testData)
			t.Fatalf("expected testData: %s to be equal to the returned result: %v but it wasn't", string(tData), string(rData))
		}
	}
}
