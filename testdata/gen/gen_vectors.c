/*
 * gen_vectors.c - Generate LZO1Z test vectors using liblzo2
 *
 * Compresses various input patterns with lzo1z_999_compress to produce
 * compressed data that exercises ALL opcode types (M1a, M1b, M2 offset
 * reuse, M2 lengths 5-8, trailing literals, etc.) that the Go compressor
 * never produces.
 *
 * Output: Go source code for testdata_interop_test.go
 *
 * Build: gcc -o gen_vectors gen_vectors.c -llzo2
 * Run:   ./gen_vectors > ../interop_vectors.go
 */

#include <lzo/lzoconf.h>
#include <lzo/lzo1z.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define MAX_INPUT   (1024 * 1024)  /* 1MB max */
#define MAX_OUTPUT  (MAX_INPUT + MAX_INPUT / 16 + 64 + 3)

static lzo_bytep in_buf;
static lzo_bytep out_buf;
static lzo_bytep wrkmem;

static void print_bytes(const lzo_bytep buf, lzo_uint len) {
    lzo_uint i;
    printf("\t\t\tcompressed: []byte{");
    for (i = 0; i < len; i++) {
        if (i > 0) printf(", ");
        if (i > 0 && i % 16 == 0) printf("\n\t\t\t\t");
        printf("0x%02x", buf[i]);
    }
    printf("},\n");
}

static void print_input_bytes(const lzo_bytep buf, lzo_uint len) {
    lzo_uint i;
    printf("\t\t\tinput: []byte{");
    for (i = 0; i < len; i++) {
        if (i > 0) printf(", ");
        if (i > 0 && i % 16 == 0) printf("\n\t\t\t\t");
        printf("0x%02x", buf[i]);
    }
    printf("},\n");
}

static int emit_vector(const char *name, const lzo_bytep input, lzo_uint in_len) {
    lzo_uint out_len = MAX_OUTPUT;
    int r;

    r = lzo1z_999_compress(input, in_len, out_buf, &out_len, wrkmem);
    if (r != LZO_E_OK) {
        fprintf(stderr, "compression failed for %s: %d\n", name, r);
        return -1;
    }

    printf("\t\t{\n");
    printf("\t\t\tname:     \"%s\",\n", name);
    printf("\t\t\tinputLen: %lu,\n", (unsigned long)in_len);
    print_input_bytes(input, in_len);
    print_bytes(out_buf, out_len);
    printf("\t\t},\n");

    return 0;
}

/* Generate input patterns designed to trigger specific opcode paths */

/* Pattern 1: Short repeated sequences at close offset -> triggers M1a */
static void gen_m1a_patterns(void) {
    lzo_uint i;

    /* Pattern: literal + 2-byte match at small offset, repeated */
    /* "ABCXABC" pattern where ABC repeats at offset 4 */
    {
        lzo_bytep p = in_buf;
        /* Build: 4-byte unique literal, then 2-byte match at offset 4, then literal, repeat */
        memcpy(p, "ABCDABEFABGHAB", 14);
        /* Extend to get more M1a matches */
        for (i = 14; i < 200; i += 2) {
            p[i] = 'A';
            p[i+1] = 'B';
        }
        emit_vector("m1a_short_repeat_close", p, 200);
    }

    /* Pattern: very short inter-match gaps (1-3 literals between matches) */
    {
        lzo_bytep p = in_buf;
        /* "ABCABC" at offset 3, then 1 literal, then another short match */
        for (i = 0; i < 300; i += 7) {
            memcpy(p + i, "ABCXABC", 7 < 300 - i ? 7 : 300 - i);
        }
        emit_vector("m1a_inter_match_gaps", p, 300);
    }
}

