package efficiency

import (
	"errors"
	"fmt"
	"math/big"
)

const MaxRecoveryBytes = 50_000

// RecoveryPage contains only a scoped ref and character pagination. Nil numeric
// fields select Source defaults; JSON null validation belongs to the tool boundary.
type RecoveryPage struct {
	Ref           string
	Offset, Limit *big.Int
}

// ReadMaskedPage retrieves a bounded page of already-masked text. Header and
// continuation instructions count toward the UTF-8 byte cap.
func ReadMaskedPage(store *RawStore, page RecoveryPage) (string, error) {
	offset, limit := page.Offset, page.Limit
	if offset == nil {
		offset = big.NewInt(0)
	}
	if limit == nil {
		limit = big.NewInt(MaxRecoveryBytes)
	}
	if offset.Sign() < 0 {
		return "Error: offset must be a non-negative integer", nil
	}
	if limit.Sign() <= 0 {
		return "Error: limit must be a positive integer", nil
	}
	if store == nil {
		return "Error: masked raw artifact recovery is not enabled for this session", nil
	}
	content, err := store.GetMasked(page.Ref)
	if err != nil {
		if errors.Is(err, ErrArtifactStore) {
			return "Error: " + err.Error(), nil
		}
		return "", err
	}
	// RawStore admits strict UTF-8. Indices map Python character offsets to byte
	// offsets once; binary-search probes then avoid copying entire rune slices.
	indices := make([]int, 0, len(content)+1)
	for i := range content {
		indices = append(indices, i)
	}
	indices = append(indices, len(content))
	total := len(indices) - 1
	start := total
	if offset.Cmp(new(big.Int).SetUint64(uint64(total))) < 0 {
		start = int(offset.Int64())
	}
	count := MaxRecoveryBytes
	if limit.Cmp(big.NewInt(MaxRecoveryBytes)) < 0 {
		count = int(limit.Int64())
	}
	maximum := total
	if count < total-start {
		maximum = start + count
	}
	render := func(end int) string {
		continuation := ""
		if end < total {
			continuation = fmt.Sprintf("; continue with offset=%d", end)
		}
		return fmt.Sprintf("[masked raw artifact; chars=%d:%d/%d%s]\n%s", start, end, total, continuation, content[indices[start]:indices[end]])
	}
	low, high, end := start, maximum, start
	for low <= high {
		candidate := (low + high) / 2
		if len(render(candidate)) <= MaxRecoveryBytes {
			end = candidate
			low = candidate + 1
		} else {
			high = candidate - 1
		}
	}
	return render(end), nil
}
