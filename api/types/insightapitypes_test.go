package types

import (
	"fmt"
	"testing"
)

func TestHexBytes_UnmarshalJSON(t *testing.T) {
	in := []byte(`"deadbeef"`)
	var hb HexBytes
	err := hb.UnmarshalJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%x\n", []byte(hb))
	out, _ := hb.MarshalJSON()
	fmt.Println(string(out))
}
