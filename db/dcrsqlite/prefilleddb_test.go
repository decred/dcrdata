package dcrsqlite

import (
	"fmt"
	"testing"

	"github.com/decred/dcrdata/testutil"
)

func TestRetrieveBlockFeeInfo(t *testing.T) {
	testutil.BindCurrentTestSetup(t)

	tag := "synced_up_to_260241"
	tdb := testutil.LoadTestDataHandler(tag)
	db := ObtainDB(tag)
	result, err := db.RetrieveBlockFeeInfo()
	if err != nil {
		testutil.ReportTestFailed(
			"RetrieveBlockFeeInfo() failed: ",
			err)
	}

	// check result by MD5 hash
	stringRepresentation := fmt.Sprintf("%#v", result)
	md5Result := testutil.MD5ofString(stringRepresentation)

	expectedMD5 := tdb.GetExpectedMD5OfRetrieveBlockFeeInfoString()
	if expectedMD5 != md5Result {
		testutil.ReportTestFailed(
			"db.RetrieveBlockFeeInfo() returned dbtypes.ChartsData result,\n"+
				"MD5 of the result string representation is <%v>\n"+
				"and it does not match the expected: <%v>",
			md5Result,
			expectedMD5)
	}
}
