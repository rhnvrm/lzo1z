package lzo1z

import (
	"bytes"
	"testing"
)

// Tests specifically targeting uncovered code paths for better coverage

func TestEmitLiteralsExtended(t *testing.T) {
	// Test extended literal encoding (> 18 bytes)
	tests := []struct {
		name    string
		litLen  int
		isFirst bool
	}{
		{"first_19", 19, true},
		{"first_50", 50, true},
		{"first_256", 256, true},
		{"first_1000", 1000, true},
		{"mid_19", 19, false},
		{"mid_50", 50, false},
		{"mid_256", 256, false},
		{"mid_1000", 1000, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lit := bytes.Repeat([]byte{'X'}, tc.litLen)
			dst := make([]byte, tc.litLen+100)

			n, err := emitLiterals(lit, dst, tc.isFirst)
			if err != nil {
				t.Fatalf("emitLiterals failed: %v", err)
			}
			if n < tc.litLen {
				t.Errorf("wrote %d bytes, expected at least %d", n, tc.litLen)
			}
		})
	}
}

func TestEmitMatchM2(t *testing.T) {
	// M2: length 3-4, offset 1-1792
	tests := []struct {
		offset int
		length int
	}{
		{1, 3},
		{1, 4},
		{100, 3},
		{100, 4},
		{1792, 3},
		{1792, 4},
	}

	for _, tc := range tests {
		dst := make([]byte, 10)
		n, err := emitMatch(dst, tc.offset, tc.length)
		if err != nil {
			t.Errorf("M2 offset=%d len=%d failed: %v", tc.offset, tc.length, err)
			continue
		}
		if n != 2 {
			t.Errorf("M2 offset=%d len=%d wrote %d bytes, expected 2", tc.offset, tc.length, n)
		}
	}
}

func TestEmitMatchM3(t *testing.T) {
	// M3: length 3+, offset 1-16384
	tests := []struct {
		offset int
		length int
	}{
		{1, 5},      // Uses M3 because length > 4
		{1, 33},     // Max length without extension
		{1, 34},     // Needs extended length
		{1, 100},    // Extended length
		{1, 300},    // Very extended length (multiple 255s)
		{16384, 5},  // Max M3 offset
		{16384, 50}, // Max offset with extended length
	}

	for _, tc := range tests {
		dst := make([]byte, 100)
		n, err := emitMatch(dst, tc.offset, tc.length)
		if err != nil {
			t.Errorf("M3 offset=%d len=%d failed: %v", tc.offset, tc.length, err)
			continue
		}
		if n < 3 {
			t.Errorf("M3 offset=%d len=%d wrote %d bytes, expected >= 3", tc.offset, tc.length, n)
		}
	}
}

func TestEmitMatchM4(t *testing.T) {
	// M4: length 3+, offset 16385-49151
	tests := []struct {
		offset int
		length int
	}{
		{16385, 3},  // Min M4 offset
		{16385, 9},  // Max length without extension
		{16385, 10}, // Needs extended length
		{16385, 50}, // Extended length
		{32000, 5},  // Mid-range offset
		{49151, 5},  // Max M4 offset
	}

	for _, tc := range tests {
		dst := make([]byte, 100)
		n, err := emitMatch(dst, tc.offset, tc.length)
		if err != nil {
			t.Errorf("M4 offset=%d len=%d failed: %v", tc.offset, tc.length, err)
			continue
		}
		if n < 3 {
			t.Errorf("M4 offset=%d len=%d wrote %d bytes, expected >= 3", tc.offset, tc.length, n)
		}
	}
}

func TestEmitMatchErrors(t *testing.T) {
	// Test error conditions
	dst := make([]byte, 2) // Too small

	_, err := emitMatch(dst, 1, 3)
	if err != ErrOutputOverrun {
		t.Errorf("expected ErrOutputOverrun for small buffer, got %v", err)
	}

	// Invalid offset (too large)
	dst = make([]byte, 100)
	_, err = emitMatch(dst, 50000, 3)
	if err != ErrOutputOverrun {
		t.Errorf("expected ErrOutputOverrun for huge offset, got %v", err)
	}
}

