package app

import (
	"context"
	"encoding/json"
	"os"
)

// jobQueue holds jobs of one kind, the songs downloading or going up to the
// cloud drive. It runs one at a time, in the order they were asked for,
// keeps those that failed with why, to try again, and is kept across
// launches. Like all of the app's state it is used on the main thread only:
// the job itself runs in a goroutine and hands its progress and its end
// back through post.
//
// K tells two jobs for the same song apart from the others: a song is
// queued once, and the window's rows are chosen by it.
type jobQueue[T any, K jobKey[T]] struct {
	waiting  []T
	running  *T
	done     int64 // how much of the running job is done, of size
	size     int64
	cancel   context.CancelFunc
	gen      int // bumped as a job starts or stops: a stale job's results are dropped
	failed   []failedJob[T]
	restored bool // the queue left at the last quit was picked up
}

// jobKey names a job's song, as a zero-size type so that a jobQueue works
// as it is declared.
type jobKey[T any] interface {
	key(T) string
}

// failedJob is a job that failed, and why.
type failedJob[T any] struct {
	item   T
	reason string
}

func (q *jobQueue[T, K]) key(it T) string {
	var k K
	return k.key(it)
}

// current is the job running, if one is.
func (q *jobQueue[T, K]) current() (T, bool) {
	if q.running == nil {
		var zero T
		return zero, false
	}
	return *q.running, true
}

// count is the jobs running and waiting, not those that failed.
func (q *jobQueue[T, K]) count() int {
	n := len(q.waiting)
	if q.running != nil {
		n++
	}
	return n
}

// pending tells whether the song of key is running or waiting.
func (q *jobQueue[T, K]) pending(key string) bool {
	if q.running != nil && q.key(*q.running) == key {
		return true
	}
	for _, it := range q.waiting {
		if q.key(it) == key {
			return true
		}
	}
	return false
}

// add queues it, unless its song is queued already. Asked again, a song
// that failed is tried again, not listed twice.
func (q *jobQueue[T, K]) add(it T) bool {
	key := q.key(it)
	if q.pending(key) {
		return false
	}
	q.dropFailed(key)
	q.waiting = append(q.waiting, it)
	return true
}

// next takes the first job waiting, while none runs.
func (q *jobQueue[T, K]) next() (T, bool) {
	if q.running != nil || len(q.waiting) == 0 {
		var zero T
		return zero, false
	}
	it := q.waiting[0]
	q.waiting = q.waiting[1:]
	return it, true
}

// run runs it as the job. work runs in a goroutine, with a context that
// stop cancels, and reports its progress; the func it returns runs on the
// main thread as it ends, unless the job was stopped meanwhile.
func (q *jobQueue[T, K]) run(it T, post func(func()), work func(ctx context.Context, progress func(done, size int64)) func()) {
	q.running = &it
	q.done, q.size = 0, 0
	q.gen++
	gen := q.gen
	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel
	go func() {
		end := work(ctx, func(done, size int64) {
			post(func() {
				if gen == q.gen && q.running != nil {
					q.done, q.size = done, size
				}
			})
		})
		post(func() {
			if gen == q.gen {
				end()
			}
		})
	}()
}

// finish takes the job that ran off the queue, as it ended.
func (q *jobQueue[T, K]) finish() T {
	var it T
	if q.running != nil {
		it = *q.running
	}
	q.running, q.cancel = nil, nil
	return it
}

// stop cancels the job running, if one is, and drops what it would still
// report.
func (q *jobQueue[T, K]) stop() bool {
	if q.running == nil {
		return false
	}
	if q.cancel != nil {
		q.cancel()
	}
	q.gen++
	q.running, q.cancel = nil, nil
	return true
}

// fail keeps it as failed, for reason, in place of an earlier failure of
// its song.
func (q *jobQueue[T, K]) fail(it T, reason string) {
	key := q.key(it)
	for i, f := range q.failed {
		if q.key(f.item) == key {
			q.failed[i] = failedJob[T]{it, reason}
			return
		}
	}
	q.failed = append(q.failed, failedJob[T]{it, reason})
}

// dropFailed forgets that the songs of keys failed, or all for none.
func (q *jobQueue[T, K]) dropFailed(keys ...string) {
	want := keySet(keys)
	kept := q.failed[:0]
	for _, f := range q.failed {
		if len(keys) > 0 && !want[q.key(f.item)] {
			kept = append(kept, f)
		}
	}
	q.failed = kept
}

