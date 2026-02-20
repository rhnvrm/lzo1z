package lzo1z

import (
	"bytes"
	"testing"
)

// FuzzRoundtrip tests that any input can be compressed and decompressed
// back to the original.
func FuzzRoundtrip(f *testing.F) {
	// Seed with various inputs
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte("Hello, World!"))
	f.Add([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	f.Add([]byte("ABCDABCDABCDABCDABCDABCDABCDABCDABCDABCD"))
	f.Add(bytes.Repeat([]byte{0xff}, 100))
	f.Add(bytes.Repeat([]byte("The quick brown fox. "), 10))

	// Sequential bytes
	seq := make([]byte, 256)
	for i := range seq {
		seq[i] = byte(i)
	}
	f.Add(seq)

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 64*1024 {
			// Skip very large inputs for speed
			return
		}

		// Compress
		compBuf := make([]byte, MaxCompressedSize(len(input)))
		compLen, err := Compress(input, compBuf)
		if err != nil {
			t.Fatalf("Compress failed: %v", err)
		}

		// Decompress
		decompBuf := make([]byte, len(input)+100)
		decompLen, err := Decompress(compBuf[:compLen], decompBuf)
		if err != nil {
			t.Fatalf("Decompress failed: %v", err)
		}

		// Verify
		if !bytes.Equal(input, decompBuf[:decompLen]) {
			t.Errorf("Roundtrip mismatch: input len=%d, output len=%d", len(input), decompLen)
		}
	})
}

// FuzzDecompress tests that the decompressor handles arbitrary input
// without panicking (may return errors, which is fine).
func FuzzDecompress(f *testing.F) {
	// Seed with valid compressed data
	f.Add([]byte{0x11, 0x00, 0x00})                                     // Empty
	f.Add([]byte{0x12, 0x41, 0x11, 0x00, 0x00})                         // Single literal
	f.Add([]byte{0x12, 0x41, 0x20, 0x06, 0x00, 0x00, 0x11, 0x00, 0x00}) // With match

	// Invalid/malformed data
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xff, 0xff, 0xff})
	f.Add([]byte{0x20})             // Truncated M3
	f.Add([]byte{0x11, 0x00})       // Truncated EOF
	f.Add([]byte{0x40, 0x00})       // M2 with zero offset
	f.Add([]byte{0x10, 0x00, 0x00}) // M4 EOF marker

	f.Fuzz(func(t *testing.T, input []byte) {
		// Just ensure no panic - errors are expected for random input
		output := make([]byte, 64*1024)
		_, _ = Decompress(input, output)
	})
}
