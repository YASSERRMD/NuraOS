package delta

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
)

// ApplyResult carries the reconstructed image and metadata.
type ApplyResult struct {
	Image []byte
	Stats ApplyStats
}

// ApplyStats records what happened during delta application.
type ApplyStats struct {
	CopiedBlocks int
	NewBlocks    int
	TotalBlocks  int
}

// Apply reads a delta from r and applies it against srcImage to reconstruct the
// target image. It verifies:
//  1. The delta source SHA-256 matches srcImage.
//  2. The reconstructed image SHA-256 matches the delta target SHA-256.
//
// If either check fails an error is returned with the appropriate sentinel.
func Apply(r io.Reader, srcImage []byte) (*ApplyResult, error) {
	// Read and validate header.
	hdr, err := readHeader(r)
	if err != nil {
		return nil, fmt.Errorf("read delta header: %w", err)
	}

	// Verify source image.
	srcSum := sha256.Sum256(srcImage)
	if srcSum != hdr.SrcSHA256 {
		return nil, ErrWrongSource
	}

	bs := int(hdr.BlockSize)
	srcBlocks := splitBlocks(srcImage, bs)

	// Apply operations.
	result := make([][]byte, 0, hdr.OpCount)
	var st ApplyStats
	for i := uint32(0); i < hdr.OpCount; i++ {
		op, err := readOp(r)
		if err != nil {
			return nil, fmt.Errorf("read op %d: %w", i, err)
		}
		switch op.Type {
		case OpCopy:
			if int(op.SrcIdx) >= len(srcBlocks) {
				return nil, fmt.Errorf("op %d: src block index %d out of range (%d blocks)",
					i, op.SrcIdx, len(srcBlocks))
			}
			result = append(result, srcBlocks[op.SrcIdx])
			st.CopiedBlocks++
		case OpData:
			// Pad to block size if needed.
			blk := make([]byte, bs)
			copy(blk, op.Data)
			result = append(result, blk)
			st.NewBlocks++
		}
	}
	st.TotalBlocks = len(result)

	// Concatenate blocks and trim to expected target length.
	var out []byte
	for _, blk := range result {
		out = append(out, blk...)
	}

	// Verify reconstructed image.
	dstSum := sha256.Sum256(out)
	if dstSum != hdr.DstSHA256 {
		return nil, ErrApplyFailed
	}

	return &ApplyResult{Image: out, Stats: st}, nil
}

func readHeader(r io.Reader) (*Header, error) {
	var hdr Header

	// magic[8]
	magicBuf := make([]byte, 8)
	if _, err := io.ReadFull(r, magicBuf); err != nil {
		return nil, err
	}
	if !bytes.Equal(magicBuf, []byte(magic)) {
		return nil, ErrBadMagic
	}
	copy(hdr.Magic[:], magicBuf)

	var err error
	if hdr.Version, err = readU16(r); err != nil {
		return nil, err
	}
	v, err := readU32(r)
	if err != nil {
		return nil, err
	}
	hdr.BlockSize = v

	if _, err := io.ReadFull(r, hdr.SrcSHA256[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, hdr.DstSHA256[:]); err != nil {
		return nil, err
	}
	ops, err := readU32(r)
	if err != nil {
		return nil, err
	}
	hdr.OpCount = ops

	return &hdr, nil
}

func readOp(r io.Reader) (Op, error) {
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, typeBuf); err != nil {
		return Op{}, err
	}
	opType := OpType(typeBuf[0])

	targetIdx, err := readU32(r)
	if err != nil {
		return Op{}, err
	}

	op := Op{Type: opType, TargetIdx: targetIdx}
	switch opType {
	case OpCopy:
		srcIdx, err := readU32(r)
		if err != nil {
			return Op{}, err
		}
		op.SrcIdx = srcIdx

	case OpData:
		length, err := readU32(r)
		if err != nil {
			return Op{}, err
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return Op{}, err
		}
		op.Data = data

	default:
		return Op{}, fmt.Errorf("unknown op type %d", opType)
	}

	return op, nil
}
