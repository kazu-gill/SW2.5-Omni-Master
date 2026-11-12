package rag

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"

	"github.com/kf/sw25-omni-master/pkg/db"
)

// The term index finds rules by name regardless of how the name is written:
// 「庇う」「かばう」「庇います」 all read カバウ once each word is put back to its
// base form. Game terms (特技・魔法・アイテム・魔物) are looked up by reading and
// their defining chunk is always included in the search result — exact keys
// beat similarity for named things (the lesson of the MLX platform's Kappa).

var (
	tokOnce sync.Once
	tok     *tokenizer.Tokenizer
)

func tokenizerJA() *tokenizer.Tokenizer {
	tokOnce.Do(func() {
		t, err := tokenizer.New(ipa.Dict(), tokenizer.OmitBosEos())
		if err == nil {
			tok = t
		}
	})
	return tok
}

// reading returns the katakana reading of text with every word in its base
// form, so conjugation does not matter (庇い → 庇う → カバウ).
func reading(text string) string {
	t := tokenizerJA()
	if t == nil {
		return ""
	}
	var b strings.Builder
	for _, token := range t.Tokenize(text) {
		base := token.Surface
		if f, ok := token.BaseForm(); ok && f != "" && f != "*" {
			base = f
		}
		b.WriteString(wordReading(t, base))
	}
	return b.String()
}

func wordReading(t *tokenizer.Tokenizer, word string) string {
	var b strings.Builder
	for _, token := range t.Tokenize(word) {
		if r, ok := token.Reading(); ok && r != "" && r != "*" {
			b.WriteString(r)
		} else {
			b.WriteString(toKatakana(token.Surface))
		}
	}
	return b.String()
}

func toKatakana(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'ぁ' && r <= 'ゖ' {
			return r + ('ァ' - 'ぁ')
		}
		return r
	}, s)
}

var (
	bracketTermRe = regexp.MustCompile(`[《【〈]([^》】〉]{1,30})[》】〉]`)
	termNoiseRe   = regexp.MustCompile(`（[ぁ-ゖー]+）|[ⅠⅡⅢⅣⅤⅥⅦⅧⅨⅩ]|[IVX]+$|\d+$|[／/]\*+|\s|・`)
)

// normalizeTerm strips what varies between mentions of the same term: ruby,
// rank numerals (《かばうⅠ》 → かばう), wildcards and middle dots.
func normalizeTerm(s string) string {
	return termNoiseRe.ReplaceAllString(s, "")
}

// termIndex maps a term's reading to the chunks that define it.
type termIndex struct {
	terms []indexedTerm // longest reading first
}

type indexedTerm struct {
	name    string
	reading string
	chunks  []int64
}

// minTermReading keeps short readings (カバ, ケン) from matching everywhere.
const minTermReading = 3

// buildTermIndex reads the defining chunks: those whose section ends in a
// bracketed term, e.g. 「… > 選択習得の宣言特技 > 《かばうⅠ》」.
func buildTermIndex(chunks []db.RAGChunk) *termIndex {
	byReading := map[string]*indexedTerm{}
	for _, c := range chunks {
		if !c.Enabled || c.Section == "" {
			continue
		}
		parts := strings.Split(c.Section, " > ")
		last := parts[len(parts)-1]
		for _, m := range bracketTermRe.FindAllStringSubmatch(last, -1) {
			name := normalizeTerm(m[1])
			r := reading(name)
			if len([]rune(r)) < minTermReading || !hasKana(r) {
				continue
			}
			it := byReading[r]
			if it == nil {
				it = &indexedTerm{name: name, reading: r}
				byReading[r] = it
			}
			it.chunks = append(it.chunks, c.ID)
		}
	}
	idx := &termIndex{}
	for _, it := range byReading {
		idx.terms = append(idx.terms, *it)
	}
	sort.Slice(idx.terms, func(i, j int) bool {
		li, lj := len([]rune(idx.terms[i].reading)), len([]rune(idx.terms[j].reading))
		if li != lj {
			return li > lj
		}
		return idx.terms[i].reading < idx.terms[j].reading
	})
	return idx
}

func hasKana(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Katakana) {
			return true
		}
	}
	return false
}

// termMatch is a term found in a query.
type termMatch struct {
	name    string
	chunkID int64
}

// match returns the defining chunks of the terms the query mentions, longest
// term first, at most limit chunks. A term inside a longer matched term is
// not counted again.
func (idx *termIndex) match(query string, limit int) []termMatch {
	q := reading(normalizeTerm(query))
	if q == "" {
		return nil
	}
	var out []termMatch
	var used []string
	for _, t := range idx.terms {
		if !strings.Contains(q, t.reading) {
			continue
		}
		inside := false
		for _, u := range used {
			if strings.Contains(u, t.reading) {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		used = append(used, t.reading)
		for _, id := range t.chunks {
			out = append(out, termMatch{name: t.name, chunkID: id})
			if len(out) == limit {
				return out
			}
		}
	}
	return out
}
