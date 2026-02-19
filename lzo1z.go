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

import "errors"

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
)

// Decompress decompresses LZO1Z compressed data from src into dst.
// Returns the number of bytes written to dst.
//
// The dst buffer must be large enough to hold the decompressed data.
// If dst is too small, ErrOutputOverrun is returned along with the
// number of bytes successfully written.
//
// This function is compatible with data compressed by lzo1z_999_compress()
// from the liblzo2 library.
func Decompress(src, dst []byte) (int, error) {
	if len(src) == 0 {
		return 0, nil
	}

	ip := 0 // input position
	op := 0 // output position
	inLen := len(src)
	outLen := len(dst)
	var lastMOff int // last match offset (LZO1Z feature)

	// State machine states
	const (
		stateStart = iota
		stateLiteralRun
		stateFirstLiteralRun
		stateMatch
		stateMatchDone
		stateMatchNext
		stateEOF
	)

	state := stateStart

	for state != stateEOF {
		switch state {
		case stateStart:
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			t := int(src[ip])

			if t > 17 {
				ip++
				t -= 17
				if t < 4 {
					// Copy t literals, then matchNext
					if op+t > outLen {
						return op, ErrOutputOverrun
					}
					if ip+t > inLen {
						return op, ErrInputOverrun
					}
					for i := 0; i < t; i++ {
						dst[op] = src[ip]
						op++
						ip++
					}
					state = stateMatchNext
					continue
				}
				// Copy t literals
				if op+t > outLen {
					return op, ErrOutputOverrun
				}
				if ip+t > inLen {
					return op, ErrInputOverrun
				}
				for i := 0; i < t; i++ {
					dst[op] = src[ip]
					op++
					ip++
				}
				state = stateFirstLiteralRun
				continue
			}
			// Don't increment ip here - stateLiteralRun will read the byte
			state = stateLiteralRun

		case stateLiteralRun:
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			t := int(src[ip])
			ip++

			if t >= 16 {
				// Process as match
				ip-- // Put back the byte for match processing
				state = stateMatch
				continue
			}

			// Literal run
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

			// Copy (t + 3) literal bytes
			copyLen := t + 3
			if op+copyLen > outLen {
				return op, ErrOutputOverrun
			}
			if ip+copyLen > inLen {
				return op, ErrInputOverrun
			}
			for i := 0; i < copyLen; i++ {
				dst[op] = src[ip]
				op++
				ip++
			}
			state = stateFirstLiteralRun

		case stateFirstLiteralRun:
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			t := int(src[ip])
			ip++

			if t >= 16 {
				ip-- // Put back for match processing
				state = stateMatch
				continue
			}

			// M1 match after first literal run
			// Offset = (1 + M2_MAX_OFFSET) + (t << 6) + (next_byte >> 2)
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			mOff := (1 + m2MaxOffset) + (t << 6) + int(src[ip]>>2)
			ip++
			lastMOff = mOff

			if mOff > op {
				return op, ErrLookbehindOverrun
			}
			if op+3 > outLen {
				return op, ErrOutputOverrun
			}
			mPos := op - mOff
			dst[op] = dst[mPos]
			dst[op+1] = dst[mPos+1]
			dst[op+2] = dst[mPos+2]
			op += 3
			state = stateMatchDone

		case stateMatch:
			if ip >= inLen {
				return op, ErrInputOverrun
			}
			t := int(src[ip])
			ip++

			if t >= 64 {
				// M2 match
				off := t & 0x1f
				var mOff int
				if off >= 0x1c {
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
				mLen := ((t >> 5) - 1) + 2

				if mOff > op {
					return op, ErrLookbehindOverrun
				}
				if op+mLen > outLen {
					return op, ErrOutputOverrun
				}
				mPos := op - mOff
				for i := 0; i < mLen; i++ {
					dst[op] = dst[mPos]
					op++
					mPos++
				}

			} else if t >= 32 {
				// M3 match
				mLen := t & 31
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
				mOff := 1 + int(src[ip])<<6 + int(src[ip+1]>>2)
				ip += 2
				lastMOff = mOff

				// Copy mLen + 2 bytes
				mLen += 2
				if mOff > op {
					return op, ErrLookbehindOverrun
				}
				if op+mLen > outLen {
					return op, ErrOutputOverrun
				}
				mPos := op - mOff
				for i := 0; i < mLen; i++ {
					dst[op] = dst[mPos]
					op++
					mPos++
				}

			} else if t >= 16 {
				// M4 match
				mOff := (t & 8) << 11
				mLen := t & 7
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
					// EOF marker found
					state = stateEOF
					continue
				}

				mOff += m4MaxOffset
				lastMOff = mOff

				// Copy mLen + 2 bytes
				mLen += 2
				if mOff > op {
					return op, ErrLookbehindOverrun
				}
				if op+mLen > outLen {
					return op, ErrOutputOverrun
				}
				mPos := op - mOff
				for i := 0; i < mLen; i++ {
					dst[op] = dst[mPos]
					op++
					mPos++
				}

			} else {
				// M1 match (t < 16) - copies 2 bytes
				if ip >= inLen {
					return op, ErrInputOverrun
				}
				mOff := 1 + (t << 6) + int(src[ip]>>2)
				ip++
				lastMOff = mOff

				if mOff > op {
					return op, ErrLookbehindOverrun
				}
				if op+2 > outLen {
					return op, ErrOutputOverrun
				}
				mPos := op - mOff
				dst[op] = dst[mPos]
				dst[op+1] = dst[mPos+1]
				op += 2
			}
			state = stateMatchDone

		case stateMatchDone:
			// Check for trailing literals (encoded in low 2 bits of last offset byte)
			if ip == 0 || ip > inLen {
				state = stateLiteralRun
				continue
			}
			t := int(src[ip-1]) & 3
			if t == 0 {
				state = stateLiteralRun
				continue
			}
			// Copy t trailing literal bytes
			if op+t > outLen {
				return op, ErrOutputOverrun
			}
			if ip+t > inLen {
				return op, ErrInputOverrun
			}
			for i := 0; i < t; i++ {
				dst[op] = src[ip]
				op++
				ip++
			}
			state = stateMatchNext

		case stateMatchNext:
			// After copying trailing literals, the next opcode is parsed via
			// the regular match path.
			state = stateMatch
		}
	}

	return op, nil
}

// DecompressSafe is an alias for Decompress that emphasizes bounds checking.
// All bounds are checked in Decompress, so this is functionally identical.
func DecompressSafe(src, dst []byte) (int, error) {
	return Decompress(src, dst)
}
