package trajectory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/agent"
	"os"
	"unicode/utf8"
)

// VisitRecords streams wire records without retaining an untyped service payload.
// Limit counts yielded records; filtering may scan the entire file, like Python.
// A visitor receives detached bytes, and no append lock spans the callback.
func (s *Store) VisitRecords(ctx context.Context, id agent.TrajectoryID, query agent.TrajectoryEventQuery, visit func([]byte) error) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if query.Limit <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	selected := make(map[string]bool, len(query.Types))
	for _, kind := range query.Types {
		selected[string(kind)] = true
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), MaxRecordBytes+1)
	yielded := 0
	for yielded < query.Limit && scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if !utf8.Valid(line) {
			return ErrInvalid
		}
		if !json.Valid(line) {
			continue
		}
		if query.Types != nil {
			var record struct{ Type *string }
			if json.Unmarshal(line, &record) != nil {
				return ErrInvalid
			}
			if record.Type == nil || !selected[*record.Type] {
				continue
			}
		}
		yielded++
		if err := visit(append([]byte(nil), line...)); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = scanner.Err()
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return nil
	}
	return err
}
