package index

import (
	"errors"
	"io"
	"math/rand"
	"testing"
)

func TestVByteRoundtrip(t *testing.T) {
	testValues := []uint64{
		0, 1, 63, 127, 128, 255, 256, 16383, 16384,
		1<<20 - 1, 1 << 20, 1<<31 - 1, 1 << 31,
		1<<62 - 1, 1<<63 - 1, ^uint64(0),
	}

	for _, val := range testValues {
		buf := AppendVByte(nil, val)
		decoded, n, err := DecodeVByte(buf)
		if err != nil {
			t.Fatalf("DecodeVByte failed for %d: %v", val, err)
		}
		if n != len(buf) {
			t.Fatalf("expected bytes read %d, got %d for %d", len(buf), n, val)
		}
		if decoded != val {
			t.Fatalf("expected %d, got %d", val, decoded)
		}
	}
}

func TestVByte_OverflowProtection(t *testing.T) {
	// 11 bytes all with continuation bit set
	badStream := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	_, _, err := DecodeVByte(badStream)
	if !errors.Is(err, ErrVarintOverflow) {
		t.Fatalf("expected ErrVarintOverflow, got: %v", err)
	}

	// 10th byte exceeding 0x01
	bad10thByte := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x02}
	_, _, err = DecodeVByte(bad10thByte)
	if !errors.Is(err, ErrVarintOverflow) {
		t.Fatalf("expected ErrVarintOverflow for bad 10th byte, got: %v", err)
	}
}

func TestVByte_TruncatedEOF(t *testing.T) {
	// Byte indicates continuation, but buffer ends
	truncated := []byte{0x85}
	_, _, err := DecodeVByte(truncated)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected ErrUnexpectedEOF, got: %v", err)
	}

	// Empty slice
	_, _, err = DecodeVByte([]byte{})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected ErrUnexpectedEOF on empty slice, got: %v", err)
	}
}

func TestPostingBlockEncodeDecode(t *testing.T) {
	docIDs := make([]uint32, 64)
	tfs := make([]uint32, 64)

	var curID uint32 = 100
	for i := 0; i < 64; i++ {
		curID += uint32(rand.Intn(10) + 1)
		docIDs[i] = curID
		tfs[i] = uint32(rand.Intn(5) + 1)
	}

	docData, tfData, err := EncodePostingBlock(docIDs, tfs)
	if err != nil {
		t.Fatalf("EncodePostingBlock failed: %v", err)
	}

	// Compression ratio calculation
	rawDocBytes := 64 * 8 // 64 * 8 bytes for uncompressed int64
	rawTFBytes := 64 * 8
	compressedTotal := len(docData) + len(tfData)
	savingsPct := (1.0 - float64(compressedTotal)/float64(rawDocBytes+rawTFBytes)) * 100.0
	t.Logf("Compressed 64 postings: Raw = %d bytes, Compressed = %d bytes (%.1f%% RAM reduction)",
		rawDocBytes+rawTFBytes, compressedTotal, savingsPct)

	if savingsPct < 70.0 {
		t.Errorf("expected >70%% savings, got %.1f%%", savingsPct)
	}

	var outDocIDs [64]uint32
	var outTFs [64]uint32

	n, err := DecodePostingBlock(docData, tfData, &outDocIDs, &outTFs)
	if err != nil {
		t.Fatalf("DecodePostingBlock failed: %v", err)
	}
	if n != 64 {
		t.Fatalf("expected 64 decoded postings, got %d", n)
	}

	for i := 0; i < 64; i++ {
		if outDocIDs[i] != docIDs[i] {
			t.Fatalf("mismatch at docID index %d: orig %d vs decoded %d", i, docIDs[i], outDocIDs[i])
		}
		if outTFs[i] != tfs[i] {
			t.Fatalf("mismatch at tf index %d: orig %d vs decoded %d", i, tfs[i], outTFs[i])
		}
	}
}

func TestPostingBlock_UnsortedRejection(t *testing.T) {
	unsortedIDs := []uint32{10, 5, 20}
	tfs := []uint32{1, 1, 1}

	_, _, err := EncodePostingBlock(unsortedIDs, tfs)
	if !errors.Is(err, ErrUnsortedDocIDs) {
		t.Fatalf("expected ErrUnsortedDocIDs for unsorted slice, got: %v", err)
	}

	duplicateIDs := []uint32{10, 10, 20}
	_, _, err = EncodePostingBlock(duplicateIDs, tfs)
	if !errors.Is(err, ErrUnsortedDocIDs) {
		t.Fatalf("expected ErrUnsortedDocIDs for duplicate slice, got: %v", err)
	}
}
