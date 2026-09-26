package raster

// A 3×5 bitmap font, for text drawn inside a taskbar icon.
//
// The 5×7 face in font.go is legible at icon size for two or three characters
// and no more: a four-character figure needs 5×4+3 = 23 font pixels across, and a
// sixteen-pixel slot has fourteen to give, which is under two thirds of a pixel
// per font pixel. A figure that cannot be read is worse than no figure, so the
// text styles use this smaller face instead — three pixels wide means four
// characters fit at a seventeenth of a pixel each, which rounds to a legible
// glyph once the canvas is supersampled.
//
// The glyph set is deliberately tiny: digits, the separators a status figure
// needs, and the two letter suffixes the abbreviations use.

const (
	compactW = 3
	compactH = 5
)

// CompactGlyphHeight is the number of font pixels in a line of the compact face,
// exported so a caller fitting text into a box does not have to know the face's
// proportions to divide by them.
const CompactGlyphHeight = compactH

var compactGlyphs = map[rune][compactH]string{
	'0': {"111", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "001", "001", "001"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
	'k': {"101", "110", "100", "110", "101"},
	'M': {"101", "111", "111", "101", "101"},
	'G': {"111", "100", "101", "101", "111"},
	'.': {"000", "000", "000", "000", "010"},
	'/': {"001", "001", "010", "100", "100"},
	'-': {"000", "000", "111", "000", "000"},
	':': {"000", "010", "000", "010", "000"},
	'%': {"101", "001", "010", "100", "101"},
	' ': {"000", "000", "000", "000", "000"},
}

// CompactTextWidth reports how many canvas units a string occupies at a given
// pixel size, with the one-pixel gap between glyphs.
func CompactTextWidth(s string, px float64) float64 {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return (float64(n*(compactW+1)) - 1) * px
}

// CompactTextHeight is the height of a line at a given pixel size.
func CompactTextHeight(px float64) float64 { return compactH * px }

// CompactText draws s with its top-left corner at x, y, where each font pixel is
// a px×px square. Unknown runes are drawn as blanks, so a stray character leaves
// a gap rather than a smear.
func (c *Canvas) CompactText(x, y, px float64, s string, v RGBA) {
	if px <= 0 || v.A == 0 {
		return
	}
	cx := x
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			// Only a few capitals are drawn; the rest of the alphabet is not
			// worth the table for a status figure.
			if _, ok := compactGlyphs[r]; !ok {
				r = r - 'A' + 'a'
			}
		}
		g, ok := compactGlyphs[r]
		if !ok {
			if upper, found := compactGlyphs[toUpper(r)]; found {
				g, ok = upper, true
			}
		}
		if !ok {
			cx += (compactW + 1) * px
			continue
		}
		for row := 0; row < compactH; row++ {
			line := g[row]
			for col := 0; col < compactW && col < len(line); col++ {
				if line[col] == '1' {
					c.Rect(cx+float64(col)*px, y+float64(row)*px, px, px, v)
				}
			}
		}
		cx += (compactW + 1) * px
	}
}

// toUpper maps a lower-case letter to the capital the table may hold.
func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	return r
}

// FitCompact returns the pixel size at which s exactly fills a box of the given
// width, capped so a one-character figure does not become a giant glyph.
func FitCompact(s string, boxWidth float64) float64 {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	units := float64(n*(compactW+1) - 1)
	return boxWidth / units
}
