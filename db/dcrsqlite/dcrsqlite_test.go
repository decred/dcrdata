package dcrsqlite

import (
	"errors"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	sqlite3 "github.com/mattn/go-sqlite3"
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

func TestFilterError(t *testing.T) {
	var triggered bool
	db := DB{
		shutdownDcrdata: func() {
			triggered = true
		},
	}

	db.filterError(nil)
	if triggered {
		t.Errorf("shutdown unexpectedly triggered")
	}

	var err sqlite3.Error
	err.Code = sqlite3.ErrLocked
	db.filterError(err)
	if !triggered {
		t.Errorf("SQLite3 database locked error failed to trigger shutdown")
	}
}
