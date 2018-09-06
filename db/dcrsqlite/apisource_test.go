package dcrsqlite

import (
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrdata/v3/testutil"
)

func TestIsZeroHashP2PHKAddress(t *testing.T) {
	testutil.BindCurrentTestSetup(t)

	mainnetDummy := "DsQxuVRvS4eaJ42dhQEsCXauMWjvopWgrVg"
	testnetDummy := "TsR28UZRprhgQQhzWns2M6cAwchrNVvbYq2"
	simnetDummy := "SsUMGgvWLcixEeHv3GT4TGYyez4kY79RHth"

	positiveTest := true
	negativeTest := !positiveTest

	testIsZeroHashP2PHKAddress(mainnetDummy, &chaincfg.MainNetParams, positiveTest)
	testIsZeroHashP2PHKAddress(testnetDummy, &chaincfg.TestNet3Params, positiveTest)
	testIsZeroHashP2PHKAddress(simnetDummy, &chaincfg.SimNetParams, positiveTest)

	// wrong network
	testIsZeroHashP2PHKAddress(mainnetDummy, &chaincfg.SimNetParams, negativeTest)
	testIsZeroHashP2PHKAddress(testnetDummy, &chaincfg.MainNetParams, negativeTest)
	testIsZeroHashP2PHKAddress(simnetDummy, &chaincfg.TestNet3Params, negativeTest)

	// wrong address
	testIsZeroHashP2PHKAddress("", &chaincfg.SimNetParams, negativeTest)
	testIsZeroHashP2PHKAddress("", &chaincfg.MainNetParams, negativeTest)
	testIsZeroHashP2PHKAddress("", &chaincfg.TestNet3Params, negativeTest)

}
func testIsZeroHashP2PHKAddress(expectedAddress string, params *chaincfg.Params, expectedTestResult bool) {
	result := IsZeroHashP2PHKAddress(expectedAddress, params)
	if expectedTestResult != result {
		testutil.ReportTestFailed(
			"IsZeroHashP2PHKAddress(%v) returned <%v>, expected <%v>",
			expectedAddress,
			result,
			expectedTestResult)
	}
}
