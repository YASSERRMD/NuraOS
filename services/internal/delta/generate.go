package delta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// Generate produces a binary delta from srcImage to dstImage and writes it to w.
// blockSize controls the granularity (0 means use the package default BlockSize).
// Model blobs are not part of rootfs images and are naturally excluded.
func Generate(w io.Writer, srcImage, dstImage []byte, blockSize int) (*Stats, error) {
	if blockSize <= 0 {
		blockSize = BlockSize
	}

	// Hash every source block for fast lookup.
	srcIdx := make(map[[32]byte]uint32)
	srcBlocks := splitBlocks(srcImage, blockSize)
	for i, blk := range srcBlocks {
		var h [32]byte
		copy(h[:], hashBlock(blk))
		srcIdx[h] = uint32(i)
	}

	dstBlocks := splitBlocks(dstImage, blockSize)

	// Build the operation list.
	ops := make([]Op, 0, len(dstBlocks))
	stats := &Stats{
		SourceBlocks:    len(srcBlocks),
		TargetBlocks:    len(dstBlocks),
		FullTargetBytes: int64(len(dstImage)),
	}

	for i, blk := range dstBlocks {
		var h [32]byte
		copy(h[:], hashBlock(blk))
		if si, ok := srcIdx[h]; ok {
			ops = append(ops, Op{Type: OpCopy, TargetIdx: uint32(i), SrcIdx: si})
			stats.CopiedBlocks++
		} else {
			ops = append(ops, Op{Type: OpData, TargetIdx: uint32(i), Data: blk})
			stats.NewBlocks++
			stats.NewDataBytes += int64(len(blk))
		}
	}

	// Compute header fields.
	var srcSum, dstSum [32]byte
	s := sha256.Sum256(srcImage)
	copy(srcSum[:], s[:])
	d := sha256.Sum256(dstImage)
	copy(dstSum[:], d[:])

	// Write header.
	if _, err := w.Write([]byte(magic)); err != nil {
		return nil, err
	}
	if err := writeU16(w, version); err != nil {
		return nil, err
	}
	if err := writeU32(w, uint32(blockSize)); err != nil {
		return nil, err
	}
	if _, err := w.Write(srcSum[:]); err != nil {
		return nil, err
	}
	if _, err := w.Write(dstSum[:]); err != nil {
		return nil, err
	}
	if err := writeU32(w, uint32(len(ops))); err != nil {
		return nil, err
	}

	// Write operations.
	for _, op := range ops {
		if err := writeOp(w, op); err != nil {
			return nil, err
		}
	}

	return stats, nil
}

func writeOp(w io.Writer, op Op) error {
	if _, err := w.Write([]byte{byte(op.Type)}); err != nil {
		return err
	}
	if err := writeU32(w, op.TargetIdx); err != nil {
		return err
	}
	switch op.Type {
	case OpCopy:
		return writeU32(w, op.SrcIdx)
	case OpData:
		if err := writeU32(w, uint32(len(op.Data))); err != nil {
			return err
		}
		_, err := w.Write(op.Data)
		return err
	}
	return nil
}

// splitBlocks divides data into blocks of size bs.
// The last block is zero-padded to bs if data is not a multiple of bs.
func splitBlocks(data []byte, bs int) [][]byte {
	var blocks [][]byte
	for i := 0; i < len(data); i += bs {
		end := i + bs
		if end > len(data) {
			// Pad the last block.
			padded := make([]byte, bs)
			copy(padded, data[i:])
			blocks = append(blocks, padded)
		} else {
			blk := make([]byte, bs)
			copy(blk, data[i:end])
			blocks = append(blocks, blk)
		}
	}
	return blocks
}

func hashBlock(blk []byte) []byte {
	h := sha256.Sum256(blk)
	return h[:]
}

// hexSum returns the hex SHA-256 of data.
func hexSum(data []byte) string {
	return hex.EncodeToString(hashBlock(data))
}

// blocksEqual returns true if two block slices contain the same content.
func blocksEqual(a, b []byte) bool {
	return bytes.Equal(a, b)
}
