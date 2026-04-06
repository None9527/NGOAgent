package tool

import (
	"strings"
	"unicode/utf8"
)

// Unicode normalization map (ported from Gemini-CLI editHelper.ts)
// Maps visually-similar Unicode characters to their ASCII equivalents.
var unicodeNormMap = map[rune]rune{
	// Hyphen/dash variations → ASCII hyphen-minus
	'\u2010': '-', '\u2011': '-', '\u2012': '-', '\u2013': '-',
	'\u2014': '-', '\u2015': '-', '\u2212': '-',
	// Curly single quotes → straight apostrophe
	'\u2018': '\'', '\u2019': '\'', '\u201A': '\'', '\u201B': '\'',
	// Curly double quotes → straight double quote
	'\u201C': '"', '\u201D': '"', '\u201E': '"', '\u201F': '"',
	// Whitespace variants → normal space
	'\u00A0': ' ', '\u2002': ' ', '\u2003': ' ', '\u2004': ' ',
	'\u2005': ' ', '\u2006': ' ', '\u2007': ' ', '\u2008': ' ',
	'\u2009': ' ', '\u200A': ' ', '\u202F': ' ', '\u205F': ' ',
	'\u3000': ' ',
}

// normalizeUnicode replaces common Unicode lookalikes with ASCII equivalents.
func normalizeUnicode(s string) string {
	needsNormalization := false
	for _, r := range s {
		if _, ok := unicodeNormMap[r]; ok {
			needsNormalization = true
			break
		}
	}
	if !needsNormalization {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if mapped, ok := unicodeNormMap[r]; ok {
			b.WriteRune(mapped)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ──────────────────────────────────────────────────────
// L1: Unicode normalized match (Gemini-CLI style)
// ──────────────────────────────────────────────────────

// unicodeNormMatch tries to find oldStr in content after normalizing unicode chars.
// Returns the matched slice from the original content, or empty string if not found.
func unicodeNormMatch(content, oldStr string) string {
	normContent := normalizeUnicode(content)
	normOld := normalizeUnicode(oldStr)
	idx := strings.Index(normContent, normOld)
	if idx < 0 {
		return ""
	}
	// Map byte offset back: since normalization is 1:1 rune replacement
	// and all replacements are single-byte ASCII, we need to compute
	// the correct byte range in the original content.
	return mapNormalizedRange(content, normContent, idx, len(normOld))
}

// mapNormalizedRange maps a byte range from normalizedContent back to orgContent.
func mapNormalizedRange(original, normalized string, normStart, normLen int) string {
	// Build rune-offset mapping
	origRunes := []rune(original)
	normRunes := []rune(normalized)
	if len(origRunes) != len(normRunes) {
		// Lengths should match since we only do 1:1 rune replacement
		return ""
	}

	// Convert byte offset in normalized to rune index
	runeStart := utf8.RuneCountInString(normalized[:normStart])
	runeEnd := utf8.RuneCountInString(normalized[:normStart+normLen])

	// Convert rune indices back to byte offsets in original
	byteStart := 0
	for i := 0; i < runeStart && i < len(origRunes); i++ {
		byteStart += utf8.RuneLen(origRunes[i])
	}
	byteEnd := byteStart
	for i := runeStart; i < runeEnd && i < len(origRunes); i++ {
		byteEnd += utf8.RuneLen(origRunes[i])
	}

	if byteEnd > len(original) {
		return ""
	}
	return original[byteStart:byteEnd]
}

// ──────────────────────────────────────────────────────
// L2: Line-based trimEnd match (Gemini-CLI 3-pass style)
// ──────────────────────────────────────────────────────

type lineTransform func(string) string

// lineBasedMatch tries 3 progressively relaxed comparison passes.
// Returns the matched slice from the original content.
func lineBasedMatch(content, oldStr string) string {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldStr, "\n")

	passes := []lineTransform{
		func(s string) string { return s },                                               // exact
		func(s string) string { return strings.TrimRight(s, " \t\r") },                   // trimEnd
		func(s string) string { return strings.TrimRight(normalizeUnicode(s), " \t\r") }, // normalize+trimEnd
	}

	// Try with original lines, then with trailing empty line removed
	candidates := [][]string{oldLines}
	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" {
		candidates = append(candidates, oldLines[:len(oldLines)-1])
	}

	for _, candidate := range candidates {
		if len(candidate) == 0 {
			continue
		}
		for _, transform := range passes {
			idx := seekSequence(contentLines, candidate, transform)
			if idx >= 0 {
				return sliceOriginalLines(contentLines, idx, len(candidate))
			}
		}
	}
	return ""
}

// seekSequence finds the first index where pattern appears within lines
// after applying the given transform to both.
func seekSequence(lines, pattern []string, transform lineTransform) int {
	if len(pattern) > len(lines) {
		return -1
	}
	for i := 0; i <= len(lines)-len(pattern); i++ {
		match := true
		for p := 0; p < len(pattern); p++ {
			if transform(lines[i+p]) != transform(pattern[p]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// sliceOriginalLines reconstructs the original text for matched lines.
func sliceOriginalLines(lines []string, start, count int) string {
	if start+count > len(lines) {
		return ""
	}
	matched := lines[start : start+count]
	return strings.Join(matched, "\n")
}

// ──────────────────────────────────────────────────────
// L3: Block anchor match (Cline style)
// ──────────────────────────────────────────────────────

// blockAnchorMatch uses first+last line as anchors to find matching block.
// Only works for blocks with ≥3 lines.
func blockAnchorMatch(content, oldStr string) string {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldStr, "\n")

	// Trim trailing empty line
	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" {
		oldLines = oldLines[:len(oldLines)-1]
	}

	if len(oldLines) < 3 {
		return ""
	}

	first := strings.TrimSpace(oldLines[0])
	last := strings.TrimSpace(oldLines[len(oldLines)-1])
	blockSize := len(oldLines)

	if first == "" || last == "" {
		return "" // empty anchors are too ambiguous
	}

	for i := 0; i <= len(contentLines)-blockSize; i++ {
		if strings.TrimSpace(contentLines[i]) != first {
			continue
		}
		if strings.TrimSpace(contentLines[i+blockSize-1]) != last {
			continue
		}
		return sliceOriginalLines(contentLines, i, blockSize)
	}
	return ""
}

// ──────────────────────────────────────────────────────
// FEEDBACK: find_similar_lines (Aider style)
// Returns the most similar chunk for error feedback.
// ──────────────────────────────────────────────────────

// findSimilarLines scans content for the region most similar to oldStr.
// Returns a context snippet with surrounding lines, or empty if similarity < threshold.
func findSimilarLines(content, oldStr string, threshold float64) string {
	searchLines := strings.Split(oldStr, "\n")
	contentLines := strings.Split(content, "\n")

	if len(searchLines) == 0 || len(contentLines) == 0 {
		return ""
	}

	bestRatio := 0.0
	bestIdx := 0

	for i := 0; i <= len(contentLines)-len(searchLines); i++ {
		chunk := contentLines[i : i+len(searchLines)]
		ratio := lineSimilarity(searchLines, chunk)
		if ratio > bestRatio {
			bestRatio = ratio
			bestIdx = i
		}
	}

	if bestRatio < threshold {
		return ""
	}

	// Extend with 3 lines context each direction
	ctxN := 3
	start := bestIdx - ctxN
	if start < 0 {
		start = 0
	}
	end := bestIdx + len(searchLines) + ctxN
	if end > len(contentLines) {
		end = len(contentLines)
	}

	return strings.Join(contentLines[start:end], "\n")
}

// lineSimilarity computes a normalized similarity ratio between two line slices.
// Uses longest common subsequence of stripped lines as the basis.
func lineSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	total := len(a) + len(b)
	if total == 0 {
		return 0
	}

	// Count matching lines (order-sensitive, like SequenceMatcher)
	matches := 0
	used := make([]bool, len(b))
	for _, lineA := range a {
		strippedA := strings.TrimSpace(lineA)
		for j, lineB := range b {
			if !used[j] && strings.TrimSpace(lineB) == strippedA {
				matches++
				used[j] = true
				break
			}
		}
	}

	return float64(2*matches) / float64(total)
}

// ──────────────────────────────────────────────────────
// cascadeFuzzyMatch runs L1→L2→L3 cascade.
// Returns the exact slice from content that should replace oldStr,
// or empty string if nothing matched.
// ──────────────────────────────────────────────────────

func cascadeFuzzyMatch(content, oldStr string) string {
	// L1: Unicode normalization
	if match := unicodeNormMatch(content, oldStr); match != "" {
		return match
	}

	// L2: Line-based trimEnd match
	if match := lineBasedMatch(content, oldStr); match != "" {
		return match
	}

	// L3: Block anchor match
	if match := blockAnchorMatch(content, oldStr); match != "" {
		return match
	}

	return ""
}
