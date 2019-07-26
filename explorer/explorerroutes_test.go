package explorer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrdata/db/dcrpg/v4"
	"github.com/decred/dcrdata/db/dcrsqlite/v4"
	"github.com/decred/dcrdata/explorer/types/v2"
)

const (
	viewsPath = "../views"
)

// WiredDBStub satisfies explorerDataSourceLite, but will likely panic with a
// nil pointer dereference for methods we do not explicitly define for
// WiredDBStub for these tests.
type WiredDBStub struct {
	// Embedding *dcrsqlite.WiredDB promotes all of the methods needed for
	// WireDBStub to satisfy the explorerDataSourceLite interface. This allows
	// us to only implement for WiredDBStub the methods required for the tests.
	*dcrsqlite.WiredDB
}

// GetChainParams is needed by explorer.New.
func (ws *WiredDBStub) GetChainParams() *chaincfg.Params {
	return chaincfg.MainNetParams()
}

// GetTip is required to populate a CommonPageData for the explorer.
func (ws *WiredDBStub) GetTip() (*types.WebBasicBlock, error) {
	return &types.WebBasicBlock{
		Hash:       "00000000000000001cf26099864194b77b860fa11241baf9f39aad436d43c7a6",
		Height:     295566,
		Size:       10111,
		Difficulty: 11926609305.972,
		StakeDiff:  103.87403392,
		Time:       1543259358,
		PoolSize:   40779,
		PoolValue:  4145018.51407483,
		PoolValAvg: 101.645908778411,
		PoolWinners: []string{
			"77ea8ce00acc53782501635ffae22df4200acfe6d92d0e47a550079f24eab86f",
			"57f309077a1abe8d4048ed0204ba78afafd50bf9896fcb7cf2c8162b241250f8",
			"16dda0e79ac8e6c8b168d41d840017064b5be1180fcaefc3f29e50d678eefff6",
			"b122bce0fb617edb4de40b4fc955ed64f4d3966b191b1d950a99759d8df0a6fa",
			"3544a5dbad15f5de90cf4ada0204679748e63b5be440720c1fb0c22c648f23a2",
		},
	}, nil
}

// ChainDBStub satisfies explorerDataSource, but will likely panic with a nil
// pointer dereference for methods we do not explicitly define here.
type ChainDBStub struct {
	// Embedding *dcrpg.ChainDBRPC promotes all of the methods needed for
	// WireDBStub to satisfy the explorerDataSource interface. This allows us to
	// only implement for ChainDBStub the methods required for the tests.
	*dcrpg.ChainDBRPC
}

func TestStatusPageResponseCodes(t *testing.T) {
	// req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	var wiredDBStub WiredDBStub
	var chainDBStub ChainDBStub

	exp := New(&ExplorerConfig{
		DataSource:        &wiredDBStub,
		PrimaryDataSource: &chainDBStub,
		UseRealIP:         false,
		AppVersion:        "test",
		DevPrefetch:       false,
		Viewsfolder:       viewsPath,
		XcBot:             nil,
		Tracker:           nil,
		AgendasSource:     nil,
		ProposalsSource:   nil,
		PoliteiaURL:       "",
		MainnetLink:       "/",
		TestnetLink:       "/",
	})

	// handler := http.HandlerFunc()
	// handler.ServeHTTP(rr, req)

	io := []struct {
		ExpStatus expStatus
		RespCode  int
	}{
		{
			ExpStatusNotSupported, http.StatusUnprocessableEntity,
		},
	}

	for _, oi := range io {
		exp.StatusPage(rr, "code", "msg", "junk", oi.ExpStatus)

		resp := rr.Result()
		if resp.StatusCode != oi.RespCode {
			t.Errorf("wrong code %d (%s), expected %d (%s)",
				resp.StatusCode, http.StatusText(resp.StatusCode),
				oi.RespCode, http.StatusText(oi.RespCode))
		}
	}
}

// func TestTxPageResponseCodes(t *testing.T) {
// 	var wiredDBStub testTxPageWiredDBStub
// 	var chainDBStub ChainDBStub
// 	exp := New(&wiredDBStub, &chainDBStub, false, "test", false, viewsPath, nil)

// 	io := []struct {
// 		ExpStatus expStatus
// 		RespCode  int
// 	}{
// 		{
// 			ExpStatusBitcoin, http.StatusUnprocessableEntity,
// 		},
// 	}

// 	for _, oi := range io {
// 		req := httptest.NewRequest("GET", "/", nil)
// 		rr := httptest.NewRecorder()

// 		// Simulate the TransactionHashCtx middleware.
// 		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 			ctx := context.WithValue(r.Context(), ctxTxHash, "notahash")
// 			http.HandlerFunc(exp.TxPage).ServeHTTP(w, r.WithContext(ctx))
// 		})

// 		handler.ServeHTTP(rr, req)

// 		resp := rr.Result()
// 		if resp.StatusCode != oi.RespCode {
// 			t.Errorf("wrong code %d (%s), expected %d (%s)",
// 				resp.StatusCode, http.StatusText(resp.StatusCode),
// 				oi.RespCode, http.StatusText(oi.RespCode))
// 		}
// 	}
// }
