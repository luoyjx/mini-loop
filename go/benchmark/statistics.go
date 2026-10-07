package benchmark

import (
	"errors"
	"math"
	"math/big"
	"sort"
)

var ErrDifferentTasks = errors.New("arms ran different task sets; nothing is comparable")

// AggregateRuns preserves first-seen task order and counts every supplied row,
// including duplicate task rows, as Python does. A tied pass vote fails.
func AggregateRuns(runs [][]TaskResult) ([]AggregateResult, error) {
	byTask := make(map[string][]TaskResult)
	order := []string{}
	for _, run := range runs {
		for _, row := range run {
			if _, exists := byTask[row.Task]; !exists {
				order = append(order, row.Task)
			}
			byTask[row.Task] = append(byTask[row.Task], row)
		}
	}
	out := make([]AggregateResult, 0, len(order))
	for _, task := range order {
		rows := byTask[task]
		passes := 0
		result := AggregateResult{TaskResult: TaskResult{Arm: rows[0].Arm, Task: task}, Repeats: len(rows)}
		for _, row := range rows {
			if row.Passed {
				passes++
			}
			if result.Error == nil && row.Error != nil && *row.Error != "" {
				text := *row.Error
				result.Error = &text
			}
		}
		result.Passed = passes > len(rows)/2
		result.PassRate, _ = Float(roundDecimal(float64(passes)/float64(len(rows)), 3))
		for _, dim := range dimensions {
			values := []Number{}
			for _, row := range rows {
				if n := *row.measurement(dim); n != nil {
					values = append(values, *n)
				}
			}
			if len(values) == 0 {
				continue
			}
			sort.SliceStable(values, func(i, j int) bool { return values[i].rational().Cmp(values[j].rational()) < 0 })
			median := values[len(values)/2]
			if len(values)%2 == 0 {
				var err error
				median, err = midpoint(values[len(values)/2-1], median)
				if err != nil {
					return nil, err
				}
			}
			*result.measurement(dim) = &median
		}
		out = append(out, result)
	}
	return out, nil
}

// Compare pairs the last row for each task for wins/regressions. Totals still
// include every row. Performance warnings inform a human; they do not alter verdict.
func Compare(baseline, candidate []TaskResult) (Comparison, error) {
	base, cand := make(map[string]bool), make(map[string]bool)
	out := Comparison{Wins: []string{}, Regressions: []string{}, Dimensions: make(map[Dimension]DimensionChange), DimensionWarnings: []string{}, Verdict: NotWorse}
	for _, row := range baseline {
		base[row.Task] = row.Passed
		if row.Passed {
			out.BaselinePassed++
		}
	}
	for _, row := range candidate {
		cand[row.Task] = row.Passed
		if row.Passed {
			out.CandidatePassed++
		}
	}
	if len(base) != len(cand) {
		return Comparison{}, ErrDifferentTasks
	}
	names := make([]string, 0, len(base))
	for name := range base {
		if _, ok := cand[name]; !ok {
			return Comparison{}, ErrDifferentTasks
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out.Tasks = len(names)
	for _, name := range names {
		if base[name] && !cand[name] {
			out.Regressions = append(out.Regressions, name)
		}
		if !base[name] && cand[name] {
			out.Wins = append(out.Wins, name)
		}
	}
	if len(out.Regressions) > 0 {
		out.Verdict = Regression
	} else if len(out.Wins) > 0 {
		out.Verdict = Improvement
	}
	for _, dim := range dimensions {
		b, err := total(baseline, dim)
		if err != nil {
			return Comparison{}, err
		}
		c, err := total(candidate, dim)
		if err != nil {
			return Comparison{}, err
		}
		if b.Float64() <= 0 && c.Float64() <= 0 {
			continue
		}
		change := DimensionChange{Baseline: b, Candidate: c}
		if b.Float64() > 0 {
			var diff float64
			if b.kind != floatingNumber && c.kind != floatingNumber {
				diff, _ = new(big.Int).Sub(c.exactInteger(), b.exactInteger()).Float64()
			} else {
				diff = c.Float64() - b.Float64()
			}
			if math.IsInf(diff, 0) || math.IsInf(b.Float64(), 0) {
				return Comparison{}, ErrMetric
			}
			percent := diff * 100 / b.Float64()
			if math.IsInf(percent, 0) || math.IsNaN(percent) {
				return Comparison{}, ErrMetric
			}
			percent = roundDecimal(percent, 1)
			number, _ := Float(percent)
			change.DeltaPercent = &number
			if percent > DimensionWarnPercent {
				out.DimensionWarnings = append(out.DimensionWarnings, string(dim)+" worsened "+floatText(percent)+"%")
			}
		}
		out.Dimensions[dim] = change
	}
	return out, nil
}

func total(rows []TaskResult, dim Dimension) (Number, error) {
	total := Integer(0)
	for _, row := range rows {
		if n := *row.measurement(dim); n != nil {
			// Python's `value or 0` replaces floating signed zero with integer zero.
			if n.Float64() == 0 {
				continue
			}
			var err error
			total, err = add(total, *n)
			if err != nil {
				return Number{}, err
			}
		}
	}
	return total, nil
}
