package lzo1z

// Compress compresses src using LZO1Z algorithm and writes to dst.
// Returns the number of bytes written to dst.
// dst must be large enough to hold the compressed data.
// Worst case size is: len(src) + len(src)/16 + 64 + 3
//
// This is a greedy compressor optimized for speed over compression ratio.
func Compress(src, dst []byte) (int, error) {
	if len(src) == 0 {
		return 0, nil
	}

	// For very short inputs, just store as literals
	if len(src) <= 3 {
		return compressLiteralsOnly(src, dst)
	}

	const (
		hashBits  = 14
		hashSize  = 1 << hashBits
		hashMask  = hashSize - 1
		maxOffset = 0xbfff // M4 max offset: 49151
		minMatch  = 3
	)

	// Hash table: maps 4-byte sequences to positions
	var hashTable [hashSize]int

	// Initialize hash table to -maxOffset to avoid false matches
	for i := range hashTable {
		hashTable[i] = -maxOffset
	}

	ip := 0               // input position
	op := 0               // output position
	litStart := 0         // start of pending literals
	isFirstOutput := true // whether we're at the start of output
	inLen := len(src)
	outLen := len(dst)

	// Hash function for 4 bytes
	hash := func(p int) int {
		if p+4 > inLen {
			return 0
		}
		v := uint32(src[p]) | uint32(src[p+1])<<8 | uint32(src[p+2])<<16 | uint32(src[p+3])<<24
		return int((v * 0x1e35a7bd) >> (32 - hashBits) & hashMask)
	}

	// Main compression loop
	for ip < inLen-minMatch {
		h := hash(ip)
		ref := hashTable[h]
		hashTable[h] = ip

		offset := ip - ref

		// Check for match
		if offset > 0 && offset <= maxOffset && ref >= 0 && ip+4 <= inLen {
			if src[ref] == src[ip] && src[ref+1] == src[ip+1] && src[ref+2] == src[ip+2] {
				// Found a match - determine length
				matchLen := 3
				maxLen := inLen - ip
				if maxLen > 264 { // Reasonable max for single match encoding
					maxLen = 264
				}
				for matchLen < maxLen && src[ref+matchLen] == src[ip+matchLen] {
					matchLen++
				}

				// Check if we can emit the pending literals before this match
				// Mid-stream literal runs must be >= 4 bytes (or 0)
				litLen := ip - litStart
				if !isFirstOutput && litLen > 0 && litLen < 4 {
					// Can't encode 1-3 literals mid-stream, skip this match
					ip++
					continue
				}

				// Check if emitting this match would leave 1-3 trailing literals
				// Mid-stream literal runs must be >= 4 bytes (encoded as 1-15)
				remainingAfterMatch := inLen - (ip + matchLen)
				if remainingAfterMatch > 0 && remainingAfterMatch < 4 {
					// Skip this match - include these bytes in the literal run
					ip++
					continue
				}

				// Emit pending literals first
				if litLen > 0 {
					n, err := emitLiterals(src[litStart:ip], dst[op:], isFirstOutput)
					if err != nil {
						return op, err
					}
					op += n
				}

				// Emit match
				n, err := emitMatch(dst[op:], offset, matchLen)
				if err != nil {
					return op, err
				}
				op += n
				isFirstOutput = false // After any output (literals or match)

				// Advance past the match
				ip += matchLen
				litStart = ip

				// Update hash table for positions within the match
				for i := ip - matchLen + 1; i < ip && i < inLen-4; i++ {
					hashTable[hash(i)] = i
				}
				continue
			}
		}

		ip++
	}

	// Handle remaining bytes as literals
	litLen := inLen - litStart
	if litLen > 0 {
		n, err := emitLiterals(src[litStart:], dst[op:], isFirstOutput)
		if err != nil {
			return op, err
		}
		op += n
	}

	// Emit EOF marker: 0x11 0x00 0x00
	if op+3 > outLen {
		return op, ErrOutputOverrun
	}
	dst[op] = 0x11
	dst[op+1] = 0x00
	dst[op+2] = 0x00
	op += 3

	return op, nil
}

// compressLiteralsOnly handles very short inputs (<=3 bytes)
func compressLiteralsOnly(src, dst []byte) (int, error) {
	op := 0
	n, err := emitLiterals(src, dst, true)
	if err != nil {
		return 0, err
	}
	op += n

	// EOF marker
	if op+3 > len(dst) {
		return op, ErrOutputOverrun
	}
	dst[op] = 0x11
	dst[op+1] = 0x00
	dst[op+2] = 0x00
	op += 3

	return op, nil
}

