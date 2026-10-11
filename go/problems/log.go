// Package problems retains source-compatible bounded distinct diagnostics and
// exact lifetime counters. Snapshots detach all slices and integer storage.
package problems

import (
	"bytes"
	"errors"
	"math/big"
	"sync"
)

const MaxDistinctProblems = 50

var ErrEmptyEviction = errors.New("problem log cannot evict from an empty list")
var ErrCounter = errors.New("problem counter must be a nonnegative decimal integer")

// Counter is an immutable exact nonnegative integer; its zero value is zero.
type Counter struct{ value *big.Int }

func FromUint64(value uint64) Counter { return Counter{new(big.Int).SetUint64(value)} }
func FromBigInt(value *big.Int) (Counter, error) {
	if value == nil || value.Sign() < 0 {
		return Counter{}, ErrCounter
	}
	return Counter{new(big.Int).Set(value)}, nil
}
func (c Counter) BigInt() *big.Int {
	if c.value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(c.value)
}
func (c Counter) String() string {
	if c.value == nil {
		return "0"
	}
	return c.value.String()
}
func (c Counter) MarshalJSON() ([]byte, error) { return []byte(c.String()), nil }
func (c *Counter) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || (len(data) > 1 && data[0] == '0') {
		return ErrCounter
	}
	for _, character := range data {
		if character < '0' || character > '9' {
			return ErrCounter
		}
	}
	value, ok := new(big.Int).SetString(string(data), 10)
	if !ok {
		return ErrCounter
	}
	c.value = value
	return nil
}

type Entry struct {
	Text  string  `json:"text"`
	Count Counter `json:"count"`
}
type Snapshot struct {
	Entries  []Entry `json:"entries"`
	Total    Counter `json:"total"`
	Dropped  Counter `json:"dropped"`
	Limit    int     `json:"limit"`
	Churning bool    `json:"churning"`
}

func (s Snapshot) Messages() []string {
	result := make([]string, 0, len(s.Entries))
	for _, entry := range s.Entries {
		result = append(result, entry.Text)
	}
	return result
}
func (s Snapshot) Summary() []string {
	result := make([]string, 0, len(s.Entries))
	for _, entry := range s.Entries {
		value := entry.Text
		if entry.Count.BigInt().Cmp(big.NewInt(1)) > 0 {
			value += " (x" + entry.Count.String() + ")"
		}
		result = append(result, value)
	}
	return result
}

type Log struct {
	mu             sync.Mutex
	configured     bool
	limit          int
	order          []string
	counts         map[string]*big.Int
	total, dropped big.Int
}

// New preserves even nonpositive source limits. Append then reports the source
// empty-eviction failure after incrementing total. The zero Log uses limit 50.
func New(limit int) *Log { return &Log{configured: true, limit: limit} }
func (l *Log) capacity() int {
	if !l.configured {
		return MaxDistinctProblems
	}
	return l.limit
}
func (l *Log) Append(message string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total.Add(&l.total, big.NewInt(1))
	if count, ok := l.counts[message]; ok {
		count.Add(count, big.NewInt(1))
		return nil
	}
	if len(l.order) >= l.capacity() {
		if len(l.order) == 0 {
			return ErrEmptyEviction
		}
		delete(l.counts, l.order[0])
		copy(l.order, l.order[1:])
		l.order = l.order[:len(l.order)-1]
		l.dropped.Add(&l.dropped, big.NewInt(1))
	}
	if l.counts == nil {
		l.counts = make(map[string]*big.Int)
	}
	l.counts[message] = big.NewInt(1)
	l.order = append(l.order, message)
	return nil
}
func (l *Log) Extend(messages []string) error {
	for _, message := range messages {
		if err := l.Append(message); err != nil {
			return err
		}
	}
	return nil
}
func (l *Log) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.order = nil
	l.counts = nil
	l.total.SetInt64(0)
	l.dropped.SetInt64(0)
}
func (l *Log) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	total, _ := FromBigInt(&l.total)
	dropped, _ := FromBigInt(&l.dropped)
	result := Snapshot{Entries: make([]Entry, 0, len(l.order)), Total: total, Dropped: dropped, Limit: l.capacity(), Churning: l.dropped.Cmp(big.NewInt(int64(l.capacity()))) > 0}
	for _, message := range l.order {
		count, _ := FromBigInt(l.counts[message])
		result.Entries = append(result.Entries, Entry{message, count})
	}
	return result
}
