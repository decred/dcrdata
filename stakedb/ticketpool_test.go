package stakedb

import (
	"crypto/rand"
	"os"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

func randomHash() chainhash.Hash {
	var hash chainhash.Hash
	if _, err := rand.Read(hash[:]); err != nil {
		panic("boom")
	}
	return hash
}

func randomHashSlice(N int) []chainhash.Hash {
	s := make([]chainhash.Hash, 0, N)
	for i := 0; i < N; i++ {
		s = append(s, randomHash())
	}
	return s
}

var (
	dbFile             = "pooldiffs.db"
	dbFileRef          = "pooldiffs0.db"
	dbFileFull         = "stakedb_ticket_pool.db"
	fullHeight   int64 = 204578
	fullPoolSize       = 41061
	poolSpots          = []int64{32215, 43268, 85508, 66322, 178346, 11013, 98265, 1081,
		40170, 105957, 187824, 148370, 14478, 153239, 109536, 169157, 92064,
		200277, 27558, 116203, 2338, 28323, 51459, 145753, 137377, 108501,
		138521, 33203, 39979, 56765, 175322, 87124, 192306, 157350, 13823,
		49777, 104992, 167135, 4507, 102258, 138354, 99312, 100434, 135096,
		46955, 127918, 138468, 190953, 135998, 64990, 87542, 149742, 30626,
		196567, 55479, 63701, 117733, 46739, 141352, 146690, 75763, 76768,
		112648, 124313, 857, 159517, 48344, 200476, 140524, 73190, 12496, 40740,
		124512, 55456, 100552, 166467, 197815, 195949, 33245, 27286, 4914,
		200908, 194977, 174689, 93864, 121588, 157975, 140562, 111278, 106384,
		160849, 69184, 31170, 125034, 109197, 78142, 138721, 186486, 69486, 6549}
	poolSizes = []int{42958, 41214, 42175, 43086, 41214, 42193, 42511, 11335,
		42083, 43278, 40946, 43035, 41655, 41109, 42460, 40875, 42580, 41582,
		42941, 41145, 22774, 43317, 41798, 43964, 43015, 44058, 43059, 42244,
		41857, 41198, 41104, 42370, 40283, 40769, 41569, 41044, 43205, 40659,
		33501, 42661, 43393, 42751, 42601, 43079, 40992, 42884, 42824, 42420,
		43353, 42766, 42086, 41893, 42310, 41423, 41552, 41727, 40955, 41509,
		42896, 44797, 42578, 42323, 41769, 43066, 6855, 41000, 41498, 41595,
		44108, 42370, 41775, 41834, 42125, 41587, 42134, 40977, 40619, 41825,
		42132, 42625, 37710, 41431, 40037, 41130, 41997, 41667, 41016, 43916,
		41379, 43994, 40935, 43343, 43027, 42718, 42673, 41802, 44422, 40786,
		42078, 41450}
)

// TestTicketPoolTraverseFull tests AdvanceToTip and Pool for random access.
// After each Pool call, it checks the pool size against the expected size.
func TestTicketPoolTraverseFull(t *testing.T) {
	if _, err := os.Stat(dbFileFull); err != nil {
		t.Skipf("%s not found, skipping TestTicketPoolTraverseFull", dbFileFull)
	}

	t.Logf("Loading entire ticket pool diffs from %s...", dbFileFull)
	p, err := NewTicketPool(dbFileFull)
	if err != nil {
		t.Fatalf("NewTicketPool failed: %v", err)
	}
	defer p.Close()

	tip := p.Tip()
	if tip != fullHeight {
		t.Fatalf("tip incorrect. expected %d, got %d", fullHeight, tip)
	}

	initPoolSize := p.CurrentPoolSize()
	if initPoolSize != 0 {
		t.Fatalf("initial pool size incorrect. expected 0, got %d", initPoolSize)
	}

	cursor := p.Cursor()
	if cursor != 0 {
		t.Errorf("cursor incorrect. expected 0, got %d", cursor)
	}

	t.Log("Advancing cursor to tip (applying all diffs and building pool map).")
	if err = p.AdvanceToTip(); err != nil {
		t.Errorf("failed to advance pool to tip: %v", err)
	}

	cursor = p.Cursor()
	if cursor != tip {
		t.Errorf("cursor not at tip. expected %d, got %d", tip, cursor)
	}

	poolSize := p.CurrentPoolSize()
	if poolSize != fullPoolSize {
		t.Errorf("pool size incorrect. expected %d, got %d", fullPoolSize, poolSize)
	}

	ph, cursor2 := p.CurrentPool()
	if cursor != cursor2 {
		t.Errorf("incorrect cursor returned from CurrentPool. expected %d, got %d", cursor, cursor2)
	}

	if len(ph) != poolSize {
		t.Errorf("incorrect pool size from CurrentPool. expected %d, got %d", len(ph), poolSize)
	}

	pss := make([]int, len(poolSpots))
	for i, spot := range poolSpots {
		ps, err := p.Pool(spot)
		if err != nil {
			t.Errorf("unable to scan pool to block %d: %v", spot, err)
		}
		pss[i] = len(ps)
		//t.Logf("Pool size at height %d: %d", spot, len(ps))
		if poolSizes[i] != pss[i] {
			t.Errorf("incorrect pool size at height %d. expected %d, got %d",
				spot, poolSizes[i], pss[i])
		}
	}
}

// TestTicketPoolAppendInvalid ensures that AppendAndAdvancePool (advance) will
// error if non-existing tickets are listed in Out slice of a PoolDiff. It also
// tests correct use of AppendAndAdvancePool, verifying that tickets are removed
// from the pool as expected.
func TestTicketPoolAppendInvalid(t *testing.T) {
	if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to delete db file: %v", err)
	}
	p, err := NewTicketPool(dbFile)
	if err != nil {
		t.Fatalf("NewTicketPool failed: %v", err)
	}
	defer p.Close()

	tip := p.Tip()
	if tip != 0 {
		t.Fatalf("tip incorrect. expected 0, got %d", tip)
	}

	initPoolSize := p.CurrentPoolSize()
	if initPoolSize != 0 {
		t.Fatalf("initial pool size incorrect. expected 0, got %d", initPoolSize)
	}

	inN, outN := 4, 3
	diff := &PoolDiff{
		In:  randomHashSlice(inN),
		Out: randomHashSlice(outN),
	}
	if err := p.AppendAndAdvancePool(diff, tip+1); err == nil {
		t.Errorf("AppendAndAdvancePool did not error while advancing and removing non-existing hashes")
	}

	newTip := p.Tip()
	if newTip != tip+1 {
		t.Errorf("new tip incorrect. expected %d, got %d", tip+1, newTip)
	}
	tip++

	poolSize := p.CurrentPoolSize()
	delta := inN // out were not in the pool to begin with
	if poolSize != initPoolSize+delta {
		t.Errorf("pool size incorrect. expected %d, got %d", initPoolSize+delta, poolSize)
	}
	t.Logf("Pool size: %d", poolSize)

	// advance, removing 2 and adding 2
	initPoolSize = poolSize
	inN = 2
	diff2 := &PoolDiff{
		In:  randomHashSlice(inN),
		Out: diff.In[:2], // remove first two added last time
	}

	if err := p.AppendAndAdvancePool(diff2, tip+1); err != nil {
		t.Errorf("AppendAndAdvancePool failed to advance: %v", err)
	}

	newTip = p.Tip()
	if newTip != tip+1 {
		t.Errorf("new tip incorrect. expected %d, got %d", tip+1, newTip)
	}
	//tip++

	poolSize = p.CurrentPoolSize()
	if poolSize != initPoolSize {
		t.Errorf("pool size incorrect. expected %d, got %d", initPoolSize, poolSize)
	}
}

