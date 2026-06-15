package update

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/yasserrmd/nuraos/services/internal/delta"
)

// ApplyDelta applies a delta file to the current inactive slot image to produce
// the new rootfs image, then hands off to Apply for verification and commit.
// If delta application fails (wrong source, bad checksum, corrupt delta), it
// falls back to applying the fullImage reader as a complete image.
//
// srcSlotPath is the path to the current inactive slot's rootfs image (the
// base for the delta). It must be the same image version the delta was generated
// against.
func ApplyDelta(
	deltaReader io.Reader,
	fullImageFallback io.Reader,
	srcSlotPath string,
	source string,
	expectedDstSHA string,
	sigBytes []byte,
	runningServices []string,
	opts Options,
) (*Transaction, error) {
	alog := NewAuditLog(opts.dataDir())

	// Read the current inactive-slot image (delta source).
	srcData, err := os.ReadFile(srcSlotPath)
	if err != nil {
		return nil, fmt.Errorf("read delta source image %s: %w", srcSlotPath, err)
	}

	// Read the entire delta into memory so we can retry with the fallback.
	deltaData, err := io.ReadAll(deltaReader)
	if err != nil {
		return nil, fmt.Errorf("read delta: %w", err)
	}

	res, applyErr := delta.Apply(bytes.NewReader(deltaData), srcData)
	if applyErr != nil {
		if fullImageFallback == nil {
			return nil, fmt.Errorf("delta application failed and no fallback provided: %w", applyErr)
		}
		alog.Log("", "delta.fallback", fmt.Sprintf("delta apply failed (%v); using full image", applyErr))
		return Apply(fullImageFallback, source, expectedDstSHA, sigBytes, runningServices, opts)
	}

	alog.Log("", "delta.applied",
		fmt.Sprintf("copied=%d new=%d savings=%.1f%%",
			res.Stats.CopiedBlocks, res.Stats.NewBlocks, savingsPct(res.Stats)))

	return Apply(bytes.NewReader(res.Image), source, expectedDstSHA, sigBytes, runningServices, opts)
}

func savingsPct(s delta.ApplyStats) float64 {
	if s.TotalBlocks == 0 {
		return 0
	}
	return float64(s.CopiedBlocks) / float64(s.TotalBlocks) * 100
}

// ErrNoFallback is returned when delta application fails and no full-image
// fallback was provided.
var ErrNoFallback = errors.New("delta failed and no full-image fallback")
