package dbtypes

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDetectCharset tests the functionality of DetectCharset function and
// Decode method.
func TestDetectCharset(t *testing.T) {
	// testCases defines the input hexString as the Key and the value as the
	// expected output.
	testCases := map[int]CharsetData{
		1: CharsetData{
			HexStr:      "54e20100000000000000000000000000000000000000000000000000a810b3113fbc16a1",
			contentType: "application/octet-stream",
			Data: []byte{84, 226, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 168, 16, 179, 17, 63, 188, 22, 161},
		},
		2: CharsetData{
			HexStr:      "42656172206d61726b65742061696e2774206f7665722079657420736f20676f6f64206c75636b2077697468207468617421",
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			language:    "en",
			confidence:  48,
			Data:        []byte(`Bear market ain't over yet so good luck with that!`),
		},
		3: CharsetData{
			HexStr:      "48656c6c6f20476f7068657221",
			contentType: "text/plain; charset=utf-8",
			confidence:  46,
			charset:     "ISO-8859-1",
			language:    "it",
			Data:        []byte(`Hello Gopher!`),
		},
		4: CharsetData{
			HexStr:      "48656c6c6f2c204465637265642e",
			contentType: "text/plain; charset=utf-8",
			confidence:  69,
			charset:     "ISO-8859-1",
			language:    "it",
			Data:        []byte(`Hello, Decred.`),
		},
		5: CharsetData{
			contentType: "text/plain; charset=utf-8",
			confidence:  100,
			charset:     "UTF-8",
			language:    "en",
			Data:        []byte(`中国傻子也多。`),
			HexStr:      "e4b8ade59bbde582bbe5ad90e4b99fe5a49ae38082",
		},
		6: CharsetData{
			HexStr:      "75ab161a5be9b414edd051b8b66eba94317ab2f897def7971b68d8000000000083790000",
			contentType: "application/octet-stream",
			confidence:  13,
			charset:     "windows-1252",
			Data: []byte{117, 171, 22, 26, 91, 233, 180, 20, 237, 208, 81, 184, 182, 110, 186, 148, 49, 122, 178, 248, 151,
				222, 247, 151, 27, 104, 216, 0, 0, 0, 0, 0, 131, 121, 0, 0},
		},
		7: CharsetData{
			HexStr:      "04772c3b3fc59a9271d0e2b5a2fb727f441b37047dccb8860000000000000000972e0400",
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			language:    "pl",
			confidence:  17,
			Data: []byte{4, 119, 44, 59, 63, 197, 154, 146, 113, 208, 226, 181, 162, 251, 114, 127, 68, 27,
				55, 4, 125, 204, 184, 134, 0, 0, 0, 0, 0, 0, 0, 0, 151, 46, 4, 0},
		},
		8: CharsetData{
			HexStr:      "69a802000000000000000000000000000000000000000000000000003e101d08fc9c1fba",
			contentType: "application/octet-stream",
			charset:     "Shift_JIS",
			language:    "ja",
			confidence:  10,
			Data: []byte{105, 168, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 62, 16, 29, 8, 252, 156, 31, 186},
		},
		9: CharsetData{
			HexStr:      "636861726c6579206c6f766573206865696469",
			contentType: "text/plain; charset=utf-8",
			confidence:  30,
			charset:     "UTF-8",
			language:    "en",
			Data:        []byte(`charley loves heidi`),
		},
		10: CharsetData{
			HexStr:      "dbc61e8e32e176ce50bfa4a30be03bf8b2bd44978de5d9ac00000000000000008d280400",
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			language:    "fr",
			confidence:  17,
			Data: []byte{219, 198, 30, 142, 50, 225, 118, 206, 80, 191, 164, 163, 11, 224, 59, 248, 178, 189, 68,
				151, 141, 229, 217, 172, 0, 0, 0, 0, 0, 0, 0, 0, 141, 40, 4, 0},
		},
		11: CharsetData{
			HexStr:      "e6b19fe58d93e5b094e38081e590b4e5bf8ce5af92e38081e69d8ee7ac91e69da520e698afe4b889e4b8aae4b8ade59bbde9aa97e5ad90",
			contentType: "text/plain; charset=utf-8",
			charset:     "UTF-8",
			confidence:  100,
			Data:        []byte(`江卓尔、吴忌寒、李笑来 是三个中国骗子`),
		},
		12: CharsetData{
			HexStr:      `ec2ae640fd87a785698e33552f48321503306287ddbddb6668baeca671b38d03546f7567682063686f69636520686572652e2e2e2073616c74792074656172732061726520736f6f6f6f6f2073776565742074686f2e`,
			contentType: "application/octet-stream",
			charset:     "windows-1252",
			confidence:  48,
			Data: []byte{236, 42, 230, 64, 253, 135, 167, 133, 105, 142, 51, 85, 47, 72, 50, 21, 3, 48, 98, 135,
				221, 189, 219, 102, 104, 186, 236, 166, 113, 179, 141, 3, 84, 111, 117, 103, 104, 32, 99, 104, 111, 105,
				99, 101, 32, 104, 101, 114, 101, 46, 46, 46, 32, 115, 97, 108, 116, 121, 32, 116, 101, 97, 114, 115, 32,
				97, 114, 101, 32, 115, 111, 111, 111, 111, 111, 32, 115, 119, 101, 101, 116, 32, 116, 104, 111, 46},
		},
	}

	// decodedText shows the final value returned as HexStr by the Decode method.
	// If decoding fails the original hex string value should be returned.
	decodedText := map[int]string{
		1:  "54e20100000000000000000000000000000000000000000000000000a810b3113fbc16a1",
		2:  "Bear market ain't over yet so good luck with that!",
		3:  "Hello Gopher!",
		4:  "Hello, Decred.",
		5:  "中国傻子也多。",
		6:  "75ab161a5be9b414edd051b8b66eba94317ab2f897def7971b68d8000000000083790000",
		7:  "04772c3b3fc59a9271d0e2b5a2fb727f441b37047dccb8860000000000000000972e0400",
		8:  "69a802000000000000000000000000000000000000000000000000003e101d08fc9c1fba",
		9:  "charley loves heidi",
		10: "dbc61e8e32e176ce50bfa4a30be03bf8b2bd44978de5d9ac00000000000000008d280400",
		11: "江卓尔、吴忌寒、李笑来 是三个中国骗子",
		12: string([]byte{195, 172, 42, 195, 166, 64, 195, 189, 226, 128, 161, 194, 167, 226, 128, 166, 105, 197, 189, 51, 85, 47, 72,
			50, 21, 3, 48, 98, 226, 128, 161, 195, 157, 194, 189, 195, 155, 102, 104, 194, 186, 195, 172, 194, 166, 113, 194, 179, 239, 191,
			189, 3, 84, 111, 117, 103, 104, 32, 99, 104, 111, 105, 99, 101, 32, 104, 101, 114, 101, 46, 46, 46, 32, 115, 97, 108, 116, 121, 32,
			116, 101, 97, 114, 115, 32, 97, 114, 101, 32, 115, 111, 111, 111, 111, 111, 32, 115, 119, 101, 101, 116, 32, 116, 104, 111, 46}),
	}

	for i, testData := range testCases {
		result, err := DetectCharset(testData.HexStr)
		if err != nil {
			t.Fatalf("expected no error back but was returned: %v", err)
		}

		rData, _ := json.Marshal(*result)
		tData, _ := json.Marshal(testData)
		tDataStr, rDataStr := string(tData), string(rData)
		if strings.Compare(tDataStr, rDataStr) != 0 {
			t.Fatalf("expected testData: %s to be equal to the returned result: %s but it wasn't\n", tDataStr, rDataStr)
		}

		v, err := result.Decode()
		if err != nil {
			t.Fatalf("expected no error back but was returned: %v", err)
		}

		expectedStr := decodedText[i]

		if strings.Compare(v.HexStr, expectedStr) != 0 {
			t.Fatalf("expected the decoded string to be: (%s) but was: (%s)", v.HexStr, expectedStr)
		}
	}
}
