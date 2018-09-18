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
	testCases := []CharsetData{
		CharsetData{
			HexStr:      "54e20100000000000000000000000000000000000000000000000000a810b3113fbc16a1",
			contentType: "application/octet-stream",
		},
		CharsetData{
			HexStr:      "ec2ae640fd87a785698e33552f48321503306287ddbddb6668baeca671b38d03546f7567682063686f69636520686572652e2e2e2073616c74792074656172732061726520736f6f6f6f6f2073776565742074686f2e",
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			language:    "en",
			confidence:  48,
		},
		CharsetData{
			HexStr:      "48656c6c6f2c204465637265642e",
			contentType: "text/plain; charset=utf-8",
			confidence:  46,
			charset:     "ISO-8859-1",
			language:    "it",
			Data:        []byte(`Hello, Decred`),
		},
		CharsetData{
			contentType: "text/plain; charset=utf-8",
			confidence:  100,
			charset:     "UTF-8",
			language:    "en",
			Data:        []byte(`中国傻子也多。`),
			HexStr:      "e4b8ade59bbde582bbe5ad90e4b99fe5a49ae38082",
		},
		CharsetData{
			HexStr:      "050005000000",
			contentType: "application/octet-stream",
			confidence:  80,
			charset:     "UTF-32LE",
		},
		CharsetData{
			HexStr:      "04772c3b3fc59a9271d0e2b5a2fb727f441b37047dccb8860000000000000000972e0400",
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			language:    "pl",
			confidence:  17,
		},
		CharsetData{
			HexStr:      "69a802000000000000000000000000000000000000000000000000003e101d08fc9c1fba",
			contentType: "application/octet-stream",
			charset:     "Shift_JIS",
			language:    "ja",
			confidence:  10,
		},
		CharsetData{
			HexStr:      "636861726c6579206c6f766573206865696469",
			contentType: "text/plain; charset=utf-8",
			confidence:  30,
			charset:     "UTF-8",
			language:    "en",
			Data:        []byte(`charley loves heidi`),
		},
		CharsetData{
			HexStr:      "dbc61e8e32e176ce50bfa4a30be03bf8b2bd44978de5d9ac00000000000000008d280400",
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			language:    "fr",
			confidence:  17,
		},
		CharsetData{
			HexStr:      "e6b19fe58d93e5b094e38081e590b4e5bf8ce5af92e38081e69d8ee7ac91e69da520e698afe4b889e4b8aae4b8ade59bbde9aa97e5ad90",
			contentType: "text/plain; charset=utf-8",
			charset:     "UTF-8",
			confidence:  100,
			Data:        []byte(`江卓尔、吴忌寒、李笑来 是三个中国骗子 `),
		},
	}

	for _, testData := range testCases {
		result, err := DetectCharset(testData.HexStr)
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
