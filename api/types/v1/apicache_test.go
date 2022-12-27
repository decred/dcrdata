package types

import (
	"container/heap"
	"testing"
)

func TestCheckBlockHeightHash(t *testing.T) {
	d := &cachedBlockData{&BlockDataBasic{Height: 1001}, nil}
	height, hash, mismatch := checkBlockHeightHash(d)
	if mismatch {
		t.Errorf("checkBlockHeightHash: %v", mismatch)
	}
	t.Log(height, hash)
}

// TODO: Make a proper test rather than a playground

func TestBlockPriorityQueue(t *testing.T) {
	pq := NewBlockPriorityQueue(5)
	//pq.SetLessFn(LessByAccessCountThenHeight)
	//pq.SetLessFn(LessByAccessCount)
	//pq.SetLessFn(LessByAccessTime)
	//pq.SetLessFn(LessByHeight)
	pq.SetLessFn(MakeLessByAccessTimeThenCount(SecondsPerDay))

	cachedBlocks := []*CachedBlock{
		newCachedBlock(&BlockDataBasic{
			Height: 123,
		}, nil),
		newCachedBlock(&BlockDataBasic{
			Height: 1000,
		}, nil),
		newCachedBlock(&BlockDataBasic{
			Height: 1,
		}, nil),
		newCachedBlock(&BlockDataBasic{
			Height: 400,
		}, nil),
	}

	// reheap, which resets all access counts and times
	pq.ResetHeap(cachedBlocks)

	// forge the access counts
	cachedBlocks[0].accesses = 1
	cachedBlocks[1].accesses = 2
	cachedBlocks[2].accesses = 10
	cachedBlocks[3].accesses = 4
	heap.Init(pq)

	t.Log(pq.capacity, pq.Len(), pq.minHeight, pq.maxHeight)

	pq.Insert(&BlockDataBasic{Height: 1001}, nil)
	pq.Insert(&BlockDataBasic{Height: 0}, nil)
	pq.Insert(&BlockDataBasic{Height: 6}, nil)

	for pq.Len() > 0 {
		cachedBlock := heap.Pop(pq).(*CachedBlock)
		t.Logf("%8d\t%4d\t%d\t%4d\n", cachedBlock.summary.Height, cachedBlock.accesses, cachedBlock.accessTime, pq.Len())
	}

	blockData := &cachedBlockData{&BlockDataBasic{Height: 1}, nil}
	heap.Push(pq, blockData)
}
