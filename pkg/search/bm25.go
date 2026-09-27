package search

import (
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	DefaultK1 = 1.2
	DefaultB  = 0.75
)

var stopWords = map[string]bool{
	"a": true, "about": true, "above": true, "after": true, "again": true, "against": true, "all": true,
	"am": true, "an": true, "and": true, "any": true, "are": true, "as": true, "at": true, "be": true,
	"because": true, "been": true, "before": true, "being": true, "below": true, "between": true, "both": true,
	"but": true, "by": true, "did": true, "do": true, "does": true, "doing": true, "down": true, "during": true,
	"each": true, "few": true, "for": true, "from": true, "further": true, "had": true, "has": true, "have": true,
	"having": true, "he": true, "her": true, "here": true, "hers": true, "herself": true, "him": true, "himself": true,
	"his": true, "how": true, "i": true, "if": true, "in": true, "into": true, "is": true, "it": true, "its": true,
	"itself": true, "just": true, "me": true, "more": true, "most": true, "my": true, "myself": true, "no": true,
	"nor": true, "not": true, "now": true, "of": true, "off": true, "on": true, "once": true, "only": true, "or": true,
	"other": true, "our": true, "ours": true, "ourselves": true, "out": true, "over": true, "own": true, "same": true,
	"she": true, "should": true, "so": true, "some": true, "such": true, "than": true, "that": true, "the": true,
	"their": true, "theirs": true, "them": true, "themselves": true, "then": true, "there": true, "these": true,
	"they": true, "this": true, "those": true, "through": true, "to": true, "too": true, "under": true, "until": true,
	"up": true, "very": true, "was": true, "we": true, "were": true, "what": true, "when": true, "where": true,
	"which": true, "while": true, "who": true, "whom": true, "why": true, "with": true, "you": true, "your": true,
	"yours": true, "yourself": true, "yourselves": true,
}

func Tokenize(text string) []string {
	var tokens []string
	var word strings.Builder

	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else {
			if word.Len() > 0 {
				w := word.String()
				if !stopWords[w] && len(w) > 1 {
					tokens = append(tokens, w)
				}
				word.Reset()
			}
		}
	}
	if word.Len() > 0 {
		w := word.String()
		if !stopWords[w] && len(w) > 1 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

type Posting struct {
	DocID uint64
	TF    int
}

type BM25Result struct {
	DocID uint64
	Score float64
}

type BM25Index struct {
	mu           sync.RWMutex
	k1           float64
	b            float64
	invertedIdx  map[string][]Posting
	docLengths   map[uint64]int
	docRawText   map[uint64]string
	totalDocLen  int
	totalDocs    int
}

func NewBM25Index() *BM25Index {
	return &BM25Index{
		k1:          DefaultK1,
		b:           DefaultB,
		invertedIdx: make(map[string][]Posting),
		docLengths:  make(map[uint64]int),
		docRawText:  make(map[uint64]string),
	}
}

func (idx *BM25Index) IndexDocument(docID uint64, text string) {
	tokens := Tokenize(text)
	docLen := len(tokens)
	if docLen == 0 {
		return
	}

	tfMap := make(map[string]int)
	for _, t := range tokens {
		tfMap[t]++
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// If document already indexed, subtract old length
	if oldLen, exists := idx.docLengths[docID]; exists {
		idx.totalDocLen -= oldLen
		idx.totalDocs--
	}

	idx.docLengths[docID] = docLen
	idx.docRawText[docID] = text
	idx.totalDocLen += docLen
	idx.totalDocs++

	for term, count := range tfMap {
		postings := idx.invertedIdx[term]
		found := false
		for i := range postings {
			if postings[i].DocID == docID {
				postings[i].TF = count
				found = true
				break
			}
		}
		if !found {
			idx.invertedIdx[term] = append(postings, Posting{DocID: docID, TF: count})
		}
	}
}

func (idx *BM25Index) Search(query string, topK int) []BM25Result {
	tokens := Tokenize(query)
	if len(tokens) == 0 || topK <= 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.totalDocs == 0 {
		return nil
	}

	avgdl := float64(idx.totalDocLen) / float64(idx.totalDocs)
	docScores := make(map[uint64]float64)

	for _, token := range tokens {
		postings, exists := idx.invertedIdx[token]
		if !exists || len(postings) == 0 {
			continue
		}

		docFreq := len(postings)
		// Robertson-Spärck Jones IDF
		idf := math.Log(1.0 + (float64(idx.totalDocs)-float64(docFreq)+0.5)/(float64(docFreq)+0.5))

		for _, p := range postings {
			docLen := float64(idx.docLengths[p.DocID])
			tf := float64(p.TF)

			num := tf * (idx.k1 + 1.0)
			denom := tf + idx.k1*(1.0-idx.b+idx.b*(docLen/avgdl))
			docScores[p.DocID] += idf * (num / denom)
		}
	}

	var results []BM25Result
	for docID, score := range docScores {
		if score > 0 {
			results = append(results, BM25Result{DocID: docID, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

func (idx *BM25Index) GetRawText(docID uint64) (string, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	txt, ok := idx.docRawText[docID]
	return txt, ok
}
