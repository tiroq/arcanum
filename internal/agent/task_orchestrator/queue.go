package taskorchestrator

import (
	"container/heap"
)

// PriorityQueue implements heap.Interface for deterministic task scheduling.
// Ordering: highest priority first; ties broken by oldest insertion time.
type PriorityQueue struct {
	entries []TaskQueueEntry
}

// NewPriorityQueue creates an empty priority queue.
func NewPriorityQueue() *PriorityQueue {
	return &PriorityQueue{}
}

// BuildFromEntries creates a priority queue from a slice of entries.
func BuildFromEntries(entries []TaskQueueEntry) *PriorityQueue {
	pq := &PriorityQueue{entries: make([]TaskQueueEntry, len(entries))}
	copy(pq.entries, entries)
	heap.Init(pq)
	return pq
}

// Push adds an entry. Required by heap.Interface.
func (pq *PriorityQueue) Push(x interface{}) {
	pq.entries = append(pq.entries, x.(TaskQueueEntry))
}

// Pop removes and returns the last element. Required by heap.Interface.
func (pq *PriorityQueue) Pop() interface{} {
	old := pq.entries
	n := len(old)
	item := old[n-1]
	pq.entries = old[:n-1]
	return item
}

// Len returns the number of entries.
func (pq *PriorityQueue) Len() int {
	return len(pq.entries)
}

// Less reports whether element i should sort before element j.
// Higher priority first; ties broken by earlier insertion time.
func (pq *PriorityQueue) Less(i, j int) bool {
	if pq.entries[i].PriorityScore != pq.entries[j].PriorityScore {
		return pq.entries[i].PriorityScore > pq.entries[j].PriorityScore
	}
	return pq.entries[i].InsertedAt.Before(pq.entries[j].InsertedAt)
}

// Swap swaps elements i and j.
func (pq *PriorityQueue) Swap(i, j int) {
	pq.entries[i], pq.entries[j] = pq.entries[j], pq.entries[i]
}

// Enqueue adds an entry to the queue.
func (pq *PriorityQueue) Enqueue(e TaskQueueEntry) {
	heap.Push(pq, e)
}

// Dequeue removes and returns the highest-priority entry.
func (pq *PriorityQueue) Dequeue() (TaskQueueEntry, bool) {
	if pq.Len() == 0 {
		return TaskQueueEntry{}, false
	}
	return heap.Pop(pq).(TaskQueueEntry), true
}

// Peek returns the highest-priority entry without removing it.
func (pq *PriorityQueue) Peek() (TaskQueueEntry, bool) {
	if pq.Len() == 0 {
		return TaskQueueEntry{}, false
	}
	return pq.entries[0], true
}

// TopN returns the top N entries without removing them.
func (pq *PriorityQueue) TopN(n int) []TaskQueueEntry {
	if n <= 0 || pq.Len() == 0 {
		return nil
	}
	// Create a copy to pop from without modifying the original.
	cpy := &PriorityQueue{entries: make([]TaskQueueEntry, len(pq.entries))}
	copy(cpy.entries, pq.entries)
	heap.Init(cpy)

	var result []TaskQueueEntry
	for i := 0; i < n && cpy.Len() > 0; i++ {
		result = append(result, heap.Pop(cpy).(TaskQueueEntry))
	}
	return result
}

// Entries returns a snapshot of all entries (unordered).
func (pq *PriorityQueue) Entries() []TaskQueueEntry {
	result := make([]TaskQueueEntry, len(pq.entries))
	copy(result, pq.entries)
	return result
}
