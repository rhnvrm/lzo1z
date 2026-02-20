package lzo1z

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestDecompress(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.inputLen == 0 {
				// Empty input test
				dst := make([]byte, 100)
				n, err := Decompress(tc.compressed, dst)
				if err != nil {
					t.Fatalf("Decompress failed: %v", err)
				}
				if n != 0 {
					t.Errorf("Expected 0 bytes, got %d", n)
				}
				return
			}

			// Allocate buffer with extra space
			dst := make([]byte, tc.inputLen+100)
			n, err := Decompress(tc.compressed, dst)
			if err != nil {
				t.Fatalf("Decompress failed: %v", err)
			}

			if n != tc.inputLen {
				t.Errorf("Wrong output length: got %d, want %d", n, tc.inputLen)
			}

			if !bytes.Equal(dst[:n], tc.input) {
				t.Errorf("Output mismatch")
				if len(tc.input) <= 64 {
					t.Errorf("Got:  %v", dst[:n])
					t.Errorf("Want: %v", tc.input)
				} else {
					t.Errorf("Got first 64:  %v", dst[:min(64, n)])
					t.Errorf("Want first 64: %v", tc.input[:min(64, len(tc.input))])
				}
			}
		})
	}
}

func TestDecompressOutputTooSmall(t *testing.T) {
	// Find a test case with reasonable size
	for _, tc := range testCases {
		if tc.inputLen > 10 {
			dst := make([]byte, 5) // Way too small
			_, err := Decompress(tc.compressed, dst)
			if err != ErrOutputOverrun {
				t.Errorf("Expected ErrOutputOverrun for %s, got: %v", tc.name, err)
			}
			break
		}
	}
}

func TestDecompressNilInput(t *testing.T) {
	dst := make([]byte, 100)
	n, err := Decompress(nil, dst)
	if err != nil {
		t.Errorf("Decompress(nil) returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("Decompress(nil) returned n=%d, want 0", n)
	}
}

func TestDecompressEmptyInput(t *testing.T) {
	dst := make([]byte, 100)
	n, err := Decompress([]byte{}, dst)
	if err != nil {
		t.Errorf("Decompress([]) returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("Decompress([]) returned n=%d, want 0", n)
	}
}

func BenchmarkDecompress(b *testing.B) {
	// Find a medium-sized test case
	var tc struct {
		name       string
		compressed []byte
		inputLen   int
	}
	for _, c := range testCases {
		if c.inputLen >= 100 && c.inputLen <= 1000 {
			tc.name = c.name
			tc.compressed = c.compressed
			tc.inputLen = c.inputLen
			break
		}
	}
	if tc.inputLen == 0 {
		b.Skip("No suitable test case found")
	}

	dst := make([]byte, tc.inputLen+100)
	b.ResetTimer()
	b.SetBytes(int64(tc.inputLen))

	for i := 0; i < b.N; i++ {
		_, _ = Decompress(tc.compressed, dst)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestDecompressErrors(t *testing.T) {
	tests := []struct {
		name       string
		compressed []byte
		dstSize    int
		wantErr    error
	}{
		{
			name:       "truncated M4 offset",
			compressed: []byte{0x18, 0x41, 0x42, 0x43, 0x11}, // M4 match but missing offset bytes
			dstSize:    100,
			wantErr:    ErrInputOverrun,
		},
		{
			name:       "truncated M3 offset",
			compressed: []byte{0x20}, // M3 match but no offset bytes
			dstSize:    100,
			wantErr:    ErrInputOverrun,
		},
		{
			name:       "output too small for literals",
			compressed: []byte{0x15, 0x41, 0x42, 0x43, 0x44}, // 4 literals but small output
			dstSize:    2,
			wantErr:    ErrOutputOverrun,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := make([]byte, tc.dstSize)
			_, err := Decompress(tc.compressed, dst)
			if err == nil {
				t.Errorf("expected error %v, got nil", tc.wantErr)
			}
		})
	}
}

func TestDecompressSafe(t *testing.T) {
	// DecompressSafe should behave identically to Decompress
	compressed := []byte{0x14, 0x41, 0x42, 0x43, 0x11, 0x00, 0x00}
	dst1 := make([]byte, 10)
	dst2 := make([]byte, 10)

	n1, err1 := Decompress(compressed, dst1)
	n2, err2 := DecompressSafe(compressed, dst2)

	if n1 != n2 || (err1 != nil) != (err2 != nil) {
		t.Errorf("DecompressSafe differs from Decompress: (%d, %v) vs (%d, %v)", n1, err1, n2, err2)
	}
}

const postLiteralMatchCompressedHex = "1a04595a2a2a3132330040000556c8736d00001c28402802316f3d9f00e04c01158b3020a000070236000200000cc90001601c40000505f4dd0004192e2d602da2400d0a40bd5e00471ad9003a00015d008618e8410ce00113403dde28003e02bb5dd4000c27007d4f403dcac1c460010a403d9829003d12620e000327003d1d5d4229003d145d4c2900bd21403d6002ec27003d275d6a2900bc4002c211a01fa891800013042a90000e2eaa5db4000e1096a066085e270417082e8f0027cf2e2b602d19400d0144156c4045563b04151740851a2901d521403d102903d518403d062902d508002ecefc2904d516403df22800fe017140bd2429007d3d403d3829017d1a403d4229013d64403d4c2904152382c504290287015f95a01f4c0f40465acd58a00ed384000ec9ca6060110000"

func TestDecompressPostLiteralMatch(t *testing.T) {
	src, err := hex.DecodeString(postLiteralMatchCompressedHex)
	if err != nil {
		t.Fatalf("decode compressed vector: %v", err)
	}

	dst := make([]byte, 4096)
	n, err := Decompress(src, dst)
	if err != nil {
		t.Fatalf("decompress post literal match vector: %v", err)
	}

	if n != 574 {
		t.Fatalf("decompressed length mismatch: got=%d want=574", n)
	}

	h := sha256.Sum256(dst[:n])
	const want = "5f65ac37285d37b6e0a4d6196ad92997e90a887f3e90831a9de43c925eee0f4a"
	if got := hex.EncodeToString(h[:]); got != want {
		t.Fatalf("decompressed payload hash mismatch: got=%s want=%s", got, want)
	}
}
