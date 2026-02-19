package lzo1z

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

const regressionCompressedHex = "1a04595a2a2a3132330040000556c8736d00001c28402802316f3d9f00e04c01158b3020a000070236000200000cc90001601c40000505f4dd0004192e2d602da2400d0a40bd5e00471ad9003a00015d008618e8410ce00113403dde28003e02bb5dd4000c27007d4f403dcac1c460010a403d9829003d12620e000327003d1d5d4229003d145d4c2900bd21403d6002ec27003d275d6a2900bc4002c211a01fa891800013042a90000e2eaa5db4000e1096a066085e270417082e8f0027cf2e2b602d19400d0144156c4045563b04151740851a2901d521403d102903d518403d062902d508002ecefc2904d516403df22800fe017140bd2429007d3d403d3829017d1a403d4229013d64403d4c2904152382c504290287015f95a01f4c0f40465acd58a00ed384000ec9ca6060110000"

func TestDecompressRegressionVector(t *testing.T) {
	src, err := hex.DecodeString(regressionCompressedHex)
	if err != nil {
		t.Fatalf("decode compressed vector: %v", err)
	}

	dst := make([]byte, 4096)
	n, err := Decompress(src, dst)
	if err != nil {
		t.Fatalf("decompress regression vector: %v", err)
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
