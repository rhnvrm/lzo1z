// Package lzo1z implements LZO1Z decompression in pure Go.
//
// LZO1Z is a variant of the LZO1X compression algorithm with different
// offset encoding, used in real-time data feeds and embedded systems.
//
// This implementation is based on the original LZO library by
// Markus Franz Xaver Johannes Oberhumer (http://www.oberhumer.com/opensource/lzo/).
//
// Key differences from LZO1X:
//   - Offset encoding uses (ip[0] << 6) + (ip[1] >> 2) instead of (ip[0] >> 2) + (ip[1] << 6)
//   - M2 matches can reuse the last match offset when (t & 0x1f) >= 0x1c
//   - Different M2_MAX_OFFSET constant (0x0700 vs 0x0800)
package lzo1z

import (
	"encoding/binary"
	"errors"
)

// Algorithm constants
const (
	m2MaxOffset = 0x0700 // 1792 - LZO1Z specific (LZO1X uses 0x0800)
	m4MaxOffset = 0x4000 // 16384 - same across LZO variants
)

// Errors returned by Decompress
var (
	ErrInputOverrun      = errors.New("lzo1z: input buffer overrun")
	ErrOutputOverrun     = errors.New("lzo1z: output buffer overrun")
	ErrLookbehindOverrun = errors.New("lzo1z: lookbehind overrun (match references before output start)")
	ErrCorrupted         = errors.New("lzo1z: corrupted input data")
	ErrInputNotConsumed  = errors.New("lzo1z: input not fully consumed (extra bytes after EOF marker)")
)

// Decompress decompresses LZO1Z compressed data from src into dst.
// Returns the number of bytes written to dst.
//
// The dst buffer must be large enough to hold the decompressed data.
// If dst is too small, ErrOutputOverrun is returned along with the
// number of bytes successfully written. Decompress never writes past the
// length it returns.
//
// This function is compatible with data compressed by lzo1z_999_compress()
// from the liblzo2 library.
//
// The control flow follows lzo1x_d.ch from liblzo2. Each label is a point
// in the token stream, and goto moves between them. A state variable and a
// switch per token cost more than the decoding itself on short tokens.
func Decompress(src, dst []byte) (int, error) {
	if len(src) == 0 {
		return 0, nil
	}

	var (
		ip, op   int // input and output positions
		inLen    = len(src)
		outLen   = len(dst)
		t        int // the current token, or a literal count
		mOff     int // match offset back from op
		mLen     int // match length
		lastMOff int // last match offset, which LZO1Z M2 matches can reuse
	)

	t = int(src[0])
	if t > 17 {
		ip = 1
		t -= 17
		if t < 4 {
			goto matchNext
		}
		if op+t > outLen {
			return op, ErrOutputOverrun
		}
		if ip+t > inLen {
			return op, ErrInputOverrun
		}
		copyShort(dst[op:op+t], src[ip:ip+t])
		op += t
		ip += t
		goto firstLiteralRun
	}

literalRun:
	if ip >= inLen {
		return op, ErrInputOverrun
	}
	t = int(src[ip])
	ip++
	if t >= 16 {
		goto match
	}
	if t == 0 {
		// Long literal run
		for ip < inLen && src[ip] == 0 {
			t += 255
			ip++
		}
		if ip >= inLen {
			return op, ErrInputOverrun
		}
		t += 15 + int(src[ip])
		ip++
	}
	// Copy t + 3 literal bytes
	t += 3
	if op+t > outLen {
		return op, ErrOutputOverrun
	}
	if ip+t > inLen {
		return op, ErrInputOverrun
	}
	copyShort(dst[op:op+t], src[ip:ip+t])
	op += t
	ip += t

firstLiteralRun:
	if ip >= inLen {
		return op, ErrInputOverrun
	}
	t = int(src[ip])
	ip++
	if t >= 16 {
		goto match
	}
	// M1 match after a literal run.
	// Offset = (1 + M2_MAX_OFFSET) + (t << 6) + (next_byte >> 2)
	if ip >= inLen {
		return op, ErrInputOverrun
	}
	mOff = (1 + m2MaxOffset) + (t << 6) + int(src[ip]>>2)
	ip++
	lastMOff = mOff
	if mOff > op {
		return op, ErrLookbehindOverrun
	}
	if op+3 > outLen {
		return op, ErrOutputOverrun
	}
	copyMatch(dst, op, mOff, 3)
	op += 3
	goto matchDone

match:
	// t is the token and ip sits just past it.
	switch {
	case t >= 64:
		// M2 match
		if off := t & 0x1f; off >= 0x1c {
			// Reuse last match offset (LZO1Z feature)
			if lastMOff == 0 {
				return op, ErrLookbehindOverrun
			}
			mOff = lastMOff
		} else {
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			mOff = 1 + (off << 6) + int(src[ip]>>2)
			ip++
			lastMOff = mOff
		}
		// Length: (t >> 5) - 1, then copy length + 2 bytes
		mLen = (t >> 5) + 1

	case t >= 32:
		// M3 match
		mLen = t & 31
		if mLen == 0 {
			// Extended length
			for ip < inLen && src[ip] == 0 {
				mLen += 255
				ip++
			}
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			mLen += 31 + int(src[ip])
			ip++
		}
		if ip+2 > inLen {
			return op, ErrInputOverrun
		}
		// LZO1Z offset encoding: (ip[0] << 6) + (ip[1] >> 2)
		mOff = 1 + int(src[ip])<<6 + int(src[ip+1]>>2)
		ip += 2
		lastMOff = mOff
		mLen += 2

	case t >= 16:
		// M4 match
		mOff = (t & 8) << 11
		mLen = t & 7
		if mLen == 0 {
			// Extended length
			for ip < inLen && src[ip] == 0 {
				mLen += 255
				ip++
			}
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			mLen += 7 + int(src[ip])
			ip++
		}
		if ip+2 > inLen {
			return op, ErrInputOverrun
		}
		// LZO1Z offset encoding
		mOff += int(src[ip])<<6 + int(src[ip+1]>>2)
		ip += 2
		if mOff == 0 {
			goto eof
		}
		mOff += m4MaxOffset
		lastMOff = mOff
		mLen += 2

	default:
		// M1 match (t < 16), which copies 2 bytes
		if ip >= inLen {
			return op, ErrInputOverrun
		}
		mOff = 1 + (t << 6) + int(src[ip]>>2)
		ip++
		lastMOff = mOff
		mLen = 2
	}
	if mOff > op {
		return op, ErrLookbehindOverrun
	}
	if op+mLen > outLen {
		return op, ErrOutputOverrun
	}
	if mOff >= mLen && mLen <= 8 {
		// Most matches are 2 to 8 bytes and don't overlap their source.
		// Copy them inline. A call per match costs more than the copy.
		s := op - mOff
		if mLen >= 4 {
			a, b := binary.LittleEndian.Uint32(dst[s:]), binary.LittleEndian.Uint32(dst[s+mLen-4:])
			binary.LittleEndian.PutUint32(dst[op:], a)
			binary.LittleEndian.PutUint32(dst[op+mLen-4:], b)
		} else {
			// First, middle and last byte cover 2 and 3 bytes.
			dst[op], dst[op+mLen>>1], dst[op+mLen-1] = dst[s], dst[s+mLen>>1], dst[s+mLen-1]
		}
	} else {
		copyMatch(dst, op, mOff, mLen)
	}
	op += mLen

matchDone:
	// Trailing literals are encoded in the low 2 bits of the last offset byte.
	t = int(src[ip-1]) & 3
	if t == 0 {
		goto literalRun
	}

matchNext:
	// Copy t trailing literal bytes, then decode the next token as a match.
	if op+t > outLen {
		return op, ErrOutputOverrun
	}
	if ip+t > inLen {
		return op, ErrInputOverrun
	}
	// t is 1 to 3. First, middle and last byte cover all three lengths.
	dst[op], dst[op+t>>1], dst[op+t-1] = src[ip], src[ip+t>>1], src[ip+t-1]
	op += t
	ip += t
	if ip >= inLen {
		return op, ErrInputOverrun
	}
	t = int(src[ip])
	ip++
	goto match

eof:
	// Check for unconsumed input after EOF marker (matches C's LZO_E_INPUT_NOT_CONSUMED)
	if ip < inLen {
		return op, ErrInputNotConsumed
	}
	return op, nil
}

