package index

import (
	"errors"
	"io"
)

var (
	// ErrVarintOverflow is returned when a VByte stream exceeds 10 bytes or has an invalid 10th byte.
	ErrVarintOverflow = errors.New("vbyte: varint integer overflow (> 64 bits or invalid continuation)")
	// ErrUnexpectedEOF is returned when a VByte stream terminates before the continuation bit is cleared.
	ErrUnexpectedEOF = io.ErrUnexpectedEOF
	// ErrUnsortedDocIDs is returned when document IDs are not strictly monotonically increasing.
	ErrUnsortedDocIDs = errors.New("vbyte: document IDs must be strictly monotonically increasing")
)

// AppendVByte appends the VByte (LEB128) encoding of x to dst and returns the extended slice.
func AppendVByte(dst []byte, x uint64) []byte {
	for x >= 0x80 {
		dst = append(dst, byte(x)|0x80)
		x >>= 7
	}
	return append(dst, byte(x))
}

// DecodeVByte decodes a single VByte integer from src with 10-byte overflow protection.
func DecodeVByte(src []byte) (val uint64, bytesRead int, err error) {
	if len(src) == 0 {
		return 0, 0, ErrUnexpectedEOF
	}
	var x uint64
	var shift uint
	for i, b := range src {
		if i >= 10 || (i == 9 && b > 1) {
			return 0, 0, ErrVarintOverflow
		}
		if b < 0x80 {
			x |= uint64(b) << shift
			return x, i + 1, nil
		}
		x |= uint64(b&0x7F) << shift
		shift += 7
	}
	return 0, 0, ErrUnexpectedEOF
}

// EncodePostingBlock compresses up to 64 strictly sorted docIDs and positive TFs into byte buffers.
func EncodePostingBlock(docIDs []uint32, tfs []uint32) (docData []byte, tfData []byte, err error) {
	if len(docIDs) != len(tfs) {
		return nil, nil, errors.New("vbyte: docIDs and tfs length mismatch")
	}
	if len(docIDs) == 0 {
		return nil, nil, nil
	}

	docData = make([]byte, 0, len(docIDs)*2)
	tfData = make([]byte, 0, len(tfs))

	var lastID uint32
	for i, docID := range docIDs {
		if i > 0 && docID <= lastID {
			return nil, nil, ErrUnsortedDocIDs
		}
		delta := docID - lastID
		docData = AppendVByte(docData, uint64(delta))
		tfData = AppendVByte(tfData, uint64(tfs[i]))
		lastID = docID
	}
	return docData, tfData, nil
}

// DecodePostingBlock decompresses byte buffers directly into fixed 64-element arrays.
func DecodePostingBlock(docData, tfData []byte, outDocIDs *[64]uint32, outTFs *[64]uint32) (int, error) {
	if len(docData) == 0 {
		return 0, nil
	}

	docOffset := 0
	tfOffset := 0
	count := 0
	var lastDocID uint32

	for docOffset < len(docData) && count < 64 {
		delta, nDoc, err := DecodeVByte(docData[docOffset:])
		if err != nil {
			return count, err
		}
		docOffset += nDoc

		if tfOffset >= len(tfData) {
			return count, ErrUnexpectedEOF
		}
		tf, nTF, err := DecodeVByte(tfData[tfOffset:])
		if err != nil {
			return count, err
		}
		tfOffset += nTF

		lastDocID += uint32(delta)
		outDocIDs[count] = lastDocID
		outTFs[count] = uint32(tf)
		count++
	}

	return count, nil
}
