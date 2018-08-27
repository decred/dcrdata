package dbtypes

import (
	"reflect"
	"testing"
)

// TestMergeTicketPoolPurchases tests the functionality of MergeTicketPoolPurchases that
// merges the immature tickets data and live tickets data used to plot tickets
// purchase real time graph.
func TestMergeTicketPoolPurchases(t *testing.T) {
	// sample immature tickets purchase distribution data
	var immatureTickets = PoolTicketsData{
		Time:     []uint64{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30},
		Immature: []uint64{3, 8, 2, 7, 1, 6, 5, 8, 3, 9, 7, 1, 5, 2, 4},
		Live:     []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		RawPrice: []uint64{300, 800, 200, 700, 100, 600, 500, 800, 300, 900, 700, 100, 500, 200, 400},
		Price: []float64{0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001,
			0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001},
	}
	// sample live tickets purchase distribition data
	var liveTickets = PoolTicketsData{
		Time:     []uint64{26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
		Immature: []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Live:     []uint64{2, 5, 9, 1, 7, 8, 3, 4, 6, 2, 3, 6, 5, 9, 1},
		RawPrice: []uint64{200, 500, 900, 100, 700, 800, 300, 400, 600, 200, 300, 600, 500, 900, 100},
		Price: []float64{0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001,
			0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001},
	}
	// expected result
	var finalResult = PoolTicketsData{
		Time: []uint64{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34,
			35, 36, 37, 38, 39, 40},
		Immature: []uint64{3, 8, 2, 7, 1, 6, 5, 8, 3, 9, 7, 1, 5, 2, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Live:     []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 5, 9, 1, 7, 8, 3, 4, 6, 2, 3, 6, 5, 9, 1},
		Price: []float64{0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001,
			0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001,
			0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001, 0.000001},
	}

	var result = MergeTicketPoolPurchases(immatureTickets, liveTickets)

	for _, v := range []int{len(result.Time), len(result.Price), len(result.Immature), len(result.Live)} {
		if len(finalResult.Price) != v {
			t.Fatalf("expected the merged tickets data to have arrays of length %d but was %d ",
				v, len(finalResult.Price))
		}
	}

	if !reflect.DeepEqual(result, finalResult) {
		t.Fatal("expected the merged tickets data to match the correct data but they didn't")
	}
}