// TestTicketPoolRetreat tests retreat.
func TestTicketPoolRetreat(t *testing.T) {
	if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to delete db file: %v", err)
	}
	p, err := NewTicketPool(dbFile)
	if err != nil {
		t.Fatalf("NewTicketPool failed: %v", err)
	}
	defer p.Close()

	tip := p.Tip()
	if tip != 0 {
		t.Fatalf("tip incorrect. expected 0, got %d", tip)
	}
	cursor := p.Cursor()
	if cursor != 0 {
		t.Errorf("cursor incorrect. expected 0, got %d", cursor)
	}

	initPoolSize := p.CurrentPoolSize()
	if initPoolSize != 0 {
		t.Fatalf("initial pool size incorrect. expected 0, got %d", initPoolSize)
	}

	inN := 4
	diff := &PoolDiff{
		In: randomHashSlice(inN),
	}
	if err := p.AppendAndAdvancePool(diff, tip+1); err != nil {
		t.Errorf("AppendAndAdvancePool failed: %v", err)
	}

	newTip := p.Tip()
	if newTip != tip+1 {
		t.Errorf("new tip incorrect. expected %d, got %d", tip+1, newTip)
	}
	tip++

	newCursor := p.Cursor()
	if newCursor != cursor+1 {
		t.Errorf("cursor incorrect. expected %d, got %d", cursor+1, newCursor)
	}
	cursor++

	poolSize := p.CurrentPoolSize()
	delta := inN // out were not in the pool to begin with
	if poolSize != initPoolSize+delta {
		t.Errorf("pool size incorrect. expected %d, got %d", initPoolSize+delta, poolSize)
	}

	if err := p.retreat(); err != nil {
		t.Errorf("retreat: %v", err)
	}

	poolSize2 := p.CurrentPoolSize()
	if poolSize-delta != poolSize2 {
		t.Errorf("pool size incorrect. expected %d, got %d", poolSize-delta, poolSize2)
	}

	newTip = p.Tip()
	if newTip != tip {
		t.Errorf("tip changed. expected %d, got %d", tip+1, newTip)
	}
	//tip++

	newCursor = p.Cursor()
	if newCursor != cursor-1 {
		t.Errorf("cursor incorrect. expected %d, got %d", cursor-1, newCursor)
	}
	//cursor++
}

