package cache

import (
	"math"
)

// CosineSimilarity computes the cosine similarity between two vectors: (A · B) / (||A|| * ||B||)
// Returns a value between -1.0 and 1.0 (1.0 means identical angle/meaning).
func CosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	sim := dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
	// Clamp to [-1, 1] to avoid float precision overflow
	if sim > 1.0 {
		return 1.0
	}
	if sim < -1.0 {
		return -1.0
	}
	return sim
}

// NormalizeVector normalizes a vector in-place to unit length
func NormalizeVector(v []float64) []float64 {
	var sum float64
	for _, val := range v {
		sum += val * val
	}
	if sum == 0 {
		return v
	}
	norm := math.Sqrt(sum)
	res := make([]float64, len(v))
	for i, val := range v {
		res[i] = val / norm
	}
	return res
}
