package ai

import (
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

type EmbeddingGenerator struct {
	Dim int
}

func NewEmbeddingGenerator(dim int) *EmbeddingGenerator {
	if dim <= 0 {
		dim = 128
	}
	return &EmbeddingGenerator{Dim: dim}
}

// GenerateEmbedding generates a normalized dense embedding vector from text using n-gram feature hashing
func (eg *EmbeddingGenerator) GenerateEmbedding(text string) []float32 {
	vec := make([]float32, eg.Dim)
	clean := strings.ToLower(strings.TrimSpace(text))
	if len(clean) == 0 {
		return vec
	}

	words := strings.FieldsFunc(clean, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	// 1. Unigrams
	for _, w := range words {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		idx := int(h.Sum32()) % eg.Dim
		vec[idx] += 1.0
	}

	// 2. Bigrams & Trigrams for semantic phrase capture
	for i := 0; i < len(words)-1; i++ {
		bigram := words[i] + "_" + words[i+1]
		h := fnv.New32a()
		_, _ = h.Write([]byte(bigram))
		idx := int(h.Sum32()) % eg.Dim
		vec[idx] += 1.5
	}
	for i := 0; i < len(words)-2; i++ {
		trigram := words[i] + "_" + words[i+1] + "_" + words[i+2]
		h := fnv.New32a()
		_, _ = h.Write([]byte(trigram))
		idx := int(h.Sum32()) % eg.Dim
		vec[idx] += 2.0
	}

	// 3. Substring Character N-grams (captures sub-word morphology)
	runes := []rune(clean)
	for i := 0; i < len(runes)-3; i++ {
		charGram := string(runes[i : i+3])
		h := fnv.New32a()
		_, _ = h.Write([]byte(charGram))
		idx := int(h.Sum32()) % eg.Dim
		vec[idx] += 0.5
	}

	// L2-normalize vector to unit length
	var sumSquares float32
	for _, v := range vec {
		sumSquares += v * v
	}
	norm := float32(math.Sqrt(float64(sumSquares)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

func ChunkText(text string, chunkSize, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 250
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 50
	}

	words := strings.Fields(text)
	if len(words) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	step := chunkSize - overlap

	for i := 0; i < len(words); i += step {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
		if end == len(words) {
			break
		}
	}

	return chunks
}