func TestCompressLiteralsOnly(t *testing.T) {
	// Test compressLiteralsOnly for small inputs
	tests := [][]byte{
		{},
		{0x01},
		{0x01, 0x02},
		{0x01, 0x02, 0x03},
	}

	for _, input := range tests {
		dst := make([]byte, MaxCompressedSize(len(input)))
		n, err := Compress(input, dst)
		if err != nil {
			t.Errorf("Compress(%v) failed: %v", input, err)
			continue
		}

		// Verify roundtrip
		out := make([]byte, len(input)+10)
		m, err := Decompress(dst[:n], out)
		if err != nil {
			t.Errorf("Decompress failed: %v", err)
			continue
		}
		if !bytes.Equal(input, out[:m]) {
			t.Errorf("roundtrip failed for %v", input)
		}
	}
}

func TestDecompressErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		compressed []byte
	}{
		{"truncated_start", []byte{0x20}},
		{"truncated_m3_length", []byte{0x20, 0x00}},
		{"truncated_m4", []byte{0x10}},
		{"truncated_m4_offset", []byte{0x10, 0x00}},
		{"bad_m2_after_literal", []byte{0x12, 0x41, 0x42, 0x40}},
		{"lookbehind_m3", []byte{0x01, 0x41, 0x41, 0x41, 0x41, 0x25, 0xff, 0x00}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := make([]byte, 1000)
			_, err := Decompress(tc.compressed, out)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestCompressWithVeryLongMatch(t *testing.T) {
	// Create data with a very long repeating pattern to trigger extended match encoding
	input := bytes.Repeat([]byte{'A'}, 500)

	dst := make([]byte, MaxCompressedSize(len(input)))
	n, err := Compress(input, dst)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Should compress very well
	if n > 50 {
		t.Errorf("500 repeated bytes compressed to %d, expected < 50", n)
	}

	// Verify roundtrip
	out := make([]byte, len(input)+10)
	m, err := Decompress(dst[:n], out)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}
	if !bytes.Equal(input, out[:m]) {
		t.Errorf("roundtrip failed")
	}
}

func TestCompressOutputOverrun(t *testing.T) {
	input := []byte("Hello, World!")
	dst := make([]byte, 5) // Too small

	_, err := Compress(input, dst)
	if err != ErrOutputOverrun {
		t.Errorf("expected ErrOutputOverrun, got %v", err)
	}
}

func TestEmitLiteralsOutputOverrun(t *testing.T) {
	lit := bytes.Repeat([]byte{'X'}, 100)
	dst := make([]byte, 10) // Too small

	_, err := emitLiterals(lit, dst, true)
	if err != ErrOutputOverrun {
		t.Errorf("expected ErrOutputOverrun, got %v", err)
	}

	_, err = emitLiterals(lit, dst, false)
	if err != ErrOutputOverrun {
		t.Errorf("expected ErrOutputOverrun for non-first, got %v", err)
	}
}

func TestDecompressM1Paths(t *testing.T) {
	// Test M1 match paths (t < 16 after a match)
	// This requires carefully crafted compressed data

	// Create input that will generate M1 matches
	input := []byte("ABCDABCD") // Will match with small offset

	dst := make([]byte, MaxCompressedSize(len(input)))
	n, err := Compress(input, dst)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	out := make([]byte, len(input)+10)
	m, err := Decompress(dst[:n], out)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}
	if !bytes.Equal(input, out[:m]) {
		t.Errorf("roundtrip failed")
	}
}

func TestDecompressM2OffsetReuse(t *testing.T) {
	// Test M2 match with offset reuse (off >= 0x1c)
	// Create input that triggers this path
	input := bytes.Repeat([]byte("AB"), 50)

	dst := make([]byte, MaxCompressedSize(len(input)))
	n, err := Compress(input, dst)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	out := make([]byte, len(input)+10)
	m, err := Decompress(dst[:n], out)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}
	if !bytes.Equal(input, out[:m]) {
		t.Errorf("roundtrip failed")
	}
}