// emitLiterals writes a literal run to dst.
// isFirst indicates if this is the first output (uses different encoding).
func emitLiterals(lit, dst []byte, isFirst bool) (int, error) {
	if len(lit) == 0 {
		return 0, nil
	}

	op := 0
	litLen := len(lit)
	outLen := len(dst)

	if isFirst && litLen <= 238 {
		// First literal run: length encoded as (len + 17) if len <= 3
		// or (len - 3) if len >= 4, preceded by 0x00 if len >= 16
		if litLen <= 3 {
			// Encode as (len + 17)
			if op+1+litLen > outLen {
				return op, ErrOutputOverrun
			}
			dst[op] = byte(litLen + 17)
			op++
		} else if litLen <= 18 {
			// Length 4-18: encode as (len - 3)
			if op+1+litLen > outLen {
				return op, ErrOutputOverrun
			}
			dst[op] = byte(litLen - 3)
			op++
		} else {
			// Length > 18: need extended encoding
			// 0x00 followed by (len - 18) in multi-byte format
			if op+2+litLen > outLen {
				return op, ErrOutputOverrun
			}
			remaining := litLen - 18
			if remaining <= 255 {
				dst[op] = 0x00
				dst[op+1] = byte(remaining)
				op += 2
			} else {
				// Very long literal run
				dst[op] = 0x00
				op++
				for remaining > 255 {
					if op >= outLen {
						return op, ErrOutputOverrun
					}
					dst[op] = 0x00
					op++
					remaining -= 255
				}
				if op >= outLen {
					return op, ErrOutputOverrun
				}
				dst[op] = byte(remaining)
				op++
			}
		}
	} else {
		// Non-first literal run (after a match)
		// Uses same encoding as first literal run:
		// - 0-15 for lengths 3-18 (value = len - 3)
		// - 0x00 + extra bytes for longer runs
		if litLen <= 18 {
			if op+1+litLen > outLen {
				return op, ErrOutputOverrun
			}
			dst[op] = byte(litLen - 3)
			op++
		} else {
			// Extended literal encoding: 0x00 followed by (len - 18)
			remaining := litLen - 18
			dst[op] = 0x00
			op++
			for remaining > 255 {
				if op >= outLen {
					return op, ErrOutputOverrun
				}
				dst[op] = 0x00
				op++
				remaining -= 255
			}
			if op >= outLen {
				return op, ErrOutputOverrun
			}
			dst[op] = byte(remaining)
			op++
		}
	}

	// Copy literal bytes
	if op+litLen > outLen {
		return op, ErrOutputOverrun
	}
	copy(dst[op:], lit)
	op += litLen

	return op, nil
}

// emitMatch writes a match (offset, length) to dst.
// Returns bytes written.
func emitMatch(dst []byte, offset, length int) (int, error) {
	if len(dst) < 4 {
		return 0, ErrOutputOverrun
	}

	op := 0

	// LZO1Z match encoding:
	// M2: length 3-4, offset 1-1792 (0x700)
	// M3: length 3-33, offset 1-16384 (0x4000)
	// M4: length 3-9 (extendable), offset 16385-49151

	// Offset encoding for LZO1Z: (byte0 << 6) | (byte1 >> 2)
	// So: byte0 = (offset - 1) >> 6, byte1 = ((offset - 1) & 0x3f) << 2

	if length >= 3 && length <= 4 && offset >= 1 && offset <= 0x700 {
		// M2 match: 2 bytes
		// Format: 0b01LXXXXX 0bOOOOOOTT
		// L = length - 2 (0 or 1, so length 3-4 maps to 1-2, stored as 0-1... wait)
		// Actually: t >= 64, length = ((t >> 5) - 1) + 2 = (t >> 5) + 1
		// So for length 3: (t >> 5) = 2, t = 0b01000000 | offset_high
		// For length 4: (t >> 5) = 3, t = 0b01100000 | offset_high

		off := offset - 1
		offHigh := (off >> 6) & 0x1f
		offLow := (off & 0x3f) << 2

		lenCode := (length - 1) << 5 // length 3 -> 0x40, length 4 -> 0x60
		dst[op] = byte(lenCode | offHigh)
		dst[op+1] = byte(offLow)
		op += 2

	} else if length >= 3 && offset >= 1 && offset <= 0x4000 {
		// M3 match: 3+ bytes
		// Format: 0b001LLLLL [0x00...] 0bOOOOOOOO 0bOOOOOOTT
		// Length encoding: if L == 0, extended length follows

		off := offset - 1
		offByte0 := (off >> 6) & 0xff
		offByte1 := ((off & 0x3f) << 2)

		if length <= 33 {
			// Length fits in 5 bits (length - 2)
			lenCode := length - 2
			dst[op] = byte(0x20 | lenCode)
			op++
		} else {
			// Extended length
			dst[op] = 0x20 // L = 0
			op++
			remaining := length - 2 - 31
			for remaining > 255 {
				dst[op] = 0x00
				op++
				remaining -= 255
			}
			dst[op] = byte(remaining)
			op++
		}

		dst[op] = byte(offByte0)
		dst[op+1] = byte(offByte1)
		op += 2

	} else if offset > 0x4000 && offset <= 0xbfff {
		// M4 match: 3+ bytes, large offset
		// Format: 0b0001HLLL [0x00...] 0bOOOOOOOO 0bOOOOOOTT
		// H = high bit of offset (adds 0x4000 to offset)
		// This is for offsets 16385-49151 (0x4001-0xbfff)
		// Note: M4 does NOT add +1 to offset (unlike M2/M3)

		off := offset - 0x4000
		offHigh := (off >> 11) & 0x08
		offByte0 := (off >> 6) & 0xff
		offByte1 := ((off & 0x3f) << 2)

		if length <= 9 {
			lenCode := length - 2
			dst[op] = byte(0x10 | offHigh | lenCode)
			op++
		} else {
			dst[op] = byte(0x10 | offHigh) // L = 0
			op++
			remaining := length - 2 - 7
			for remaining > 255 {
				dst[op] = 0x00
				op++
				remaining -= 255
			}
			dst[op] = byte(remaining)
			op++
		}

		dst[op] = byte(offByte0)
		dst[op+1] = byte(offByte1)
		op += 2

	} else {
		return 0, ErrOutputOverrun // Can't encode this match
	}

	return op, nil
}

// MaxCompressedSize returns the maximum possible compressed size for input of length n.
// Use this to allocate the destination buffer.
func MaxCompressedSize(n int) int {
	if n == 0 {
		return 3 // Just EOF marker
	}
	// Worst case: all literals + overhead + EOF
	return n + n/16 + 64 + 3
}