/* Pattern 2: Matches at offset > M2_MAX_OFFSET after 4+ literals -> M1b */
static void gen_m1b_patterns(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Create a match at offset ~2000 (> M2_MAX_OFFSET=1792) */
    /* Put a pattern, then 2000 bytes of unique data, then the same pattern */
    memcpy(p, "XYZXYZ", 6);
    for (i = 6; i < 2006; i++) {
        p[i] = (lzo_byte)(i * 7 + 13); /* pseudo-random unique data */
    }
    memcpy(p + 2006, "XYZXYZ", 6);
    /* Add more varied data after */
    for (i = 2012; i < 2500; i++) {
        p[i] = (lzo_byte)(i * 11 + 37);
    }
    emit_vector("m1b_medium_offset", p, 2500);

    /* Pattern at exactly M2_MAX_OFFSET+1 = 1793 */
    memset(p, 0, 3000);
    memcpy(p, "MNOP", 4);
    for (i = 4; i < 1797; i++) {
        p[i] = (lzo_byte)(i * 3 + 5);
    }
    memcpy(p + 1797, "MNOP", 4);
    for (i = 1801; i < 2200; i++) {
        p[i] = (lzo_byte)(i * 9 + 17);
    }
    emit_vector("m1b_offset_1793", p, 2200);
}

/* Pattern 3: Repeated matches at same offset -> triggers M2 offset reuse */
static void gen_m2_offset_reuse(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Create data where the same offset is matched repeatedly */
    /* "ABCDE" appears, then 100 bytes, then "ABCDE" again, then 1 literal,
       then "ABCDE" again (same offset for reuse) */
    memcpy(p, "ABCDE", 5);
    for (i = 5; i < 50; i++) {
        p[i] = (lzo_byte)(i + 0x30);
    }
    memcpy(p + 50, "ABCDE", 5);
    p[55] = 'Z'; /* 1 trailing literal */
    memcpy(p + 56, "ABCDE", 5); /* same offset, should trigger reuse */
    for (i = 61; i < 120; i++) {
        p[i] = (lzo_byte)(i + 0x40);
    }
    /* One more at same offset to really exercise reuse */
    memcpy(p + 120, "ABCDE", 5);
    for (i = 125; i < 200; i++) {
        p[i] = (lzo_byte)(i + 0x50);
    }
    emit_vector("m2_offset_reuse_basic", p, 200);

    /* Dense repeated-offset pattern */
    memcpy(p, "HELLO", 5);
    for (i = 5; i < 20; i++) p[i] = (lzo_byte)(i + 0x41);
    memcpy(p + 20, "HELLO", 5); /* first match */
    p[25] = '!';
    memcpy(p + 26, "HELLO", 5); /* offset reuse */
    p[31] = '?';
    memcpy(p + 32, "HELLO", 5); /* offset reuse again */
    p[37] = '.';
    memcpy(p + 38, "HELLO", 5); /* offset reuse again */
    for (i = 43; i < 100; i++) p[i] = (lzo_byte)(i + 0x61);
    emit_vector("m2_offset_reuse_dense", p, 100);
}

/* Pattern 4: Trailing literals in match encoding */
static void gen_trailing_literals(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Create matches followed by exactly 1, 2, 3 trailing literals */
    /* The C compressor encodes trailing literal count in low 2 bits of offset byte */
    memcpy(p, "ABCDEFGH", 8);
    for (i = 8; i < 40; i++) p[i] = (lzo_byte)(i + 0x30);
    memcpy(p + 40, "ABCDEFGH", 8); /* match */
    p[48] = 'X'; /* 1 trailing literal */
    memcpy(p + 49, "ABCDEFGH", 8); /* another match */
    p[57] = 'Y';
    p[58] = 'Z'; /* 2 trailing literals */
    memcpy(p + 59, "ABCDEFGH", 8); /* another match */
    p[67] = '1';
    p[68] = '2';
    p[69] = '3'; /* 3 trailing literals */
    memcpy(p + 70, "ABCDEFGH", 8); /* another match */
    for (i = 78; i < 150; i++) p[i] = (lzo_byte)(i + 0x41);
    emit_vector("trailing_literals_1_2_3", p, 150);
}

