//go:build darwin && arm64 && cgo && mlx

package mlx

/*
#cgo CFLAGS: -DMLX_C_BINDINGS
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lmlx -lmlxc

#include <mlx/c/array.h>
#include <mlx/c/fast.h>
#include <mlx/c/ops.h>
#include <mlx/c/stream.h>

int gemma4_shim_geglu(mlx_array* out,
                             const mlx_array gate,
                             const mlx_array up,
                             const mlx_stream s) {
    mlx_array c044715 = mlx_array_new_float(0.044715f);
    mlx_array sqrt2pi = mlx_array_new_float(0.7978845608028654f);
    mlx_array one = mlx_array_new_float(1.0f);
    mlx_array half = mlx_array_new_float(0.5f);
    mlx_array three = mlx_array_new_float(3.0f);

    mlx_array x3 = {0}, tmp1 = {0}, tmp2 = {0}, inner = {0};
    mlx_array tanh_res = {0}, one_plus = {0}, half_x = {0}, gelu = {0};

    int rc = mlx_power(&x3, gate, three, s);
    if (rc) goto done;
    rc = mlx_multiply(&tmp1, c044715, x3, s);
    if (rc) goto done;
    rc = mlx_add(&tmp2, gate, tmp1, s);
    if (rc) goto done;
    rc = mlx_multiply(&inner, sqrt2pi, tmp2, s);
    if (rc) goto done;
    rc = mlx_tanh(&tanh_res, inner, s);
    if (rc) goto done;
    rc = mlx_add(&one_plus, one, tanh_res, s);
    if (rc) goto done;
    rc = mlx_multiply(&half_x, half, gate, s);
    if (rc) goto done;
    rc = mlx_multiply(&gelu, half_x, one_plus, s);
    if (rc) goto done;
    rc = mlx_multiply(out, gelu, up, s);

done:
    if (x3.ctx) mlx_array_free(x3);
    if (tmp1.ctx) mlx_array_free(tmp1);
    if (tmp2.ctx) mlx_array_free(tmp2);
    if (inner.ctx) mlx_array_free(inner);
    if (tanh_res.ctx) mlx_array_free(tanh_res);
    if (one_plus.ctx) mlx_array_free(one_plus);
    if (half_x.ctx) mlx_array_free(half_x);
    if (gelu.ctx) mlx_array_free(gelu);
    mlx_array_free(c044715);
    mlx_array_free(sqrt2pi);
    mlx_array_free(one);
    mlx_array_free(half);
    mlx_array_free(three);
    return rc;
}

int gemma4_shim_gelu(mlx_array* out,
                            const mlx_array x,
                            const mlx_stream s) {
    mlx_array c044715 = mlx_array_new_float(0.044715f);
    mlx_array sqrt2pi = mlx_array_new_float(0.7978845608028654f);
    mlx_array one = mlx_array_new_float(1.0f);
    mlx_array half = mlx_array_new_float(0.5f);
    mlx_array three = mlx_array_new_float(3.0f);

    mlx_array x3 = {0}, tmp1 = {0}, tmp2 = {0}, inner = {0};
    mlx_array tanh_res = {0}, one_plus = {0}, half_x = {0};

    int rc = mlx_power(&x3, x, three, s);
    if (rc) goto done;
    rc = mlx_multiply(&tmp1, c044715, x3, s);
    if (rc) goto done;
    rc = mlx_add(&tmp2, x, tmp1, s);
    if (rc) goto done;
    rc = mlx_multiply(&inner, sqrt2pi, tmp2, s);
    if (rc) goto done;
    rc = mlx_tanh(&tanh_res, inner, s);
    if (rc) goto done;
    rc = mlx_add(&one_plus, one, tanh_res, s);
    if (rc) goto done;
    rc = mlx_multiply(&half_x, half, x, s);
    if (rc) goto done;
    rc = mlx_multiply(out, half_x, one_plus, s);

done:
    if (x3.ctx) mlx_array_free(x3);
    if (tmp1.ctx) mlx_array_free(tmp1);
    if (tmp2.ctx) mlx_array_free(tmp2);
    if (inner.ctx) mlx_array_free(inner);
    if (tanh_res.ctx) mlx_array_free(tanh_res);
    if (one_plus.ctx) mlx_array_free(one_plus);
    if (half_x.ctx) mlx_array_free(half_x);
    mlx_array_free(c044715);
    mlx_array_free(sqrt2pi);
    mlx_array_free(one);
    mlx_array_free(half);
    mlx_array_free(three);
    return rc;
}
*/
import "C"

// Gemma4GeGLU computes gelu_tanh_approx(gate) * up using a single C function
// that chains all MLX C API calls internally. Replaces ~15 separate CGO
// crossings with 1. Uses standard MLX ops so lazy evaluation works correctly.
func Gemma4GeGLU(gate, up *Array, s *Stream) (*Array, error) {
	var out C.mlx_array
	rc := C.gemma4_shim_geglu(&out, gate.cHandle(), up.cHandle(), s.cHandle())
	return wrapResult(out, rc, "gemma4_geglu")
}

// Gemma4GELU computes gelu_tanh_approx(x) using a single C function.
func Gemma4GELU(x *Array, s *Stream) (*Array, error) {
	var out C.mlx_array
	rc := C.gemma4_shim_gelu(&out, x.cHandle(), s.cHandle())
	return wrapResult(out, rc, "gemma4_gelu")
}
