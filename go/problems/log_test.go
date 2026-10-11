package problems

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"sync"
	"testing"
)

func TestActualPythonProblemLog(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-problem-log.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string
			Limit int
			Steps []struct {
				Operation string
				Messages  []string
				State     Snapshot
				Summary   []string
				Error     *string
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 9 {
		t.Fatal("source problem-log inventory drift")
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			log := New(test.Limit)
			for i, step := range test.Steps {
				var err error
				switch step.Operation {
				case "append":
					err = log.Append(step.Messages[0])
				case "extend":
					err = log.Extend(step.Messages)
				case "clear":
					log.Clear()
				case "snapshot":
				default:
					t.Fatal("unknown source operation")
				}
				if step.Error != nil {
					if *step.Error != "IndexError" || !errors.Is(err, ErrEmptyEviction) {
						t.Fatalf("step %d error %v vs %s", i, err, *step.Error)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				got, _ := json.Marshal(log.Snapshot())
				want, _ := json.Marshal(step.State)
				if string(got) != string(want) || !reflect.DeepEqual(log.Snapshot().Summary(), step.Summary) {
					t.Fatalf("step %d\ngot %s\nwant %s", i, got, want)
				}
			}
		})
	}
}
func TestConcurrentExactSnapshotsAndDetachment(t *testing.T) {
	var log Log
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 100; n++ {
				if err := log.Append("same"); err != nil {
					t.Error(err)
				}
				snapshot := log.Snapshot()
				snapshot.Entries[0].Text = "changed"
				snapshot.Total.BigInt().SetInt64(-1)
			}
		}()
	}
	workers.Wait()
	snapshot := log.Snapshot()
	if snapshot.Total.String() != "1600" || snapshot.Entries[0].Count.String() != "1600" || snapshot.Entries[0].Text != "same" || snapshot.Limit != 50 {
		t.Fatal(snapshot)
	}
	original := new(big.Int).Lsh(big.NewInt(1), 100)
	counter, err := FromBigInt(original)
	if err != nil {
		t.Fatal(err)
	}
	original.SetInt64(0)
	encoded, _ := json.Marshal(counter)
	var decoded Counter
	if err = json.Unmarshal(encoded, &decoded); err != nil || decoded.String() != counter.String() {
		t.Fatal("exact counter roundtrip")
	}
	before := decoded.String()
	for _, input := range []string{`-1`, `true`, `1.0`, `1e3`, `null`, `"1"`} {
		if err = json.Unmarshal([]byte(input), &decoded); err == nil || decoded.String() != before {
			t.Fatal("invalid counter mutated state", input)
		}
	}
	if _, err := FromBigInt(nil); !errors.Is(err, ErrCounter) {
		t.Fatal("nil admitted")
	}
	if _, err := FromBigInt(big.NewInt(-1)); !errors.Is(err, ErrCounter) {
		t.Fatal("negative admitted")
	}
	if (Counter{}).String() != "0" || FromUint64(42).String() != "42" {
		t.Fatal("counter defaults")
	}
}