// retry queues again the failed songs of keys, or all of them for none,
// and says how many were queued.
func (q *jobQueue[T, K]) retry(keys ...string) int {
	want := keySet(keys)
	var kept []failedJob[T]
	n := 0
	for _, f := range q.failed {
		if len(keys) > 0 && !want[q.key(f.item)] {
			kept = append(kept, f)
			continue
		}
		if !q.pending(q.key(f.item)) {
			q.waiting = append(q.waiting, f.item)
			n++
		}
	}
	q.failed = kept
	return n
}

// unqueue takes the song of key out of those waiting.
func (q *jobQueue[T, K]) unqueue(key string) (T, bool) {
	for i, it := range q.waiting {
		if q.key(it) == key {
			q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
			return it, true
		}
	}
	var zero T
	return zero, false
}

// How remove found the song it took out.
const (
	notQueued = iota
	wasRunning
	wasWaiting
	wasFailed
)

// remove takes the song of key out of those waiting or failed. A running
// one is left for the caller to stop, as it says.
func (q *jobQueue[T, K]) remove(key string) (T, int) {
	if q.running != nil && q.key(*q.running) == key {
		return *q.running, wasRunning
	}
	if it, ok := q.unqueue(key); ok {
		return it, wasWaiting
	}
	for _, f := range q.failed {
		if q.key(f.item) == key {
			q.dropFailed(key)
			return f.item, wasFailed
		}
	}
	var zero T
	return zero, notQueued
}

// clear forgets the songs waiting and failed, and says whether one runs:
// the caller stops it.
func (q *jobQueue[T, K]) clear() bool {
	q.waiting, q.failed = nil, nil
	return q.running != nil
}

// drop takes the chosen songs out of those waiting and those that failed,
// and says how many, and whether the running one was chosen too: the
// caller stops it.
func (q *jobQueue[T, K]) drop(chosen map[string]bool) (n int, running bool) {
	waiting := q.waiting[:0]
	for _, it := range q.waiting {
		if chosen[q.key(it)] {
			n++
		} else {
			waiting = append(waiting, it)
		}
	}
	q.waiting = waiting
	failed := q.failed[:0]
	for _, f := range q.failed {
		if chosen[q.key(f.item)] {
			n++
		} else {
			failed = append(failed, f)
		}
	}
	q.failed = failed
	return n, q.running != nil && chosen[q.key(*q.running)]
}

// kept is the queue as it is kept across launches: the running job first,
// as it is done again, then those waiting.
func (q *jobQueue[T, K]) kept() []T {
	var out []T
	if q.running != nil {
		out = append(out, *q.running)
	}
	return append(out, q.waiting...)
}

// restore picks up the jobs and failures kept at the last quit, leaving
// out songs queued since.
func (q *jobQueue[T, K]) restore(items []T, failed []failedJob[T]) {
	q.failed = failed
	for _, it := range items {
		q.add(it)
	}
}

// reset stops the job running and forgets the queue, to pick up another
// one kept.
func (q *jobQueue[T, K]) reset() {
	q.stop()
	q.waiting, q.failed = nil, nil
	q.restored = false
}

func keySet(keys []string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// queueFile is a queue as it is kept across launches: the jobs to do, in
// order, then those that failed, in each queue's own form.
type queueFile[T, F any] struct {
	Queue  []T `json:"queue"`
	Failed []F `json:"failed,omitempty"`
}

// saveQueueFile keeps a queue at path, or removes the file once it is
// empty.
func saveQueueFile[T, F any](path string, queue []T, failed []F) {
	if path == "" {
		return
	}
	if len(queue) == 0 && len(failed) == 0 {
		os.Remove(path)
		return
	}
	b, err := json.Marshal(queueFile[T, F]{queue, failed})
	if err != nil {
		return
	}
	writeFileAtomic(path, b, 0o600)
}

// readQueueFile reads a queue kept at path.
func readQueueFile[T, F any](path string) (queueFile[T, F], bool) {
	var q queueFile[T, F]
	if path == "" {
		return q, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return q, false
	}
	return q, json.Unmarshal(b, &q) == nil
}