func TestDecompressAllMatchTypes(t *testing.T) {
	// Create test data that exercises all match types
	tests := []struct {
		name  string
		input []byte
	}{
		// M1 matches (small offset, after match)
		{"m1_small", bytes.Repeat([]byte("ABAB"), 20)},

		// M2 matches with various offsets
		{"m2_offset_1", bytes.Repeat([]byte("X"), 100)},
		{"m2_offset_100", append(bytes.Repeat([]byte("Y"), 100), bytes.Repeat([]byte("Y"), 50)...)},

		// M3 matches with extended length
		{"m3_long", bytes.Repeat([]byte("Z"), 300)},

		// Mixed patterns
		{"mixed", []byte("AAAABBBBCCCCAAAABBBBCCCCAAAABBBBCCCC")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := make([]byte, MaxCompressedSize(len(tc.input)))
			n, err := Compress(tc.input, dst)
			if err != nil {
				t.Fatalf("Compress failed: %v", err)
			}

			out := make([]byte, len(tc.input)+100)
			m, err := Decompress(dst[:n], out)
			if err != nil {
				t.Fatalf("Decompress failed: %v", err)
			}
			if !bytes.Equal(tc.input, out[:m]) {
				t.Errorf("roundtrip failed")
			}
		})
	}
}

func TestEmitLiteralsVeryLong(t *testing.T) {
	// Test very long literal runs that need multiple 0x00 bytes
	sizes := []int{500, 1000, 2000, 5000}

	for _, size := range sizes {
		lit := make([]byte, size)
		for i := range lit {
			lit[i] = byte(i % 256)
		}

		dst := make([]byte, size+100)
		n, err := emitLiterals(lit, dst, true)
		if err != nil {
			t.Errorf("emitLiterals(%d) failed: %v", size, err)
			continue
		}
		if n < size {
			t.Errorf("emitLiterals(%d) wrote only %d bytes", size, n)
		}

		// Also test non-first
		_, err = emitLiterals(lit, dst, false)
		if err != nil {
			t.Errorf("emitLiterals(%d, false) failed: %v", size, err)
		}
	}
}

func TestDecompressStateTransitions(t *testing.T) {
	// Test various state transitions in decompressor
	// by creating inputs that follow specific paths

	tests := [][]byte{
		// First byte > 17, t < 4: stateStart -> stateMatchNext
		[]byte("AAA"),

		// First byte > 17, t >= 4: stateStart -> stateFirstLiteralRun
		[]byte("AAAAAA"),

		// First byte <= 17: stateStart -> stateLiteralRun
		bytes.Repeat([]byte("X"), 20),

		// Multiple matches with different offsets
		[]byte("ABCDEFABCDEFABCDEF"),
	}

	for i, input := range tests {
		dst := make([]byte, MaxCompressedSize(len(input)))
		n, err := Compress(input, dst)
		if err != nil {
			t.Errorf("test %d: Compress failed: %v", i, err)
			continue
		}

		out := make([]byte, len(input)+100)
		m, err := Decompress(dst[:n], out)
		if err != nil {
			t.Errorf("test %d: Decompress failed: %v", i, err)
			continue
		}
		if !bytes.Equal(input, out[:m]) {
			t.Errorf("test %d: roundtrip failed", i)
		}
	}
}

func TestDecompressEdgeCases(t *testing.T) {
	// More edge cases for decompression error paths

	tests := []struct {
		name string
		data []byte
	}{
		// Extended M3 length with zeros
		{"m3_ext_zeros", []byte{0x20, 0x00, 0x00, 0x01}},

		// M4 with extended length
		{"m4_ext", []byte{0x10, 0x00, 0x00, 0x00}},

		// Truncated in middle of copy
		{"truncated_copy", []byte{0x15, 0x41, 0x42}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := make([]byte, 1000)
			_, _ = Decompress(tc.data, out) // Just ensure no panic
		})
	}
}

