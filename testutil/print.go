package testutil

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/davecgh/go-spew/spew"
)

// ArrayToString converts any array into pretty-print string
// unless UseSpewToPrettyPrintArrays flag signals to use the spew library
//
// Format:
/*
---(%tag)[%arraySize]---
      (0) %valueAtIndex0
      (1) %valueAtIndex1
      (2) %valueAtIndex2
      (3) %valueAtIndex3
      ...

Example call: ArrayToString("array",[5]int{14234, 42, -1, 1000, 5})
Output:
		---(array)[5]---
		       (0) 14234
		       (1) 42
		       (2) -1
		       (3) 1000
		       (4) 5
*/
func ArrayToString(tag string, iface interface{}) string {
	if UseSpewToPrettyPrintArrays {
		return "<" + tag + ">:\n" + spew.Sdump(iface)
	}

	array := reflect.ValueOf(iface)
	//if array.Kind() != reflect.Array {
	//	panic("This is not array: " + fmt.Sprint(iface))
	//}
	n := array.Len()
	prefix := "---(" + tag + ")"
	head := prefix + "-[" + strconv.Itoa(n) + "]---"
	prefixLen := len(prefix) + 1
	result := head + "\n"
	for i := 0; i < n; i++ {
		e := array.Index(i)
		val := fmt.Sprintf("%v", e)
		line := fmt.Sprintf("%"+strconv.Itoa(prefixLen)+"s",
			"("+strconv.Itoa(i)+") ") + val
		result = result + line + "\n"
	}
	return result
}
