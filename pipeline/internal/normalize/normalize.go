package normalize

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var legalSuffixes = []string{
	" international", " holdings", " group", " corporation", " corp",
	" incorporated", " inc", " limited", " ltd", " llc", " co",
	" sa", " ag", " plc", " gmbh", " bv", " nv", " ab",
	// Russian/CIS company forms
	" pjsc", " ojsc", " jsc", " cjsc", " pao", " oao", " zao",
}

// Company normalizes a company name for fuzzy comparison.
// Lowercases, strips accents, removes legal suffixes and punctuation.
func Company(s string) string {
	s = strings.ToLower(s)

	// strip Unicode accents: ē → e, é → e, etc.
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(t, s)

	// replace non-alphanumeric with single space (first pass, before suffix removal)
	s = collapseToWords(s)

	// remove legal suffixes in a loop (handles "international inc")
	for {
		changed := false
		for _, suffix := range legalSuffixes {
			// suffix has a leading space; trim it for comparison with words
			bare := strings.TrimPrefix(suffix, " ")
			words := strings.Fields(s)
			if len(words) > 0 && words[len(words)-1] == bare {
				s = strings.TrimSpace(strings.Join(words[:len(words)-1], " "))
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// remove trailing single-letter tokens (artifacts of forms like S.A., B.V., N.V.)
	for {
		words := strings.Fields(s)
		if len(words) > 1 && len([]rune(words[len(words)-1])) == 1 {
			s = strings.TrimSpace(strings.Join(words[:len(words)-1], " "))
		} else {
			break
		}
	}

	return strings.TrimSpace(s)
}

// collapseToWords replaces non-alphanumeric characters with spaces and
// collapses consecutive spaces to a single space.
func collapseToWords(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace && b.Len() > 0 {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// TokenSortRatio returns 0–100 similarity using token-sort trigram matching.
// Splits both strings into words, sorts words alphabetically, then computes
// trigram (3-character substring) Jaccard similarity. Word-order independent.
func TokenSortRatio(a, b string) int {
	return trigramSimilarity(sortTokens(a), sortTokens(b))
}

func sortTokens(s string) string {
	words := strings.Fields(s)
	// insertion sort — company names are short
	for i := 1; i < len(words); i++ {
		for j := i; j > 0 && words[j] < words[j-1]; j-- {
			words[j], words[j-1] = words[j-1], words[j]
		}
	}
	return strings.Join(words, " ")
}

func trigramSimilarity(a, b string) int {
	if a == b {
		return 100
	}
	setA := trigrams(a)
	setB := trigrams(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	return (intersection * 100) / union
}

func trigrams(s string) map[string]bool {
	chars := []rune(s)
	m := make(map[string]bool)
	for i := 0; i+2 < len(chars); i++ {
		m[string(chars[i:i+3])] = true
	}
	return m
}
