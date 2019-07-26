package explorer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decred/dcrdata/db/dcrpg/v4"
)

const (
	viewsPath = "../views"
)

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

	var chainDBStub ChainDBStub

	exp := New(&ExplorerConfig{
		DataSource:      &chainDBStub,
		UseRealIP:       false,
		AppVersion:      "test",
		DevPrefetch:     false,
		Viewsfolder:     viewsPath,
		XcBot:           nil,
		Tracker:         nil,
		AgendasSource:   nil,
		ProposalsSource: nil,
		PoliteiaURL:     "",
		MainnetLink:     "/",
		TestnetLink:     "/",
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
