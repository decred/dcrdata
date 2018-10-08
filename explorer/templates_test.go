package explorer

import (
	"testing"
)

func TestamountAsDecimalPartsTrimmed(t *testing.T) {
	test1 := amountAsDecimalPartsTrimmed(314159000, 2, false)
	test2 := amountAsDecimalPartsTrimmed(76543210000, 2, false)
	test3 := amountAsDecimalPartsTrimmed(76643210000, 2, false)
	test4 := amountAsDecimalPartsTrimmed(654321000, 1, false)
	test5 := amountAsDecimalPartsTrimmed(987654321, 8, false)
	test6 := amountAsDecimalPartsTrimmed(987654321, 2, false)
	test7 := amountAsDecimalPartsTrimmed(90765432100, 2, false)
	test8 := amountAsDecimalPartsTrimmed(9076543200000, 2, false)
	test9 := amountAsDecimalPartsTrimmed(907654320, 7, false)
	test10 := amountAsDecimalPartsTrimmed(1234590700, 2, false)

	if test1[0]+"."+test1[1] != "3.14" {
		t.Error("Wrong output, got" + test1[0] + "." + test1[1])
	}

	if test2[0]+"."+test2[1] != "765.43" {
		t.Error("Wrong output, got" + test2[0] + "." + test2[1])
	}

	if test3[0]+"."+test3[1] != "766.43" {
		t.Error("Wrong output, got" + test3[0] + "." + test3[1])
	}

	if test4[0]+"."+test4[1] != "6.5" {
		t.Error("Wrong output, got" + test4[0] + "." + test4[1])
	}

	if test5[0]+"."+test5[1] != "9.87654321" {
		t.Error("Wrong output, got" + test5[0] + "." + test5[1])
	}

	if test6[0]+"."+test6[1] != "9.87" {
		t.Error("Wrong output, got" + test6[0] + "." + test6[1])
	}

	if test7[0]+"."+test7[1] != "907.65" {
		t.Error("Wrong output, got" + test7[0] + "." + test7[1])
	}

	if test8[0]+"."+test8[1] != "90765.43" {
		t.Error("Wrong output, got" + test8[0] + "." + test8[1])
	}

	if test9[0]+"."+test9[1] != "9.0765432" {
		t.Error("Wrong output, got" + test9[0] + "." + test9[1])
	}

	if test10[0]+"."+test10[1] != "12.34" {
		t.Error("Wrong output, got" + test10[0] + "." + test10[1])
	}

}
