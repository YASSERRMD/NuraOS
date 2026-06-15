package delta_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/delta"
)

const testBlockSize = 512

func makeImage(blocks int, seed byte) []byte {
	img := make([]byte, blocks*testBlockSize)
	for i := range img {
		img[i] = seed + byte(i/testBlockSize)
	}
	return img
}

func TestRoundTripIdentical(t *testing.T) {
	src := makeImage(8, 0xAA)
	dst := src // identical

	var buf bytes.Buffer
	stats, err := delta.Generate(&buf, src, dst, testBlockSize)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if stats.CopiedBlocks != 8 {
		t.Errorf("CopiedBlocks = %d; want 8 (all blocks unchanged)", stats.CopiedBlocks)
	}
	if stats.NewBlocks != 0 {
		t.Errorf("NewBlocks = %d; want 0", stats.NewBlocks)
	}

	res, err := delta.Apply(&buf, src)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !bytes.Equal(res.Image, dst) {
		t.Error("reconstructed image does not match target")
	}
}

func TestRoundTripPartialChange(t *testing.T) {
	src := makeImage(8, 0xAA)
	dst := make([]byte, len(src))
	copy(dst, src)
	// Change blocks 2 and 5.
	for i := 2 * testBlockSize; i < 3*testBlockSize; i++ {
		dst[i] = 0xFF
	}
	for i := 5 * testBlockSize; i < 6*testBlockSize; i++ {
		dst[i] = 0x00
	}

	var buf bytes.Buffer
	stats, err := delta.Generate(&buf, src, dst, testBlockSize)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if stats.NewBlocks != 2 {
		t.Errorf("NewBlocks = %d; want 2 (blocks 2 and 5 changed)", stats.NewBlocks)
	}
	if stats.CopiedBlocks != 6 {
		t.Errorf("CopiedBlocks = %d; want 6", stats.CopiedBlocks)
	}

	res, err := delta.Apply(&buf, src)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !bytes.Equal(res.Image, dst) {
		t.Error("reconstructed image does not match target")
	}
}

func TestApplyWrongSource(t *testing.T) {
	src := makeImage(4, 0x11)
	dst := makeImage(4, 0x22)

	var buf bytes.Buffer
	if _, err := delta.Generate(&buf, src, dst, testBlockSize); err != nil {
		t.Fatal(err)
	}

	wrongSrc := makeImage(4, 0x33)
	_, err := delta.Apply(&buf, wrongSrc)
	if err == nil {
		t.Fatal("expected error with wrong source image, got nil")
	}
}

func TestApplyBadMagic(t *testing.T) {
	src := makeImage(2, 0x00)
	bad := []byte("notadelta!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")

	_, err := delta.Apply(bytes.NewReader(bad), src)
	if err == nil {
		t.Fatal("expected error with bad magic, got nil")
	}
}

func TestSavingsPct(t *testing.T) {
	src := makeImage(10, 0xAA)
	dst := make([]byte, len(src))
	copy(dst, src)
	// Change 2 of 10 blocks.
	for i := 0; i < testBlockSize; i++ {
		dst[i] = 0xFF       // block 0 changed
		dst[testBlockSize+i] = 0xEE // block 1 changed
	}

	var buf bytes.Buffer
	stats, err := delta.Generate(&buf, src, dst, testBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	pct := stats.SavingsPct()
	// 8 of 10 blocks are copied (no new bytes), so savings should be > 0.
	if pct <= 0 {
		t.Errorf("SavingsPct = %.1f; want > 0", pct)
	}
	t.Logf("bandwidth savings: %.1f%% (%d new bytes vs %d full target bytes)",
		pct, stats.NewDataBytes, stats.FullTargetBytes)
}

func TestSHA256MatchesTarget(t *testing.T) {
	src := makeImage(4, 0x01)
	dst := makeImage(4, 0x02)

	var buf bytes.Buffer
	if _, err := delta.Generate(&buf, src, dst, testBlockSize); err != nil {
		t.Fatal(err)
	}
	res, err := delta.Apply(&buf, src)
	if err != nil {
		t.Fatal(err)
	}
	gotSum := sha256.Sum256(res.Image)
	wantSum := sha256.Sum256(dst)
	if gotSum != wantSum {
		t.Error("SHA-256 of reconstructed image does not match target")
	}
}