// copyShort copies s into d, which have the same length and don't overlap.
// LZO1Z literal runs and matches are mostly a few bytes long, where copy's
// memmove call costs more than the copy itself. From 4 bytes up, two loads
// and stores of the same width cover the run. The second ends exactly at the
// last byte, so nothing past d is written. Below 4 bytes only the M1 match
// after a literal run gets here, which is rare enough to leave to copy.
func copyShort(d, s []byte) {
	switch n := len(s); {
	case n > 16:
		copy(d, s)
	case n >= 8:
		a, b := binary.LittleEndian.Uint64(s), binary.LittleEndian.Uint64(s[n-8:])
		binary.LittleEndian.PutUint64(d, a)
		binary.LittleEndian.PutUint64(d[n-8:], b)
	case n >= 4:
		a, b := binary.LittleEndian.Uint32(s), binary.LittleEndian.Uint32(s[n-4:])
		binary.LittleEndian.PutUint32(d, a)
		binary.LittleEndian.PutUint32(d[n-4:], b)
	default:
		copy(d, s)
	}
}

// copyMatch copies n bytes that start mOff bytes back from op to op. When
// mOff < n the source runs into the bytes being written, and the match
// repeats its first mOff bytes. That needs the byte loop. The caller has
// checked that mOff <= op and op+n <= len(dst).
func copyMatch(dst []byte, op, mOff, n int) {
	if mOff >= n {
		copyShort(dst[op:op+n], dst[op-mOff:op-mOff+n])
		return
	}
	d := dst[op : op+n]
	if n <= 16 {
		// d and s share dst's array, so a forward copy reads back bytes it
		// has just written. Slicing both to n drops the per-byte bounds
		// checks.
		s := dst[op-mOff : op-mOff+n]
		for i := range d {
			d[i] = s[i]
		}
		return
	}
	// A long overlapping match, such as a run of zeros, repeats its first
	// mOff bytes. Copy them once, then double the filled prefix. Every
	// prefix length is a multiple of mOff, so the period holds.
	filled := copy(d, dst[op-mOff:op])
	for filled < n {
		filled += copy(d[filled:], d[:filled])
	}
}

// DecompressSafe is an alias for Decompress that emphasizes bounds checking.
// All bounds are checked in Decompress, so this is functionally identical.
func DecompressSafe(src, dst []byte) (int, error) {
	return Decompress(src, dst)
}
