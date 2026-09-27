#include <cstddef>
#include <cmath>

#if defined(__x86_64__) || defined(_M_X64)
#include <immintrin.h>
#endif

extern "C" {

float l2_distance_avx512(const float* a, const float* b, size_t dim) {
    float total = 0.0f;
    size_t i = 0;

#if defined(__AVX512F__)
    __m512 sum512 = _mm512_setzero_ps();
    for (; i + 16 <= dim; i += 16) {
        __m512 va = _mm512_loadu_ps(a + i);
        __m512 vb = _mm512_loadu_ps(b + i);
        __m512 diff = _mm512_sub_ps(va, vb);
        sum512 = _mm512_fmadd_ps(diff, diff, sum512);
    }
    total += _mm512_reduce_add_ps(sum512);
#elif defined(__AVX2__)
    __m256 sum256 = _mm256_setzero_ps();
    for (; i + 8 <= dim; i += 8) {
        __m256 va = _mm256_loadu_ps(a + i);
        __m256 vb = _mm256_loadu_ps(b + i);
        __m256 diff = _mm256_sub_ps(va, vb);
        sum256 = _mm256_fmadd_ps(diff, diff, sum256);
    }
    float buffer[8];
    _mm256_storeu_ps(buffer, sum256);
    for (int k = 0; k < 8; ++k) total += buffer[k];
#endif

    for (; i < dim; ++i) {
        float diff = a[i] - b[i];
        total += diff * diff;
    }

    return std::sqrt(total);
}

float dot_product_avx512(const float* a, const float* b, size_t dim) {
    float total = 0.0f;
    size_t i = 0;

#if defined(__AVX512F__)
    __m512 sum512 = _mm512_setzero_ps();
    for (; i + 16 <= dim; i += 16) {
        __m512 va = _mm512_loadu_ps(a + i);
        __m512 vb = _mm512_loadu_ps(b + i);
        sum512 = _mm512_fmadd_ps(va, vb, sum512);
    }
    total += _mm512_reduce_add_ps(sum512);
#elif defined(__AVX2__)
    __m256 sum256 = _mm256_setzero_ps();
    for (; i + 8 <= dim; i += 8) {
        __m256 va = _mm256_loadu_ps(a + i);
        __m256 vb = _mm256_loadu_ps(b + i);
        sum256 = _mm256_fmadd_ps(va, vb, sum256);
    }
    float buffer[8];
    _mm256_storeu_ps(buffer, sum256);
    for (int k = 0; k < 8; ++k) total += buffer[k];
#endif

    for (; i < dim; ++i) {
        total += a[i] * b[i];
    }

    return total;
}

}