/* Pattern 5: M2 with longer lengths (5-8) */
static void gen_m2_long_lengths(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Close offset match with length 5-8 */
    memcpy(p, "ABCDEFGHIJ", 10);
    for (i = 10; i < 30; i++) p[i] = (lzo_byte)(i + 0x30);
    /* Match of length 5 */
    memcpy(p + 30, "ABCDE", 5);
    for (i = 35; i < 50; i++) p[i] = (lzo_byte)(i + 0x40);
    /* Match of length 6 */
    memcpy(p + 50, "ABCDEF", 6);
    for (i = 56; i < 70; i++) p[i] = (lzo_byte)(i + 0x50);
    /* Match of length 7 */
    memcpy(p + 70, "ABCDEFG", 7);
    for (i = 77; i < 90; i++) p[i] = (lzo_byte)(i + 0x60);
    /* Match of length 8 */
    memcpy(p + 90, "ABCDEFGH", 8);
    for (i = 98; i < 150; i++) p[i] = (lzo_byte)(i + 0x70);
    emit_vector("m2_lengths_5_to_8", p, 150);
}

/* Pattern 6: M4 with large offsets (>16384) */
static void gen_m4_large_offset(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Create match at offset > 16384 */
    memcpy(p, "LONGMATCH!", 10);
    /* Fill with unique data to push offset beyond 16384 */
    for (i = 10; i < 17000; i++) {
        p[i] = (lzo_byte)((i * 7 + 13) & 0xFF);
    }
    memcpy(p + 17000, "LONGMATCH!", 10);
    for (i = 17010; i < 17500; i++) {
        p[i] = (lzo_byte)((i * 11 + 37) & 0xFF);
    }
    emit_vector("m4_offset_17000", p, 17500);

    /* Match at exactly 16385 (minimum M4 offset) */
    memcpy(p, "EXACT!", 6);
    for (i = 6; i < 16391; i++) {
        p[i] = (lzo_byte)((i * 13 + 41) & 0xFF);
    }
    memcpy(p + 16391, "EXACT!", 6);
    for (i = 16397; i < 16900; i++) {
        p[i] = (lzo_byte)((i * 17 + 53) & 0xFF);
    }
    emit_vector("m4_offset_16385", p, 16900);
}

/* Pattern 7: M3 with extended length (very long matches) */
static void gen_m3_extended(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Create a long repeating pattern at medium offset */
    for (i = 0; i < 100; i++) {
        p[i] = (lzo_byte)(i & 0xFF);
    }
    /* Unique separator */
    for (i = 100; i < 200; i++) {
        p[i] = (lzo_byte)((i * 7) & 0xFF);
    }
    /* Repeat the first 100 bytes -> creates M3 match of length 100 */
    for (i = 200; i < 300; i++) {
        p[i] = (lzo_byte)((i - 200) & 0xFF);
    }
    /* More unique data */
    for (i = 300; i < 400; i++) {
        p[i] = (lzo_byte)((i * 11) & 0xFF);
    }
    /* Very long match: repeat first 300 bytes at offset 400 */
    memcpy(p + 400, p, 300);
    for (i = 700; i < 800; i++) {
        p[i] = (lzo_byte)((i * 13) & 0xFF);
    }
    emit_vector("m3_extended_length", p, 800);
}

/* Pattern 8: Mixed patterns that produce all match types together */
static void gen_mixed_all_types(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Build input that should exercise M1a, M1b, M2, M2-reuse, M3, M4, and trailing literals */

    /* Start with a literal run */
    for (i = 0; i < 20; i++) p[i] = (lzo_byte)('A' + i);

    /* Short match (M2) */
    memcpy(p + 20, "ABCDE", 5);
    /* 1 trailing literal */
    p[25] = '!';
    /* Another match at same offset (M2 reuse) */
    memcpy(p + 26, "ABCDE", 5);

    /* Some unique data */
    for (i = 31; i < 60; i++) p[i] = (lzo_byte)(i * 3 + 7);

    /* Medium-length M3 match */
    memcpy(p + 60, p, 20);

    /* More unique data */
    for (i = 80; i < 120; i++) p[i] = (lzo_byte)(i * 5 + 11);

    /* Short match for M1a (2 bytes at close offset) */
    p[120] = p[118];
    p[121] = p[119];

    /* Build up to M1b territory: need match at offset > 1792 */
    for (i = 122; i < 2000; i++) p[i] = (lzo_byte)(i * 7 + 13);

    /* Place the same initial pattern way far away for M4 territory */
    for (i = 2000; i < 18000; i++) p[i] = (lzo_byte)(i * 11 + 37);

    /* Copy 20 bytes from offset 0 at position 18000 -> M4 match */
    memcpy(p + 18000, p, 20);

    /* Trailing */
    for (i = 18020; i < 18100; i++) p[i] = (lzo_byte)(i * 13 + 41);

    emit_vector("mixed_all_match_types", p, 18100);
}

