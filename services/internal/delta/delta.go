// Package delta implements block-level binary delta generation and application
// for NuraOS rootfs images.
//
// A delta (.nudelta) describes the differences between a known source image
// and a target image as a sequence of operations:
//
//   - OpCopy: reuse a block from the source image unchanged.
//   - OpData: write new block content that differs from the source.
//
// Delta generation hashes every block of the source image and matches target
// blocks against that index. Unchanged blocks become cheap COPY operations;
// only changed blocks are stored in full. Model blob partitions (/data) are
// not part of the OS rootfs image and are excluded automatically.
//
// Wire format (binary, big-endian):
//
//	Header (82 bytes):
//	  magic[8]      "NURADELT"
//	  version[2]    1
//	  block_size[4] block size in bytes (default 4096)
//	  src_sha256[32]
//	  dst_sha256[32]
//	  op_count[4]
//
//	Operations (variable):
//	  type[1]       0=copy  1=data
//	  target_idx[4] target block index
//	  if copy: src_idx[4]
//	  if data: length[4] + data[length]
package delta

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
)

const (
	magic     = "NURADELT"
	version   = uint16(1)
	BlockSize = 4096
)

// OpType is the type of a delta operation.
type OpType uint8

const (
	OpCopy OpType = 0
	OpData OpType = 1
)

// Header is the fixed-size delta file header.
type Header struct {
	Magic     [8]byte
	Version   uint16
	BlockSize uint32
	SrcSHA256 [32]byte
	DstSHA256 [32]byte
	OpCount   uint32
}

// Op is one delta operation.
type Op struct {
	Type      OpType
	TargetIdx uint32
	SrcIdx    uint32 // OpCopy only
	Data      []byte // OpData only
}

// Stats reports bandwidth efficiency of a delta.
type Stats struct {
	SourceBlocks int
	TargetBlocks int
	CopiedBlocks int
	NewBlocks    int
	// NewDataBytes is the total bytes stored as OpData (the delta payload size excluding header).
	NewDataBytes int64
	// FullTargetBytes is the uncompressed target image size.
	FullTargetBytes int64
}

// SavingsPct returns the percentage of target bytes avoided by the delta.
// Returns 0 if FullTargetBytes is 0.
func (s Stats) SavingsPct() float64 {
	if s.FullTargetBytes == 0 {
		return 0
	}
	return (1 - float64(s.NewDataBytes)/float64(s.FullTargetBytes)) * 100
}

// ErrBadMagic is returned when the delta file has an unrecognised magic bytes.
var ErrBadMagic = errors.New("not a NuraOS delta file")

// ErrWrongSource is returned when the delta's source SHA-256 does not match.
var ErrWrongSource = errors.New("delta source SHA-256 does not match current image")

// ErrApplyFailed is returned when the reconstructed image SHA-256 does not match.
var ErrApplyFailed = errors.New("reconstructed image SHA-256 mismatch; delta application failed")

// sha256Hex returns the lowercase hex SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sha256Reader returns the SHA-256 of all data read from r and the number of bytes.
func sha256Reader(r io.Reader) ([32]byte, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, n, err
}

func writeU16(w io.Writer, v uint16) error {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func writeU32(w io.Writer, v uint32) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func readU16(r io.Reader) (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func readU32(r io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}
