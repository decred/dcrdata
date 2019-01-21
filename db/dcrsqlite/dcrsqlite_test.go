package dcrsqlite

/*
This file contains package-related test-setup utils
*/
import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdata/v4/testutil"
)

func TestParseUnknownTicketError(t *testing.T) {
	expectedHashStr := "b7019b626f5ad29936214779435cfed4063a539f2b4bbbb7ca9196876e2a924e"
	expectedHash, _ := chainhash.NewHashFromStr(expectedHashStr)

	errStr := "dcrsqlite.SyncDBAsync failed at height 310844: unknown ticket " +
		expectedHashStr + " spent in block."

	err := errors.New(errStr)
	ticketHash := parseUnknownTicketError(err)
	if ticketHash == nil {
		t.Errorf("ticket hash not identified")
	}
	if *ticketHash != *expectedHash {
		t.Errorf("incorrect ticket hash. got %v, expected %v", ticketHash, expectedHash)
	}

	errStrBad := "unknown ticket 988088bf810ce82608db020bcd6d7955d7d60d964856c3a64941e45c2fc0d73e spent in block."
	err = errors.New(errStrBad)
	ticketHash = parseUnknownTicketError(err)
	if ticketHash == nil {
		t.Errorf("ticket hash not identified")
	}
	if *ticketHash == *expectedHash {
		t.Errorf("those should not have been equal")
	}

	errStrNoHash := "unknown ticket notahashatall spent in block."
	err = errors.New(errStrNoHash)
	ticketHash = parseUnknownTicketError(err)
	if ticketHash != nil {
		t.Errorf("ticket hash incorrect. expected <nil>, got %v", ticketHash)
	}

	errStrNoMsg := "nifty ticket 988088bf810ce82608db020bcd6d7955d7d60d964856c3a64941e45c2fc0d73e spent in sock."
	err = errors.New(errStrNoMsg)
	ticketHash = parseUnknownTicketError(err)
	if ticketHash != nil {
		t.Errorf("ticket hash incorrect. expected <nil>, got %v", ticketHash)
	}
}

// DBPathForTest produces path inside dedicated test folder for current test
func DBPathForTest() string {
	testName := testutil.CurrentTestSetup().Name()
	testutil.ResetTempFolder(&testName)
	target := filepath.Join(testName, testutil.DefaultDBFileName)
	targetDBFile := testutil.FilePathInsideTempDir(target)
	return targetDBFile
}

// InitTestDB creates default DB instance
func InitTestDB(targetDBFile string) *DB {
	dbInfo := &DBInfo{FileName: targetDBFile}
	db, err := InitDB(dbInfo)
	if err != nil {
		testutil.ReportTestFailed("InitDB() failed: %v", err)
	}
	if db == nil {
		testutil.ReportTestFailed("InitDB() failed")
	}
	return db //is not nil
}

var reusableEmptyDB *DB

// ObtainReusableEmptyDB returns a single reusable instance of an empty DB. The instance
// is created once during the first call. All the subsequent calls will return
// result cached in the reusableEmptyDB variable above.
func ObtainReusableEmptyDB() *DB {
	if reusableEmptyDB == nil {
		testName := "reusableEmptyDB"
		testutil.ResetTempFolder(&testName)
		target := filepath.Join(testName, testutil.DefaultDBFileName)
		targetDBFile := testutil.FilePathInsideTempDir(target)
		reusableEmptyDB = InitTestDB(targetDBFile)
	}
	return reusableEmptyDB
}
