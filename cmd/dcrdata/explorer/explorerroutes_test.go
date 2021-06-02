package explorer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrdata/db/dcrpg/v6"
	"github.com/decred/dcrdata/v6/explorer/types"
)

const (
	viewsPath = "../views"
)

// ChainDBStub satisfies explorerDataSource, but will likely panic with a nil
// pointer dereference for methods we do not explicitly define here.
type ChainDBStub struct {
	// Embedding *dcrpg.ChainDB promotes all of the methods needed for
	// WireDBStub to satisfy the explorerDataSource interface. This allows us to
	// only implement for ChainDBStub the methods required for the tests.
	*dcrpg.ChainDB
}

// GetChainParams is needed by explorer.New.
func (ws *ChainDBStub) GetChainParams() *chaincfg.Params {
	return chaincfg.MainNetParams()
}

// GetTip is required to populate a CommonPageData for the explorer.
func (ws *ChainDBStub) GetTip() (*types.WebBasicBlock, error) {
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

func TestStatusPageResponseCodes(t *testing.T) {
	// req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	var chainDBStub ChainDBStub

	exp := New(&ExplorerConfig{
		DataSource:    &chainDBStub,
		UseRealIP:     false,
		AppVersion:    "test",
		DevPrefetch:   false,
		Viewsfolder:   viewsPath,
		XcBot:         nil,
		Tracker:       nil,
		AgendasSource: nil,
		Proposals:     nil,
		PoliteiaURL:   "",
		MainnetLink:   "/",
		TestnetLink:   "/",
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

func TestPageNumbersDesc(t *testing.T) {
	type pageTest struct {
		name     string
		rows     int
		pageSize int
		offset   int
		expected string
		active   string
	}
	pageTests := make([]pageTest, 0)

	stringify := func(pages pageNumbers) string {
		out := ""
		for i, page := range pages {
			if i != 0 {
				out += " "
			}
			if page.Str == ellipsisHTML {
				out += "..."
			} else {
				out += page.Str
			}
		}
		return out
	}

	// case: fewer than 11 pages
	pageTests = append(pageTests, pageTest{
		name:     "< 11",
		rows:     95,
		pageSize: 10,
		offset:   91,
		expected: "1 2 3 4 5 6 7 8 9 10",
		active:   "1",
	})
	// case: exactly 11, page near endings
	pageTests = append(pageTests, pageTest{
		name:     "= 11, near end",
		rows:     105,
		pageSize: 10,
		offset:   1,
		expected: "1 ... 4 5 6 7 8 9 10 11",
		active:   "11",
	})

	// case: exactly 11, page near beginning
	pageTests = append(pageTests, pageTest{
		name:     "= 11, near beginning",
		rows:     105,
		pageSize: 10,
		offset:   91,
		expected: "1 2 3 4 5 6 7 8 ... 11",
		active:   "2",
	})

	// case: exactly 11, page in middle
	pageTests = append(pageTests, pageTest{
		name:     "= 11, in middle",
		rows:     105,
		pageSize: 10,
		offset:   55,
		expected: "1 ... 3 4 5 6 7 8 9 ... 11",
		active:   "6",
	})

	// case: 20 pages near beginning
	pageTests = append(pageTests, pageTest{
		name:     "= 20, near beginning",
		rows:     195,
		pageSize: 10,
		offset:   184,
		expected: "1 2 3 4 5 6 7 8 ... 20",
		active:   "2",
	})

	// case: 20 pages near end
	pageTests = append(pageTests, pageTest{
		name:     "= 20, near end",
		rows:     195,
		pageSize: 10,
		offset:   25,
		expected: "1 ... 13 14 15 16 17 18 19 20",
		active:   "18",
	})

	// case: 20 pages in middle
	pageTests = append(pageTests, pageTest{
		name:     "= 20, in middle",
		rows:     195,
		pageSize: 10,
		offset:   85,
		expected: "1 ... 9 10 11 12 13 14 15 ... 20",
		active:   "12",
	})

	// case: less than a page
	pageTests = append(pageTests, pageTest{
		name:     "< a page",
		rows:     6,
		pageSize: 10,
		offset:   5,
		expected: "",
		active:   "1",
	})

	for _, test := range pageTests {
		nums := calcPagesDesc(test.rows, test.pageSize, test.offset, "/test/%d")
		if stringify(nums) != test.expected {
			t.Errorf("failed stringify for test \"%s\". Expected %s, found %s", test.name, test.expected, stringify(nums))
		}
		for i, num := range nums {
			if num.Link == "" {
				continue
			}
			if test.active == num.Str {
				if !num.Active {
					t.Errorf("failed active for test \"%s\", page string %s at index %d", test.name, num.Str, i)
				}
			} else {
				if num.Active {
					t.Errorf("found false active for test \"%s\", page string %s at index %d", test.name, num.Str, i)
				}
			}
		}
	}

	nums := calcPagesDesc(25, 10, 24, "/test/%d")
	if len(nums) != 3 {
		t.Errorf("unexpected page count")
	}
	if nums[0].Link != "/test/25" {
		t.Errorf("last page has wrong link: %s", nums[0].Link)
	}
	if nums[1].Link != "/test/15" {
		t.Errorf("page 2 has wrong link: %s", nums[1].Link)
	}
	if nums[2].Link != "/test/5" {
		t.Errorf("page 3 has wrong link: %s", nums[2].Link)
	}
}

func TestPageNumbers(t *testing.T) {
	type pageTest struct {
		name     string
		rows     int
		pageSize int
		offset   int
		expected string
		active   string
	}
	pageTests := make([]pageTest, 0)

	stringify := func(pages pageNumbers) string {
		out := ""
		for i, page := range pages {
			if i != 0 {
				out += " "
			}
			if page.Str == ellipsisHTML {
				out += "..."
			} else {
				out += page.Str
			}
		}
		return out
	}

	// case: fewer than 11 pages
	pageTests = append(pageTests, pageTest{
		name:     "< 11",
		rows:     95,
		pageSize: 10,
		offset:   91,
		expected: "1 2 3 4 5 6 7 8 9 10",
		active:   "10",
	})
	// case: exactly 11, page near beginning
	pageTests = append(pageTests, pageTest{
		name:     "= 11, near beginning",
		rows:     105,
		pageSize: 10,
		offset:   1,
		expected: "1 2 3 4 5 6 7 8 ... 11",
		active:   "1",
	})

	// case: exactly 11, page near end
	pageTests = append(pageTests, pageTest{
		name:     "= 11, near end",
		rows:     105,
		pageSize: 10,
		offset:   91,
		expected: "1 ... 4 5 6 7 8 9 10 11",
		active:   "10",
	})

	// case: exactly 11, page in middle
	pageTests = append(pageTests, pageTest{
		name:     "= 11, in middle",
		rows:     105,
		pageSize: 10,
		offset:   55,
		expected: "1 ... 3 4 5 6 7 8 9 ... 11",
		active:   "6",
	})

	// case: 20 pages near beginning
	pageTests = append(pageTests, pageTest{
		name:     "= 20, near beginning",
		rows:     195,
		pageSize: 10,
		offset:   15,
		expected: "1 2 3 4 5 6 7 8 ... 20",
		active:   "2",
	})

	// case: 20 pages near end
	pageTests = append(pageTests, pageTest{
		name:     "= 20, near end",
		rows:     195,
		pageSize: 10,
		offset:   174,
		expected: "1 ... 13 14 15 16 17 18 19 20",
		active:   "18",
	})

	// case: 20 pages in middle
	pageTests = append(pageTests, pageTest{
		name:     "= 20, in middle",
		rows:     195,
		pageSize: 10,
		offset:   75,
		expected: "1 ... 5 6 7 8 9 10 11 ... 20",
		active:   "8",
	})

	// case: less than a page
	pageTests = append(pageTests, pageTest{
		name:     "< a page",
		rows:     6,
		pageSize: 10,
		offset:   5,
		expected: "",
		active:   "1",
	})

	for _, test := range pageTests {
		nums := calcPages(test.rows, test.pageSize, test.offset, "/test/%d")
		if stringify(nums) != test.expected {
			t.Errorf("failed stringify for test \"%s\". Expected %s, found %s", test.name, test.expected, stringify(nums))
		}
		for i, num := range nums {
			if num.Link == "" {
				continue
			}
			if test.active == num.Str {
				if !num.Active {
					t.Errorf("failed active for test \"%s\", page string %s at index %d", test.name, num.Str, i)
				}
			} else {
				if num.Active {
					t.Errorf("found false active for test \"%s\", page string %s at index %d", test.name, num.Str, i)
				}
			}
		}
	}

	nums := calcPages(25, 10, 24, "/test/%d")
	if len(nums) != 3 {
		t.Errorf("unexpected page count")
	}
	if nums[0].Link != "/test/0" {
		t.Errorf("last page has wrong link: %s", nums[0].Link)
	}
	if nums[1].Link != "/test/10" {
		t.Errorf("page 2 has wrong link: %s", nums[1].Link)
	}
	if nums[2].Link != "/test/20" {
		t.Errorf("page 3 has wrong link: %s", nums[2].Link)
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
