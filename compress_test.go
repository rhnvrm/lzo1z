package lzo1z

import (
	"bytes"
	"testing"
)

func TestCompressDecompressRoundtrip(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"single_byte", []byte{0x41}},
		{"two_bytes", []byte{0x41, 0x42}},
		{"three_bytes", []byte{0x41, 0x42, 0x43}},
		{"hello", []byte("Hello, World!")},
		{"repeated_A", bytes.Repeat([]byte("A"), 40)},
		{"repeated_B", bytes.Repeat([]byte("B"), 100)},
		{"repeated_ABCD", bytes.Repeat([]byte("ABCD"), 100)},
		{"sequential", func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
		{"hello_x3", []byte("Hello, World! Hello, World! Hello, World!")},
		{"sentence", bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. "), 50)},
		{"zeros", make([]byte, 1000)},
		{"ones", bytes.Repeat([]byte{0xff}, 1000)},
		{"alternating", func() []byte {
			b := make([]byte, 500)
			for i := range b {
				if i%2 == 0 {
					b[i] = 0x00
				} else {
					b[i] = 0xff
				}
			}
			return b
		}()},
		{"large_16kb", bytes.Repeat([]byte("Lorem ipsum dolor sit amet, consectetur adipiscing elit. "), 300)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Compress
			compBuf := make([]byte, MaxCompressedSize(len(tc.input)))
			compLen, err := Compress(tc.input, compBuf)
			if err != nil {
				t.Fatalf("Compress failed: %v", err)
			}
			compressed := compBuf[:compLen]

			// Decompress
			decompBuf := make([]byte, len(tc.input)+100)
			decompLen, err := Decompress(compressed, decompBuf)
			if err != nil {
				t.Fatalf("Decompress failed: %v", err)
			}
			decompressed := decompBuf[:decompLen]

			// Verify
			if !bytes.Equal(tc.input, decompressed) {
				t.Errorf("Roundtrip failed:\ninput:  %d bytes\noutput: %d bytes", len(tc.input), len(decompressed))
				if len(tc.input) < 100 && len(decompressed) < 100 {
					t.Errorf("input:  %v\noutput: %v", tc.input, decompressed)
				}
			}

			// Log compression ratio
			if len(tc.input) > 0 {
				ratio := float64(compLen) / float64(len(tc.input)) * 100
				t.Logf("%s: %d -> %d bytes (%.1f%%)", tc.name, len(tc.input), compLen, ratio)
			}
		})
	}
}

func TestCompressEmpty(t *testing.T) {
	dst := make([]byte, 10)
	n, err := Compress(nil, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes, got %d", n)
	}
}

func TestMaxCompressedSize(t *testing.T) {
	tests := []struct {
		inputLen int
	}{
		{0},
		{1},
		{100},
		{1000},
		{10000},
	}

	for _, tc := range tests {
		maxSize := MaxCompressedSize(tc.inputLen)
		if maxSize < tc.inputLen {
			t.Errorf("MaxCompressedSize(%d) = %d, should be >= input", tc.inputLen, maxSize)
		}
	}
}

func BenchmarkCompress(b *testing.B) {
	// Test with compressible data
	input := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. "), 100)
	dst := make([]byte, MaxCompressedSize(len(input)))

	b.ResetTimer()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		_, _ = Compress(input, dst)
	}
}

func BenchmarkCompressIncompressible(b *testing.B) {
	// Test with random-like data (sequential bytes)
	input := make([]byte, 4096)
	for i := range input {
		input[i] = byte(i * 7)
	}
	dst := make([]byte, MaxCompressedSize(len(input)))

	b.ResetTimer()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		_, _ = Compress(input, dst)
	}
}

func TestCompressMediumOffset(t *testing.T) {
	// Test with offsets that fit in M3 range (up to 16384)
	input := make([]byte, 10000)

	// Fill with pattern at start
	copy(input[0:100], bytes.Repeat([]byte("ABCD"), 25))

	// Fill middle with different data
	for i := 100; i < 8000; i++ {
		input[i] = byte(i % 256)
	}

	// Repeat the pattern - offset ~8000 which is within M3 range
	copy(input[8000:8100], bytes.Repeat([]byte("ABCD"), 25))

	// Fill rest
	for i := 8100; i < 10000; i++ {
		input[i] = byte(i % 256)
	}

	// Compress and decompress
	compBuf := make([]byte, MaxCompressedSize(len(input)))
	compLen, err := Compress(input, compBuf)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	decompBuf := make([]byte, len(input)+100)
	decompLen, err := Decompress(compBuf[:compLen], decompBuf)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(input, decompBuf[:decompLen]) {
		t.Errorf("Roundtrip failed for medium offset test")
	}

	t.Logf("Medium offset test: %d -> %d bytes", len(input), compLen)
}

func TestCompressExtendedLength(t *testing.T) {
	// Create data that requires extended length encoding (very long matches)
	// M3 can encode up to 33 bytes normally, longer needs extension
	input := bytes.Repeat([]byte("X"), 500)

	compBuf := make([]byte, MaxCompressedSize(len(input)))
	compLen, err := Compress(input, compBuf)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	decompBuf := make([]byte, len(input)+100)
	decompLen, err := Decompress(compBuf[:compLen], decompBuf)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(input, decompBuf[:decompLen]) {
		t.Errorf("Roundtrip failed for extended length test")
	}

	t.Logf("Extended length test: %d -> %d bytes (%.1f%%)",
		len(input), compLen, float64(compLen)/float64(len(input))*100)
}

func TestCompressVeryLongLiterals(t *testing.T) {
	// Random-ish data that won't compress well - tests literal run encoding
	input := make([]byte, 1000)
	for i := range input {
		input[i] = byte((i * 7) % 256)
	}

	compBuf := make([]byte, MaxCompressedSize(len(input)))
	compLen, err := Compress(input, compBuf)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	decompBuf := make([]byte, len(input)+100)
	decompLen, err := Decompress(compBuf[:compLen], decompBuf)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(input, decompBuf[:decompLen]) {
		t.Errorf("Roundtrip failed for long literals test")
	}
}