/* Pattern 9: Large data (64KB, 256KB) for block boundary tests */
static void gen_large_data(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* 64KB with repeating patterns */
    for (i = 0; i < 65536; i++) {
        p[i] = (lzo_byte)("The quick brown fox jumps over the lazy dog. "[i % 46]);
    }
    emit_vector("large_64kb_text", p, 65536);

    /* 64KB alternating compressible and random sections */
    for (i = 0; i < 65536; i++) {
        if ((i / 1024) % 2 == 0) {
            /* Compressible: repeating pattern */
            p[i] = (lzo_byte)(i % 4 + 'A');
        } else {
            /* Less compressible: pseudo-random */
            p[i] = (lzo_byte)((i * 2654435761U) >> 24);
        }
    }
    emit_vector("large_64kb_mixed", p, 65536);
}

/* Pattern 10: EOF edge cases */
static void gen_eof_patterns(void) {
    lzo_bytep p = in_buf;
    lzo_uint i;

    /* Input that ends right after a match (no trailing literals) */
    memcpy(p, "ABCDEFGH", 8);
    for (i = 8; i < 30; i++) p[i] = (lzo_byte)(i + 0x30);
    memcpy(p + 30, "ABCDEFGH", 8);
    emit_vector("eof_after_match", p, 38);

    /* Input ending with exactly 3 trailing literals */
    memcpy(p, "ABCDEFGH", 8);
    for (i = 8; i < 30; i++) p[i] = (lzo_byte)(i + 0x30);
    memcpy(p + 30, "ABCDEFGH", 8);
    p[38] = 'X';
    p[39] = 'Y';
    p[40] = 'Z';
    emit_vector("eof_with_3_trailing", p, 41);

    /* Very short inputs */
    memcpy(p, "AB", 2);
    emit_vector("tiny_2_bytes", p, 2);

    memcpy(p, "ABCABC", 6);
    emit_vector("tiny_6_with_match", p, 6);
}

int main(int argc, char *argv[]) {
    int r;

    (void)argc; (void)argv;

    if (lzo_init() != LZO_E_OK) {
        fprintf(stderr, "lzo_init() failed\n");
        return 1;
    }

    in_buf = (lzo_bytep) malloc(MAX_INPUT);
    out_buf = (lzo_bytep) malloc(MAX_OUTPUT);
    wrkmem = (lzo_bytep) malloc(LZO1Z_999_MEM_COMPRESS);
    if (!in_buf || !out_buf || !wrkmem) {
        fprintf(stderr, "out of memory\n");
        return 1;
    }

    printf("package lzo1z\n\n");
    printf("// Code generated by testdata/gen/gen_vectors.c using liblzo2. DO NOT EDIT.\n");
    printf("// Regenerate: cd testdata/gen && docker build -t lzo1z-gen . && docker run --rm lzo1z-gen > ../../interop_vectors_test.go\n\n");
    printf("var interopTestCases = []struct {\n");
    printf("\tname       string\n");
    printf("\tinputLen   int\n");
    printf("\tinput      []byte\n");
    printf("\tcompressed []byte\n");
    printf("}{");

    gen_m1a_patterns();
    gen_m1b_patterns();
    gen_m2_offset_reuse();
    gen_trailing_literals();
    gen_m2_long_lengths();
    gen_m4_large_offset();
    gen_m3_extended();
    gen_mixed_all_types();
    gen_large_data();
    gen_eof_patterns();

    printf("}\n");

    free(in_buf);
    free(out_buf);
    free(wrkmem);

    return 0;
}
