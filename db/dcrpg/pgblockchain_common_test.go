// +build pgonline chartdata

package dcrpg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrdata/v6/db/cache"
	"github.com/decred/dcrdata/v6/testutil/dbconfig"
	"github.com/decred/slog"
)

type MemStats runtime.MemStats

func memStats() MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return MemStats(m)
}

func (m MemStats) String() string {
	bytesToMebiBytes := func(b uint64) uint64 {
		return b >> 20
	}
	out := "Memory statistics:\n"
	out += fmt.Sprintf("- Alloc: %v MiB\n", bytesToMebiBytes(m.Alloc))
	out += fmt.Sprintf("- TotalAlloc: %v MiB\n", bytesToMebiBytes(m.TotalAlloc))
	out += fmt.Sprintf("- System obtained: %v MiB\n", bytesToMebiBytes(m.Sys))
	out += fmt.Sprintf("- Lookups: %v\n", m.Lookups)
	out += fmt.Sprintf("- Mallocs: %v\n", m.Mallocs)
	out += fmt.Sprintf("- Frees: %v\n", m.Frees)
	out += fmt.Sprintf("- HeapAlloc: %v MiB\n", bytesToMebiBytes(m.HeapAlloc))
	out += fmt.Sprintf("- HeapObjects: %v\n", m.HeapObjects)
	out += fmt.Sprintf("- NumGC: %v\n", m.NumGC)
	return out
}

var (
	db           *ChainDB
	sqlDb        *sql.DB
	trefUNIX     int64 = 1454954400 // mainnet genesis block time
	trefStr            = "2016-02-08T12:00:00-06:00"
	addrCacheCap int   = 1e4
)

type dummyParser struct{}

func (p *dummyParser) UpdateSignal() <-chan struct{} {
	return make(chan struct{})
}

func openDB() (func() error, error) {
	dbi := &DBInfo{
		Host:   dbconfig.PGTestsHost,
		Port:   dbconfig.PGTestsPort,
		User:   dbconfig.PGTestsUser,
		Pass:   dbconfig.PGTestsPass,
		DBName: dbconfig.PGTestsDBName, // dcrdata_testnet3 for treasury testing
	}
	cfg := &ChainDBCfg{
		dbi,
		chaincfg.MainNetParams(),
		true, false, 24, 1024, 1 << 16,
	}
	var err error
	db, err = NewChainDB(context.Background(), cfg, nil, nil, new(dummyParser), nil, func() {})
	cleanUp := func() error { return nil }
	if db != nil {
		cleanUp = db.Close
	} else {
		return cleanUp, err
	}

	sqlDb = db.db
	if err = DropTestingTable(sqlDb); err != nil {
		return nil, err
	}
	if err = CreateTable(sqlDb, "testing"); err != nil {
		return nil, err
	}

	return cleanUp, err
}

func TestMain(m *testing.M) {
	// Setup dcrpg logger.
	UseLogger(slog.NewBackend(os.Stdout).Logger("dcrpg"))
	log.SetLevel(slog.LevelTrace)

	// Setup db/cache logger.
	cache.UseLogger(slog.NewBackend(os.Stdout).Logger("db/cache"))

	// your func
	cleanUp, err := openDB()
	defer cleanUp()
	if err != nil {
		panic(fmt.Sprintln("no db for testing:", err))
	}

	retCode := m.Run()

	// call with result of m.Run()
	os.Exit(retCode)
}