func TestMaxCompressedSizeEdgeCases(t *testing.T) {
	tests := []int{0, 1, 15, 16, 17, 100, 1000, 65536}

	for _, n := range tests {
		size := MaxCompressedSize(n)
		if n > 0 && size <= n {
			t.Errorf("MaxCompressedSize(%d) = %d, should be > input", n, size)
		}
	}
}

func TestEmitLiteralsBoundaryConditions(t *testing.T) {
	// Test boundary conditions in emitLiterals

	// Test litLen = 4 (boundary between <= 3 and > 3)
	lit4 := []byte("ABCD")
	dst := make([]byte, 10)
	n, err := emitLiterals(lit4, dst, true)
	if err != nil || n != 5 {
		t.Errorf("litLen=4 first: got n=%d err=%v", n, err)
	}

	// Test litLen = 18 (boundary for extended encoding)
	lit18 := bytes.Repeat([]byte("X"), 18)
	dst = make([]byte, 30)
	_, err = emitLiterals(lit18, dst, true)
	if err != nil {
		t.Errorf("litLen=18 first: err=%v", err)
	}

	// Test litLen = 19 (first extended encoding)
	lit19 := bytes.Repeat([]byte("X"), 19)
	dst = make([]byte, 30)
	_, err = emitLiterals(lit19, dst, true)
	if err != nil {
		t.Errorf("litLen=19 first: err=%v", err)
	}

	// Test non-first with various lengths
	for _, length := range []int{4, 5, 10, 18, 19, 50, 300} {
		lit := bytes.Repeat([]byte("Y"), length)
		dst := make([]byte, length+50)
		_, err := emitLiterals(lit, dst, false)
		if err != nil {
			t.Errorf("litLen=%d non-first: err=%v", length, err)
		}
	}
}

func TestCompressSkipMatchConditions(t *testing.T) {
	// Test conditions where matches are skipped

	// Input that would leave 1-3 trailing literals after a match
	inputs := [][]byte{
		[]byte("AAAABBBB1"),   // Would leave 1 trailing
		[]byte("AAAABBBB12"),  // Would leave 2 trailing
		[]byte("AAAABBBB123"), // Would leave 3 trailing
	}

	for _, input := range inputs {
		dst := make([]byte, MaxCompressedSize(len(input)))
		n, err := Compress(input, dst)
		if err != nil {
			t.Errorf("Compress(%q) failed: %v", string(input), err)
			continue
		}

		out := make([]byte, len(input)+10)
		m, err := Decompress(dst[:n], out)
		if err != nil {
			t.Errorf("Decompress failed for %q: %v", string(input), err)
			continue
		}
		if !bytes.Equal(input, out[:m]) {
			t.Errorf("roundtrip failed for %q", string(input))
		}
	}
}

func TestDecompressMalformedInput(t *testing.T) {
	// Test decompressor with various malformed inputs
	malformed := [][]byte{
		// Empty
		{},
		// Just EOF marker
		{0x11, 0x00, 0x00},
		// Truncated literal
		{0x18, 0x41},
		// Invalid M2 match (lookbehind)
		{0x18, 0x41, 0x40, 0xff},
		// M3 with zero offset (invalid)
		{0x18, 0x41, 0x20, 0x00, 0x00},
		// Extended literal with truncation
		{0x00, 0xff},
		// M4 near EOF marker
		{0x11, 0x01, 0x00},
	}

	for i, data := range malformed {
		out := make([]byte, 1000)
		_, err := Decompress(data, out)
		// We just want to ensure no panic, errors are expected
		_ = err
		t.Logf("malformed[%d]: err=%v", i, err)
	}
}

func TestCompressLiteralsOnlyError(t *testing.T) {
	// Test compressLiteralsOnly with small output buffer
	input := []byte("AB")
	dst := make([]byte, 2) // Too small for output + EOF

	_, err := Compress(input, dst)
	if err == nil {
		t.Error("expected error for small buffer")
	}
}
