package testutil

import (
	"fmt"
	"reflect"
)

func Log(tag string, message ...interface{}) {
	if len(message) > 0 {
		log(tag, message[0])
	} else {
		log(tag, nil)
	}
}

func log(tag string, a interface{}) {
	if a == nil {
		fmt.Println(tag)
		return
	}

	// a!=nil
	array := reflect.ValueOf(a)
	if array.Kind() == reflect.Array || array.Kind() == reflect.Slice {
		fmt.Println(ArrayToString(tag, a))
	} else {
		fmt.Println(tag+" >", a)
	}
}
