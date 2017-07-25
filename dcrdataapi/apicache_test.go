package dcrdataapi

import (
	"container/heap"
	"testing"
)

// constants from time
const (
	secondsPerMinute int64 = 60
	secondsPerHour   int64 = 60 * 60
	secondsPerDay    int64 = 24 * secondsPerHour
	secondsPerWeek   int64 = 7 * secondsPerDay
)

// TODO: Make a proper test rather than a playground

func TestBlockPriorityQueue(t *testing.T) {
	pq := NewBlockPriorityQueue(5)
	//pq.SetLessFn(LessByAccessCountThenHeight)
	//pq.SetLessFn(LessByAccessCount)
	//pq.SetLessFn(LessByAccessTime)
	//pq.SetLessFn(LessByHeight)
	pq.SetLessFn(MakeLessByAccessTimeThenCount(secondsPerDay))

	cachedBlocks := []*CachedBlock{
		NewCachedBlock(&BlockDataBasic{
			Height: 123,
		}),
		NewCachedBlock(&BlockDataBasic{
			Height: 1000,
		}),
		NewCachedBlock(&BlockDataBasic{
			Height: 1,
		}),
		NewCachedBlock(&BlockDataBasic{
			Height: 400,
		}),
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

	// heap.Push(pq, &BlockDataBasic{Height: 1001})
	pq.Insert(&BlockDataBasic{Height: 1001})
	// heap.Push(pq, &BlockDataBasic{Height: 1002})
	pq.Insert(&BlockDataBasic{Height: 0})
	pq.Insert(&BlockDataBasic{Height: 6})

	for pq.Len() > 0 {
		cachedBlock := heap.Pop(pq).(*CachedBlock)
		t.Logf("%8d\t%4d\t%d\t%4d\n", cachedBlock.summary.Height, cachedBlock.accesses, cachedBlock.accessTime, pq.Len())
	}

	heap.Push(pq, &BlockDataBasic{Height: 1})
}