// TestTicketPoolHeight tests Pool(height).
func TestTicketPoolHeight(t *testing.T) {
	if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to delete db file: %v", err)
	}
	p, err := NewTicketPool(dbFile)
	if err != nil {
		t.Fatalf("NewTicketPool failed: %v", err)
	}
	defer p.Close()

	tip := p.Tip()
	if tip != 0 {
		t.Fatalf("tip incorrect. expected 0, got %d", tip)
	}
	cursor := p.Cursor()
	if cursor != 0 {
		t.Errorf("cursor incorrect. expected 0, got %d", cursor)
	}

	initPoolSize := p.CurrentPoolSize()
	if initPoolSize != 0 {
		t.Fatalf("initial pool size incorrect. expected 0, got %d", initPoolSize)
	}

	inN := 4
	diff := &PoolDiff{
		In: randomHashSlice(inN),
	}
	if err := p.AppendAndAdvancePool(diff, tip+1); err != nil {
		t.Errorf("AppendAndAdvancePool failed: %v", err)
	}

	newTip := p.Tip()
	if newTip != tip+1 {
		t.Errorf("new tip incorrect. expected %d, got %d", tip+1, newTip)
	}
	tip++

	newCursor := p.Cursor()
	if newCursor != cursor+1 {
		t.Errorf("cursor incorrect. expected %d, got %d", cursor+1, newCursor)
	}
	cursor++

	if cursor != tip {
		t.Errorf("cursor (%d) != tip (%d)", cursor, tip)
	}

	poolSize := p.CurrentPoolSize()
	delta := inN // out were not in the pool to begin with
	if poolSize != initPoolSize+delta {
		t.Errorf("pool size incorrect. expected %d, got %d", initPoolSize+delta, poolSize)
	}

	pX, _ := p.CurrentPool()
	if len(pX) != poolSize {
		t.Errorf("CurrrentPool size incorrect. expected %d, got %d", poolSize, len(pX))
	}

	pY, err := p.Pool(tip)
	if err != nil {
		t.Errorf("Pool(tip) failed: %v", err)
	}
	if len(pY) != poolSize {
		t.Errorf("incorrect pool size. expected %d, got %d", poolSize, len(pY))
	}

	p0, err := p.Pool(0)
	if err != nil {
		t.Errorf("Pool(tip) failed: %v", err)
	}
	if len(p0) != initPoolSize {
		t.Errorf("incorrect pool size. expected %d, got %d", poolSize, len(pY))
	}
	cursor = p.Cursor()
	if cursor != 0 {
		t.Errorf("Cursor returned %d, expected 0", cursor)
	}
}

// TestTicketPoolPersistent tests the persistent DB by loading a referece .db
// file, and verifies its state is as expected, and Pool works on it.
func TestTicketPoolPersistent(t *testing.T) {
	p, err := NewTicketPool(dbFileRef)
	if err != nil {
		t.Fatalf("NewTicketPool failed: %v", err)
	}
	defer p.Close()

	tip := p.Tip()
	if tip != 2 {
		t.Fatalf("tip incorrect. expected 2, got %d", tip)
	}
	cursor := p.Cursor()
	if cursor != 0 {
		t.Errorf("cursor incorrect. expected 0, got %d", cursor)
	}

	initPoolSize := p.CurrentPoolSize()
	if initPoolSize != 0 {
		t.Fatalf("initial pool size incorrect. expected 0, got %d", initPoolSize)
	}

	tipPool, err := p.Pool(tip)
	if err != nil {
		t.Fatalf("Pool(tip) failed: %v", err)
	}
	t.Logf("tip pool size: %d", len(tipPool))

	tipPoolSize := p.CurrentPoolSize()
	if tipPoolSize != 4 {
		t.Fatalf("initial pool size incorrect. expected 4, got %d", tipPoolSize)
	}
}
