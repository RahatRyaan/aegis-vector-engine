package pq

import (
	"errors"
	"math/rand"
	"project-x/pkg/vector/distance"
)

var (
	ErrInvalidDimensions = errors.New("pq: dimension must be divisible by numSubVectors")
	ErrInsufficientData  = errors.New("pq: need more vectors than clusters to train")
)

type ProductQuantizer struct {
	Dim           int
	NumSubVectors int
	SubDim        int
	NumClusters   int // 256 for 8-bit quantization
	Codebooks     [][][]float32 // [subVectorIdx][clusterIdx][subDim]
}

func NewProductQuantizer(dim, numSubVectors int) (*ProductQuantizer, error) {
	if dim%numSubVectors != 0 {
		return nil, ErrInvalidDimensions
	}
	return &ProductQuantizer{
		Dim:           dim,
		NumSubVectors: numSubVectors,
		SubDim:        dim / numSubVectors,
		NumClusters:   256,
		Codebooks:     make([][][]float32, numSubVectors),
	}, nil
}

func (pq *ProductQuantizer) Train(vectors [][]float32, maxIters int) error {
	if len(vectors) < pq.NumClusters {
		return ErrInsufficientData
	}
	if maxIters <= 0 {
		maxIters = 25
	}

	for m := 0; m < pq.NumSubVectors; m++ {
		startIdx := m * pq.SubDim
		endIdx := startIdx + pq.SubDim

		subVectors := make([][]float32, len(vectors))
		for i, v := range vectors {
			subVectors[i] = v[startIdx:endIdx]
		}

		centroids := trainKMeans(subVectors, pq.NumClusters, pq.SubDim, maxIters)
		pq.Codebooks[m] = centroids
	}

	return nil
}

func trainKMeans(data [][]float32, k, subDim, maxIters int) [][]float32 {
	centroids := make([][]float32, k)
	perm := rand.Perm(len(data))
	for i := 0; i < k; i++ {
		centroids[i] = make([]float32, subDim)
		copy(centroids[i], data[perm[i]])
	}

	assignments := make([]int, len(data))
	for iter := 0; iter < maxIters; iter++ {
		changed := false
		for i, v := range data {
			bestIdx := 0
			minDist := float32(1e30)
			for cIdx, c := range centroids {
				d := distance.L2Distance(v, c)
				if d < minDist {
					minDist = d
					bestIdx = cIdx
				}
			}
			if assignments[i] != bestIdx {
				assignments[i] = bestIdx
				changed = true
			}
		}

		if !changed && iter > 0 {
			break
		}

		// Recompute centroids
		counts := make([]int, k)
		newCentroids := make([][]float32, k)
		for i := 0; i < k; i++ {
			newCentroids[i] = make([]float32, subDim)
		}

		for i, v := range data {
			cIdx := assignments[i]
			counts[cIdx]++
			for d := 0; d < subDim; d++ {
				newCentroids[cIdx][d] += v[d]
			}
		}

		for i := 0; i < k; i++ {
			if counts[i] > 0 {
				for d := 0; d < subDim; d++ {
					centroids[i][d] = newCentroids[i][d] / float32(counts[i])
				}
			}
		}
	}

	return centroids
}

func (pq *ProductQuantizer) Encode(vector []float32) []byte {
	code := make([]byte, pq.NumSubVectors)

	for m := 0; m < pq.NumSubVectors; m++ {
		start := m * pq.SubDim
		subVec := vector[start : start+pq.SubDim]

		bestCluster := 0
		minDist := float32(1e30)
		for cIdx, centroid := range pq.Codebooks[m] {
			d := distance.L2Distance(subVec, centroid)
			if d < minDist {
				minDist = d
				bestCluster = cIdx
			}
		}
		code[m] = byte(bestCluster)
	}

	return code
}

func (pq *ProductQuantizer) PrecomputeDistanceTable(query []float32) [][]float32 {
	table := make([][]float32, pq.NumSubVectors)
	for m := 0; m < pq.NumSubVectors; m++ {
		table[m] = make([]float32, pq.NumClusters)
		start := m * pq.SubDim
		subQuery := query[start : start+pq.SubDim]

		for cIdx, centroid := range pq.Codebooks[m] {
			table[m][cIdx] = distance.L2Distance(subQuery, centroid)
		}
	}
	return table
}

func AsymmetricDistance(table [][]float32, code []byte) float32 {
	var total float32
	for m, c := range code {
		total += table[m][int(c)]
	}
	return total
}
