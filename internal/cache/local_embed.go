package cache

import (
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

const VectorDimension = 256

// LocalEmbeddingEngine generates fast, zero-dependency, normalized vector representations
// for arbitrary text using token hashing and character 3-grams (MinHash / Feature Hashing)
type LocalEmbeddingEngine struct {
	dimension int
}

// NewLocalEmbeddingEngine creates a new local embedding generator
func NewLocalEmbeddingEngine() *LocalEmbeddingEngine {
	return &LocalEmbeddingEngine{
		dimension: VectorDimension,
	}
}

// EmbedText converts a text prompt into a unit-normalized 256-dimensional float64 vector
func (e *LocalEmbeddingEngine) EmbedText(text string) []float64 {
	vec := make([]float64, e.dimension)
	if len(text) == 0 {
		return vec
	}

	// 1. Lowercase and tokenize words
	clean := strings.ToLower(text)
	words := strings.FieldsFunc(clean, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})

	// 2. Hash whole words into vector buckets
	for _, w := range words {
		if len(w) == 0 {
			continue
		}
		idx, sign := e.hashFeature(w)
		vec[idx] += sign * 1.5 // weight whole words higher
	}

	// 3. Hash character 3-grams for typo and inflection tolerance
	runes := []rune(clean)
	if len(runes) >= 3 {
		for i := 0; i <= len(runes)-3; i++ {
			gram := string(runes[i : i+3])
			idx, sign := e.hashFeature("g:" + gram)
			vec[idx] += sign * 0.8
		}
	}

	// 4. L2 Normalization (unit length)
	var sumSquares float64
	for _, val := range vec {
		sumSquares += val * val
	}

	if sumSquares > 0 {
		norm := math.Sqrt(sumSquares)
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// hashFeature maps a string feature to an index [0, dimension) and a sign (+1 or -1)
func (e *LocalEmbeddingEngine) hashFeature(feature string) (int, float64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(feature))
	val := h.Sum64()

	index := int(val % uint64(e.dimension))
	sign := 1.0
	if (val & (1 << 63)) != 0 {
		sign = -1.0
	}
	return index, sign
}
