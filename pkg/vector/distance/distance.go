package distance

import (
	"math"
)

type MetricType int

const (
	L2 MetricType = iota
	Cosine
	DotProduct
)

func L2Distance(a, b []float32) float32 {
	n := len(a)
	if n != len(b) || n == 0 {
		return 0
	}

	var sum0, sum1, sum2, sum3, sum4, sum5, sum6, sum7 float32
	i := 0

	// 8-way unrolled loop for SIMD auto-vectorization
	for i+8 <= n {
		d0 := a[i+0] - b[i+0]
		d1 := a[i+1] - b[i+1]
		d2 := a[i+2] - b[i+2]
		d3 := a[i+3] - b[i+3]
		d4 := a[i+4] - b[i+4]
		d5 := a[i+5] - b[i+5]
		d6 := a[i+6] - b[i+6]
		d7 := a[i+7] - b[i+7]

		sum0 += d0 * d0
		sum1 += d1 * d1
		sum2 += d2 * d2
		sum3 += d3 * d3
		sum4 += d4 * d4
		sum5 += d5 * d5
		sum6 += d6 * d6
		sum7 += d7 * d7

		i += 8
	}

	total := (sum0 + sum1) + (sum2 + sum3) + (sum4 + sum5) + (sum6 + sum7)

	for ; i < n; i++ {
		d := a[i] - b[i]
		total += d * d
	}

	return float32(math.Sqrt(float64(total)))
}

func DotProductDistance(a, b []float32) float32 {
	n := len(a)
	if n != len(b) || n == 0 {
		return 0
	}

	var sum0, sum1, sum2, sum3, sum4, sum5, sum6, sum7 float32
	i := 0

	for i+8 <= n {
		sum0 += a[i+0] * b[i+0]
		sum1 += a[i+1] * b[i+1]
		sum2 += a[i+2] * b[i+2]
		sum3 += a[i+3] * b[i+3]
		sum4 += a[i+4] * b[i+4]
		sum5 += a[i+5] * b[i+5]
		sum6 += a[i+6] * b[i+6]
		sum7 += a[i+7] * b[i+7]
		i += 8
	}

	dot := (sum0 + sum1) + (sum2 + sum3) + (sum4 + sum5) + (sum6 + sum7)

	for ; i < n; i++ {
		dot += a[i] * b[i]
	}

	return -dot // Inverted so smaller is better/closer
}

func CosineDistance(a, b []float32) float32 {
	n := len(a)
	if n != len(b) || n == 0 {
		return 0
	}

	var dot, normA, normB float32
	var d0, d1, d2, d3 float32
	var na0, na1, na2, na3 float32
	var nb0, nb1, nb2, nb3 float32

	i := 0
	for i+4 <= n {
		va0, va1, va2, va3 := a[i], a[i+1], a[i+2], a[i+3]
		vb0, vb1, vb2, vb3 := b[i], b[i+1], b[i+2], b[i+3]

		d0 += va0 * vb0
		d1 += va1 * vb1
		d2 += va2 * vb2
		d3 += va3 * vb3

		na0 += va0 * va0
		na1 += va1 * va1
		na2 += va2 * va2
		na3 += va3 * va3

		nb0 += vb0 * vb0
		nb1 += vb1 * vb1
		nb2 += vb2 * vb2
		nb3 += vb3 * vb3

		i += 4
	}

	dot = d0 + d1 + d2 + d3
	normA = na0 + na1 + na2 + na3
	normB = nb0 + nb1 + nb2 + nb3

	for ; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	denom := math.Sqrt(float64(normA)) * math.Sqrt(float64(normB))
	if denom == 0 {
		return 1.0
	}
	return float32(1.0 - (float64(dot) / denom))
}
