//go:build (darwin || linux) && arm64 && cgo && ggml

// Package ggml provides CGO bindings to the GGML tensor library, enabling
// GPU-accelerated compute on Metal (macOS), CUDA (NVIDIA), ROCm (AMD), and
// Vulkan (portable) behind one C API.
//
// GGML uses lazy graph evaluation: ops build a ggml_cgraph that is run in a
// single batch by the backend. This package bridges GGML's graph model to
// the tensor.Backend interface's eager semantics by accumulating ops into a
// graph and flushing on Eval().
package ggml

/*
#cgo darwin CFLAGS: -I/opt/homebrew/include
#cgo linux CFLAGS: -I/usr/local/include
#cgo darwin LDFLAGS: -L/opt/homebrew/lib -lggml -lggml-base -framework Metal -framework Foundation
#cgo linux LDFLAGS: -L/usr/local/lib -lggml -lggml-base

#define _GNU_SOURCE
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <ggml.h>
#include <ggml-backend.h>
#include <ggml-alloc.h>

#include <dlfcn.h>

// The CPU backend is dlopened by ggml_backend_load_all, so its feature
// predicates are not available at link time. Returns -1 when unresolved.
int ggml_feat(const char * name) {
    int (*fn)(void) = (int (*)(void)) dlsym(RTLD_DEFAULT, name);
    return fn ? fn() : -1;
}

// Create init params.
struct ggml_init_params ggml_make_params(size_t mem_size) {
    struct ggml_init_params params = { .mem_size = mem_size, .mem_buffer = NULL, .no_alloc = true };
    return params;
}

// Load backends and init best.
ggml_backend_t ggml_init_best() {
    ggml_backend_load_all();
    return ggml_backend_init_best();
}

const char * backend_name(ggml_backend_t b) { return ggml_backend_name(b); }

// Create a float32 2D tensor.
struct ggml_tensor * new_f32_2d(struct ggml_context * ctx, int ne0, int ne1) {
    return ggml_new_tensor_2d(ctx, GGML_TYPE_F32, ne0, ne1);
}

// Create a float32 1D tensor.
struct ggml_tensor * new_f32_1d(struct ggml_context * ctx, int ne0) {
    return ggml_new_tensor_1d(ctx, GGML_TYPE_F32, ne0);
}

// Create an int64 2D tensor (for token IDs).
struct ggml_tensor * new_i64_2d(struct ggml_context * ctx, int ne0, int ne1) {
    return ggml_new_tensor_2d(ctx, GGML_TYPE_I64, ne0, ne1);
}

// Get tensor shape element i.
// Get tensor ndims (GGML pads ne to 4; ggml_n_dims is the true rank).
int tensor_ndims(const struct ggml_tensor * t) { return ggml_n_dims(t); }

// Get tensor dim i.
int64_t tensor_ne(const struct ggml_tensor * t, int i) { return t->ne[i]; }

// Get tensor op code.
int tensor_op(const struct ggml_tensor * t) { return (int)t->op; }

// Get a human-readable op name for tracing.
const char * tensor_op_name(const struct ggml_tensor * t) { return ggml_op_name(t->op); }

// Get tensor's backend buffer (NULL for views that share a source buffer).
struct ggml_backend_buffer * tensor_buffer(const struct ggml_tensor * t) { return t->buffer; }

// Check whether a tensor is contiguous (row-major, no views/permutes).
int tensor_is_contiguous(const struct ggml_tensor * t) { return ggml_is_contiguous(t); }

// Get tensor stride (bytes) element i.
size_t tensor_nb(const struct ggml_tensor * t, int i) { return t->nb[i]; }

// Get tensor data pointer (for CPU tensors only).
void * tensor_data(const struct ggml_tensor * t) { return t->data; }

// Get ggml_type for a dtype int. The cases are tensor.Dtype values, which
// mirror the mlx_dtype enum (see pkg/tensor/types.go) — keep them in sync.
// Getting one wrong is silent and destructive: the tensor is created with a
// different element size than its data, so half the bytes read back are
// uninitialised and Dtype() misreports the type to callers.
enum ggml_type dtype_to_ggml(int dtype) {
    switch (dtype) {
        case 0:  return GGML_TYPE_I8;  // Bool — no ggml bool; I8 at least matches width
        case 1:  return GGML_TYPE_I8;  // UInt8
        case 3:  return GGML_TYPE_I32; // UInt32 → I32
        case 5:  return GGML_TYPE_I8;  // Int8
        case 6:  return GGML_TYPE_I16; // Int16
        case 7:  return GGML_TYPE_I32; // Int32
        case 8:  return GGML_TYPE_I64; // Int64
        case 9:  return GGML_TYPE_F16; // Float16
        case 10: return GGML_TYPE_F32; // Float32
        case 12: return GGML_TYPE_BF16; // BFloat16
        default: return GGML_TYPE_F32;
    }
}

// Get number of bytes in a tensor.
size_t tensor_nbytes(const struct ggml_tensor * t) { return ggml_nbytes(t); }

// Get number of elements in a tensor.
int64_t tensor_nelements(const struct ggml_tensor * t) { return ggml_nelements(t); }

// Walk a GGML tensor's source graph recursively and set data for any
// tensor registered in the Go-side data map. The callback receives each
// tensor pointer; Go decides whether to set data.
typedef void (*set_data_fn)(struct ggml_tensor * t, void * user_data);

void walk_and_set(struct ggml_tensor * t, set_data_fn callback, void * user_data) {
    if (t == NULL) return;
    // Call callback for this tensor
    callback(t, user_data);
    // Walk source tensors
    for (int i = 0; i < GGML_MAX_SRC; i++) {
        if (t->src[i] != NULL) {
            walk_and_set(t->src[i], callback, user_data);
        }
    }
}

// Get GGML_MAX_SRC
int ggml_max_src() { return GGML_MAX_SRC; }

// Get src[i] from a tensor
struct ggml_tensor * get_src(struct ggml_tensor * t, int i) {
    if (i < 0 || i >= GGML_MAX_SRC) return NULL;
    return t->src[i];
}

// Get total system RAM in bytes via sysconf.
size_t ggml_ctx_used(struct ggml_context * ctx) { return ggml_used_mem(ctx); }

size_t ggml_sysconf_page_size() { return (size_t)sysconf(_SC_PAGESIZE); }
size_t ggml_sysconf_phys_pages() { return (size_t)sysconf(_SC_PHYS_PAGES); }

// A single reusable arena for per-op graph metadata, kept separate from the
// main weight context so graph metadata doesn't corrupt weight tensors.
// Letting ggml_init malloc its own arena per op costs an mmap/munmap pair,
// which dominates the cost of a small op once the heap is warm; re-initing a
// context over a buffer we already own costs nothing. ggml_free leaves a
// caller-supplied mem_buffer alone, so the arena survives across ops.
// Callers must serialise: one arena means one live temp context.
static void * temp_arena = NULL;
static size_t temp_arena_size = 0;

// Op graph nodes live in their own arena so they can be reclaimed. They are
// dead the moment evalOp copies its result into a pinned buffer, but a ggml
// context cannot free individual tensors, so the whole arena is re-inited
// once it fills. Reusing the same buffer keeps stale pointers mapped.
static void * ops_arena = NULL;
static size_t ops_arena_size = 0;

struct ggml_context * ggml_new_ops_ctx(size_t mem_size) {
    if (ops_arena_size < mem_size) {
        free(ops_arena);
        ops_arena = aligned_alloc(64, mem_size);
        if (!ops_arena) { ops_arena_size = 0; return NULL; }
        ops_arena_size = mem_size;
    }
    struct ggml_init_params params = { .mem_size = ops_arena_size, .mem_buffer = ops_arena, .no_alloc = true };
    return ggml_init(params);
}

struct ggml_context * ggml_new_temp_ctx(size_t mem_size) {
    if (temp_arena_size < mem_size) {
        free(temp_arena);
        temp_arena = aligned_alloc(64, mem_size);
        if (!temp_arena) { temp_arena_size = 0; return NULL; }
        temp_arena_size = mem_size;
    }
    struct ggml_init_params params = { .mem_size = temp_arena_size, .mem_buffer = temp_arena, .no_alloc = true };
    return ggml_init(params);
}

// Quantize a 2D F32 weight to GGML_TYPE_Q4_0, row by row.
// src is [nrows * n_per_row] floats (row-major); dst must hold
// ggml_row_size(Q4_0, n_per_row) * nrows bytes.
// Returns the number of bytes written, or 0 on error.
size_t ggml_quantize_q4_0(const float * src, void * dst, int nrows, int n_per_row) {
    return ggml_quantize_chunk(GGML_TYPE_Q4_0, src, dst, 0, nrows, n_per_row, NULL);
}

// Get the row size (bytes per row) for a given GGML type.
size_t ggml_row_size_q4_0(int n) {
    return ggml_row_size(GGML_TYPE_Q4_0, n);
}

// Give a tensor its own backend buffer so it keeps its data for life. The
// graph allocator skips tensors that already have data, so a pinned tensor
// is never re-assigned a graph buffer and never needs re-uploading.
ggml_backend_buffer_t ggml_pin_tensor(ggml_backend_t b, struct ggml_tensor * t) {
    ggml_backend_buffer_t buf = ggml_backend_alloc_buffer(b, ggml_nbytes(t) + 64);
    if (!buf) return NULL;
    if (ggml_backend_tensor_alloc(buf, t, ggml_backend_buffer_get_base(buf)) != GGML_STATUS_SUCCESS) {
        ggml_backend_buffer_free(buf);
        return NULL;
    }
    return buf;
}

// ggml validates mul_mat operands with GGML_ASSERT, which aborts the whole
// process on a shape mismatch. Check first so a caller error surfaces as a Go
// error with the shapes in it instead of killing the server.
int ggml_can_mul_mat_check(const struct ggml_tensor * a, const struct ggml_tensor * b) {
    return (a->ne[0] == b->ne[0])
        && (b->ne[2] % a->ne[2] == 0)
        && (b->ne[3] % a->ne[3] == 0);
}

// After a batch is computed, each result holds its data in its own pinned
// buffer, but its op and src pointers still describe how it was produced —
// and those sources live in the ops arena, which is recycled. Severing them
// turns the result into a plain leaf, so a later graph treats it as an input
// instead of walking into freed memory trying to recompute it.
void ggml_make_leaf(struct ggml_tensor * t) {
    if (!t) return;
    t->op = GGML_OP_NONE;
    for (int i = 0; i < GGML_MAX_SRC; i++) t->src[i] = NULL;
    t->view_src = NULL;
    t->view_offs = 0;
}

// ggml_new_graph defaults to 2048 nodes; a batched flush can exceed that.
struct ggml_cgraph * ggml_new_graph_big(struct ggml_context * ctx, size_t size) {
    return ggml_new_graph_custom(ctx, size, false);
}

// Copy src into dst backend-to-backend, but only when the layouts genuinely
// agree — ggml_backend_tensor_copy asserts rather than returning an error, so
// the preconditions are checked here and 0 is returned to signal "use the
// slow path" instead of aborting the process.
int ggml_copy_tensor_checked(struct ggml_tensor * src, struct ggml_tensor * dst) {
    if (!src || !dst || src->type != dst->type) return 0;
    for (int i = 0; i < GGML_MAX_DIMS; i++) {
        if (src->ne[i] != dst->ne[i]) return 0;
    }
    if (!ggml_is_contiguous(src) || !ggml_is_contiguous(dst)) return 0;
    if (ggml_nbytes(src) != ggml_nbytes(dst)) return 0;
    ggml_backend_tensor_copy(src, dst);
    return 1;
}

// Allocate a buffer of an explicit size and bind the tensor into it. Sizing
// by bucket rather than by exact need is what lets buffers be pooled.
ggml_backend_buffer_t ggml_pin_tensor_sized(ggml_backend_t b, struct ggml_tensor * t, size_t size) {
    ggml_backend_buffer_t buf = ggml_backend_alloc_buffer(b, size);
    if (!buf) return NULL;
    if (ggml_backend_tensor_alloc(buf, t, ggml_backend_buffer_get_base(buf)) != GGML_STATUS_SUCCESS) {
        ggml_backend_buffer_free(buf);
        return NULL;
    }
    return buf;
}

// Bind a tensor into an existing buffer, so a released buffer can be reused
// for a later tensor of no greater size instead of going back to malloc.
int ggml_bind_tensor(ggml_backend_buffer_t buf, struct ggml_tensor * t) {
    if (!buf || !t) return 0;
    return ggml_backend_tensor_alloc(buf, t, ggml_backend_buffer_get_base(buf)) == GGML_STATUS_SUCCESS;
}

// Detach a tensor from its buffer without freeing the buffer.
void ggml_unbind_tensor(struct ggml_tensor * t) {
    if (t) { t->buffer = NULL; t->data = NULL; }
}

// Release a pinned buffer and detach the tensor from it.
void ggml_unpin_tensor(ggml_backend_buffer_t buf, struct ggml_tensor * t) {
    if (t) { t->buffer = NULL; t->data = NULL; }
    if (buf) ggml_backend_buffer_free(buf);
}

// Raise the CPU backend's thread count. The setter is an optional per-backend
// entry point, so it is resolved through the registry rather than linked
// directly; returns 0 when the active backend does not expose it.
int ggml_set_threads(ggml_backend_t b, int n) {
    ggml_backend_dev_t dev = ggml_backend_get_device(b);
    if (!dev) return 0;
    ggml_backend_reg_t reg = ggml_backend_dev_backend_reg(dev);
    if (!reg) return 0;
    ggml_backend_set_n_threads_t fn = (ggml_backend_set_n_threads_t)
        ggml_backend_reg_get_proc_address(reg, "ggml_backend_set_n_threads");
    if (!fn) return 0;
    fn(b, n);
    return 1;
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	tensor.RegisterBackend(&GGMLBackend{})
	debugEnabled = os.Getenv("SPROUT_GGML_DEBUG") != ""
	statsEnabled = os.Getenv("SPROUT_GGML_STATS") != ""
	if os.Getenv("SPROUT_GGML_LEAK") != "" {
		leakEnabled = true
		startLeakReporter()
	}
}

// livePins counts tensors currently holding a pinned buffer. A forward pass
// should return to its starting count; a per-request climb means op results
// are never being freed. SPROUT_GGML_LEAK=1 reports it periodically.
var (
	batchOps     int64
	opHist       sync.Map // op name -> *int64, cumulative; leak mode only
	batchFlushes int64
	batchForced  int64
	livePins     int64
	livePinBytes int64
	leakBackend  *GGMLBackend
	leakEnabled  bool
)

func startLeakReporter() {
	go func() {
		for {
			time.Sleep(5 * time.Second)
			type oc struct {
				name string
				n    int64
			}
			var ops []oc
			var total int64
			opHist.Range(func(k, v any) bool {
				n := atomic.LoadInt64(v.(*int64))
				ops = append(ops, oc{k.(string), n})
				total += n
				return true
			})
			sort.Slice(ops, func(i, j int) bool { return ops[i].n > ops[j].n })
			for i, e := range ops {
				if i >= 12 {
					break
				}
				fmt.Printf("ggml: op %-14s %8d  %4.1f%%\n", e.name, e.n, 100*float64(e.n)/float64(total))
			}
			fmt.Printf("ggml: total ops=%d\n", total)
			o, f, fo := atomic.LoadInt64(&batchOps), atomic.LoadInt64(&batchFlushes), atomic.LoadInt64(&batchForced)
			if f > 0 {
				fmt.Printf("ggml: batch ops=%d flushes=%d (%.1f ops/flush, %d hit the limit)\n", o, f, float64(o)/float64(f), fo)
			}
			fmt.Printf("ggml: live pinned tensors=%d bytes=%.1fMB\n",
				atomic.LoadInt64(&livePins), float64(atomic.LoadInt64(&livePinBytes))/1e6)
			if leakBackend != nil {
				fmt.Printf("ggml: leaf ctx used=%.1fMB of 512MB\n",
					float64(C.ggml_ctx_used(leakBackend.leafCtxPtr()))/1e6)
				var arrays int
				leakBackend.arrayMap.Range(func(_, _ any) bool { arrays++; return true })
				fmt.Printf("ggml: arrayMap entries=%d\n", arrays)
				hist := map[string]int{}
				leakBackend.pinned.Range(func(_, v any) bool {
					hist[v.(pinnedBuf).origin]++
					return true
				})
				type kv struct {
					k string
					n int
				}
				var rows []kv
				for k, n := range hist {
					rows = append(rows, kv{k, n})
				}
				sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
				for i, r := range rows {
					if i >= 6 {
						break
					}
					fmt.Printf("ggml:   origin %-16s %d\n", r.k, r.n)
				}
			}
		}
	}()
}

// tempArenaBytes sizes the reusable arena that holds one op's graph
// metadata. Ops are evaluated eagerly, so a graph is a handful of nodes on
// top of ggml_new_graph's fixed 2048-node table — megabytes, not hundreds.
const tempArenaBytes = 16 * 1024 * 1024

// debugEnabled gates the per-op shape tracing. A single forward pass emits
// thousands of lines, so it stays off unless SPROUT_GGML_DEBUG is set.
var debugEnabled bool

func debugf(format string, args ...any) {
	if debugEnabled {
		fmt.Printf(format, args...)
	}
}

// statsEnabled gates per-op result statistics, which localise where a forward
// pass first turns degenerate (all-zero, NaN, or exploding).
var statsEnabled bool

func logResultStats(op string, shape []int, dtype C.enum_ggml_type, raw []byte) {
	if dtype != C.GGML_TYPE_F32 || len(raw) < 4 {
		fmt.Printf("ggml/stat %-16s shape=%v (non-f32)\n", op, shape)
		return
	}
	vals := unsafe.Slice((*float32)(unsafe.Pointer(&raw[0])), len(raw)/4)
	var nan, inf, zero int
	min, max := float32(math.Inf(1)), float32(math.Inf(-1))
	var sumAbs float64
	for _, v := range vals {
		switch {
		case math.IsNaN(float64(v)):
			nan++
			continue
		case math.IsInf(float64(v), 0):
			inf++
			continue
		case v == 0:
			zero++
		}
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sumAbs += math.Abs(float64(v))
	}
	fmt.Printf("ggml/stat %-16s shape=%-18v n=%-8d min=%12.5g max=%12.5g meanAbs=%12.5g nan=%d inf=%d zero=%d\n",
		op, shape, len(vals), min, max, sumAbs/float64(len(vals)), nan, inf, zero)
}

// GGMLBackend implements tensor.Backend via the GGML C library.
type GGMLBackend struct {
	once    sync.Once
	backend C.ggml_backend_t
	ctx     unsafe.Pointer
	opsCtx  unsafe.Pointer
	name    string
	threads int
	initErr error

	// tempMu serialises use of the shared per-op graph arena.
	tempMu sync.Mutex

	// pinOrigin labels the next pin with the op that produced it, for leak
	// diagnosis. Written only under tempMu.
	pinOrigin string

	// pending holds ops whose graphs have been built but not yet computed.
	// Batching them into one graph is what removes the per-op cgo transition
	// and OpenMP fork/join, which together measured ~66% of decode.
	// Guarded by tempMu.
	pending []pendingOp
	// deferUnpin holds buffers whose Arrays were freed while still pending;
	// the graph still writes into them, so they are released after the flush.
	deferUnpin []*C.struct_ggml_tensor

	// bufPool recycles released backend buffers by capacity bucket. Every op
	// result gets its own buffer, so without reuse a decode step performs
	// thousands of malloc/free pairs; glibc's heap fragments under that and
	// allocation alone grew to ~35% of decode time in a CPU profile.
	bufMu   sync.Mutex
	bufPool map[int][]unsafe.Pointer
	bufHeld int

	// pinned maps C tensor pointers to the dedicated backend buffer holding
	// their data. Leaf tensors own their buffer for life, so their contents
	// survive across graph allocations and are uploaded exactly once.
	// Key: uintptr of *C.ggml_tensor.
	pinned sync.Map // map[uintptr]unsafe.Pointer (ggml_backend_buffer_t)

	// arrayMap maps C tensor pointers to the Array that wraps them, so
	// logicalShape can be propagated through op chains.
	arrayMap sync.Map // map[uintptr]*Array
}

// registerArray associates an Array with its C tensor pointer.
func (g *GGMLBackend) registerArray(a *Array) {
	if a != nil && a.tensor != nil {
		g.arrayMap.Store(uintptr(unsafe.Pointer(a.tensor)), a)
	}
}

// pinTensorData gives a leaf tensor its own backend buffer and uploads data
// into it once. The Go slice is not retained: every later read goes through
// ggml_backend_tensor_get and every later op reads the pinned buffer in
// place, so a weight is copied out of Go memory exactly once instead of on
// every op that consumes it.
func (g *GGMLBackend) pinTensorData(t *C.struct_ggml_tensor, data []byte) error {
	if t == nil {
		return fmt.Errorf("ggml: pin on nil tensor")
	}
	nbytes := int(C.tensor_nbytes(t))
	if len(data) > nbytes {
		return fmt.Errorf("ggml: pin of %d bytes exceeds tensor nbytes %d (ne=[%d %d %d %d] type=%d)",
			len(data), nbytes, int64(t.ne[0]), int64(t.ne[1]), int64(t.ne[2]), int64(t.ne[3]), int(t._type))
	}

	capacity := bufCapacityFor(nbytes)
	var buf C.ggml_backend_buffer_t
	if reused, ok := g.bufPoolGet(capacity); ok {
		if C.ggml_bind_tensor(reused, t) != 0 {
			buf = reused
		} else {
			C.ggml_unpin_tensor(reused, nil)
		}
	}
	if buf == nil {
		buf = C.ggml_pin_tensor_sized(g.backend, t, C.size_t(capacity))
		if buf == nil {
			return fmt.Errorf("ggml: failed to allocate %d-byte buffer for tensor", capacity)
		}
	}
	if len(data) > 0 {
		C.ggml_backend_tensor_set(t, unsafe.Pointer(&data[0]), 0, C.size_t(len(data)))
	}
	g.pinned.Store(uintptr(unsafe.Pointer(t)), pinnedBuf{buf: unsafe.Pointer(buf), capacity: capacity, origin: g.pinOrigin})
	atomic.AddInt64(&livePins, 1)
	atomic.AddInt64(&livePinBytes, int64(capacity))
	return nil
}

// cpuFeatures reports which ARM quant fast paths the ggml build detected.
func (g *GGMLBackend) cpuFeatures() (dotprod, i8mm, neon bool) {
	if g.ensureInit() != nil {
		return false, false, false
	}
	feat := func(n string) bool {
		cs := C.CString(n)
		defer C.free(unsafe.Pointer(cs))
		return C.ggml_feat(cs) > 0
	}
	return feat("ggml_cpu_has_dotprod"), feat("ggml_cpu_has_matmul_int8"), feat("ggml_cpu_has_neon")
}

// unpin returns a tensor's pinned buffer to the pool. Used for the
// short-lived inputs that ops build themselves (RoPE positions, attention
// masks) and for every op result when its last Array is freed.
func (g *GGMLBackend) unpin(t *C.struct_ggml_tensor) {
	if t == nil {
		return
	}
	if v, ok := g.pinned.LoadAndDelete(uintptr(unsafe.Pointer(t))); ok {
		pb := v.(pinnedBuf)
		C.ggml_unbind_tensor(t)
		atomic.AddInt64(&livePins, -1)
		atomic.AddInt64(&livePinBytes, -int64(pb.capacity))
		g.bufPoolPut(C.ggml_backend_buffer_t(pb.buf), pb.capacity)
	}
}

func (g *GGMLBackend) ensureInit() error {
	g.once.Do(func() {
		b := C.ggml_init_best()
		if b == nil {
			g.initErr = fmt.Errorf("ggml: no backend available")
			return
		}
		params := C.ggml_make_params(512 * 1024 * 1024)
		ctx := C.ggml_init(params)
		if ctx == nil {
			C.ggml_backend_free(b)
			g.initErr = fmt.Errorf("ggml: failed to init context")
			return
		}
		opsCtx := C.ggml_new_ops_ctx(C.size_t(opsArenaBytes))
		if opsCtx == nil {
			C.ggml_free(ctx)
			C.ggml_backend_free(b)
			g.initErr = fmt.Errorf("ggml: failed to init op context")
			return
		}
		g.backend = b
		g.ctx = unsafe.Pointer(ctx)
		g.opsCtx = unsafe.Pointer(opsCtx)
		g.name = C.GoString(C.backend_name(b))
		leakBackend = g
		g.threads = pickThreadCount()
		if C.ggml_set_threads(b, C.int(g.threads)) == 0 {
			g.threads = 0
		}
	})
	return g.initErr
}

// pickThreadCount chooses the CPU thread count. Measured end to end on a
// 12-core Snapdragon X Elite (Qwen3.5-4B): 4 threads 4.28 tok/s, 6 threads
// 4.33, 8 threads 0.56, 12 threads 0.44. The collapse past 6 is
// oversubscription — ggml's OpenMP workers spin by default, and together with
// Go's own runtime threads they swamp the cores. Streaming memory bandwidth
// also saturates by 4 threads here (107 GB/s), so there is nothing to win.
// Cap at 4; SPROUT_GGML_THREADS overrides for experiments.
const maxAutoThreads = 4

func pickThreadCount() int {
	if v := os.Getenv("SPROUT_GGML_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	if n := runtime.NumCPU(); n < maxAutoThreads {
		if n < 1 {
			return 1
		}
		return n
	}
	return maxAutoThreads
}

func (g *GGMLBackend) Name() string {
	if err := g.ensureInit(); err != nil {
		return "ggml"
	}
	return g.name
}

func (g *GGMLBackend) Available() bool {
	// GGML is available if the ggml build tag is set and a backend loads.
	return g.ensureInit() == nil
}

// ctxPtr returns the context op wrappers build graph nodes in. It is
// recycled (see recycleOpsCtx), so nothing allocated here may outlive the
// evalOp that consumes it.
func (g *GGMLBackend) ctxPtr() *C.struct_ggml_context {
	return (*C.struct_ggml_context)(g.opsCtx)
}

// leafCtxPtr returns the permanent context. Tensors that outlive a single op
// — weights, and the result leaf every evalOp hands back — belong here.
func (g *GGMLBackend) leafCtxPtr() *C.struct_ggml_context {
	return (*C.struct_ggml_context)(g.ctx)
}

// opsArenaBytes sizes the recyclable graph-node arena. One op needs a handful
// of tensors; this absorbs many ops between resets.
const opsArenaBytes = 64 * 1024 * 1024

// recycleOpsCtx re-inits the op arena once it is half full. Called at the end
// of evalOp, the one point where no graph node is still needed: the result is
// copied into its own buffer and the wrapper's nodes are dead.
func (g *GGMLBackend) recycleOpsCtx() {
	ctx := (*C.struct_ggml_context)(g.opsCtx)
	if ctx == nil || uint64(C.ggml_ctx_used(ctx)) < opsArenaBytes/2 {
		return
	}
	C.ggml_free(ctx)
	g.opsCtx = unsafe.Pointer(C.ggml_new_ops_ctx(C.size_t(opsArenaBytes)))
}

// Array wraps a GGML tensor.
type Array struct {
	backend *GGMLBackend
	tensor  *C.struct_ggml_tensor
	hasData bool
	// alloc is the graph allocator that owns the backend buffer for this
	// tensor's graph. Kept alive until Free() so the data persists.
	alloc C.ggml_gallocr_t
	// graph is the compute graph associated with this tensor's Eval.
	graph *C.struct_ggml_cgraph
	// logicalShape overrides Shape() when set. GGML collapses trailing 1s
	// (ggml_n_dims returns 1 for ne=[2560,1]), which loses the batch dim that
	// the MLX convention preserves ([1, seqLen, hidden]). Ops that need to
	// preserve rank set this explicitly.
	logicalShape []int
	// pending is set while this Array's node is queued but not yet computed.
	// Reading it forces a flush.
	pending bool
	// refs counts the Array wrappers sharing this C tensor (see RetainArray).
	// The backend's tensorData/arrayMap registries pin the result buffer of
	// every op, so the last wrapper to be freed must evict them — otherwise a
	// forward pass retains every intermediate it ever produced.
	refs *int32
}

// pinnedBuf is a tensor's backend buffer plus the bucket capacity it was
// allocated at, which is what it must be returned to.
type pinnedBuf struct {
	buf      unsafe.Pointer
	capacity int
	origin   string
}

// bufPoolMaxBytes caps how much memory the recycler holds. Past this, buffers
// are returned to the allocator rather than retained.
const bufPoolMaxBytes = 512 << 20

// bufCapacityFor rounds an allocation up to a power-of-two bucket (with a
// floor, since most op results are small) so that differently-sized results
// can share buffers. The op graph repeats every token, so a handful of
// buckets covers essentially every allocation after the first step.
func bufCapacityFor(nbytes int) int {
	const minCap = 4096
	c := minCap
	for c < nbytes+64 {
		c <<= 1
	}
	return c
}

func (g *GGMLBackend) bufPoolGet(capacity int) (C.ggml_backend_buffer_t, bool) {
	g.bufMu.Lock()
	defer g.bufMu.Unlock()
	free := g.bufPool[capacity]
	if len(free) == 0 {
		return nil, false
	}
	p := free[len(free)-1]
	g.bufPool[capacity] = free[:len(free)-1]
	g.bufHeld -= capacity
	return C.ggml_backend_buffer_t(p), true
}

func (g *GGMLBackend) bufPoolPut(buf C.ggml_backend_buffer_t, capacity int) {
	if buf == nil {
		return
	}
	g.bufMu.Lock()
	if g.bufHeld+capacity > bufPoolMaxBytes {
		g.bufMu.Unlock()
		C.ggml_unpin_tensor(buf, nil)
		return
	}
	if g.bufPool == nil {
		g.bufPool = make(map[int][]unsafe.Pointer)
	}
	g.bufPool[capacity] = append(g.bufPool[capacity], unsafe.Pointer(buf))
	g.bufHeld += capacity
	g.bufMu.Unlock()
}

// newArray wraps a freshly created C tensor with a reference count of one.
func (g *GGMLBackend) newArray(t *C.struct_ggml_tensor) *Array {
	refs := new(int32)
	*refs = 1
	return &Array{backend: g, tensor: t, refs: refs}
}

// Stream is a no-op for GGML (graph compute is synchronous).
type Stream struct{}

func (Stream) Synchronize() error { return nil }
func (Stream) Free()              {}

// Dtype conversion
func toGGMLType(dt tensor.Dtype) C.enum_ggml_type {
	return C.dtype_to_ggml(C.int(dt))
}

func fromGGMLType(gt C.enum_ggml_type) tensor.Dtype {
	switch gt {
	case C.GGML_TYPE_F32:
		return tensor.Float32
	case C.GGML_TYPE_F16:
		return tensor.Float16
	case C.GGML_TYPE_BF16:
		return tensor.BFloat16
	case C.GGML_TYPE_I64:
		return tensor.Int64
	case C.GGML_TYPE_I32:
		return tensor.Int32
	default:
		return tensor.Float32
	}
}

// ── tensor.Array implementation ────────────────────────────────────

func (a *Array) Shape() []int {
	if a.tensor == nil {
		return nil
	}
	// GGML pads ne[] to 4 dims and ggml_n_dims collapses trailing 1s
	// (ne=[2560,1] reports 1D). When an op set a logical shape (preserving
	// the MLX rank convention), use it.
	if a.logicalShape != nil {
		return a.logicalShape
	}
	// Collect GGML dims (ne[0] = contiguous, ne[1] = rows, ...)
	nd := int(C.tensor_ndims(a.tensor))
	var ne []int
	for i := 0; i < nd; i++ {
		n := int(C.tensor_ne(a.tensor, C.int(i)))
		if n <= 0 {
			break
		}
		ne = append(ne, n)
	}
	// Reverse to row-major convention: shape[0] = rows = ne[last], shape[1] = cols = ne[last-1], ...
	shape := make([]int, len(ne))
	for i := range ne {
		shape[i] = ne[len(ne)-1-i]
	}
	return shape
}

func (a *Array) Dtype() tensor.Dtype {
	if a.tensor == nil {
		return tensor.Float32
	}
	return fromGGMLType(a.tensor._type)
}

func (a *Array) Ndim() int {
	s := a.Shape()
	// GGML always has 4 dims; trim trailing 1s
	nd := len(s)
	for nd > 1 && s[nd-1] == 1 {
		nd--
	}
	return nd
}

func (a *Array) Size() int {
	if a.tensor == nil {
		return 0
	}
	return int(C.tensor_nelements(a.tensor))
}

func (a *Array) Eval() error {
	if a.tensor == nil {
		return nil
	}
	if a.pending {
		if err := a.backend.flush(); err != nil {
			return err
		}
		return nil
	}
	if a.hasData {
		return nil
	}
	g := a.backend

	// A pinned leaf already holds its data in its own buffer.
	if C.tensor_buffer(a.tensor) != nil {
		a.hasData = true
		return nil
	}

	// Use a temp context for graph building to avoid polluting the main arena.
	g.tempMu.Lock()
	defer g.tempMu.Unlock()
	tempCtx := C.ggml_new_temp_ctx(tempArenaBytes)
	if tempCtx == nil {
		return fmt.Errorf("ggml: failed to create temp context for Eval")
	}

	graph := C.ggml_new_graph(tempCtx)
	C.ggml_build_forward_expand(graph, a.tensor)

	alloc := C.ggml_gallocr_new(C.ggml_backend_get_default_buffer_type(g.backend))
	if !C.ggml_gallocr_alloc_graph(alloc, graph) {
		C.ggml_gallocr_free(alloc)
		C.ggml_free(tempCtx)
		return fmt.Errorf("ggml: graph allocation failed")
	}

	status := C.ggml_backend_graph_compute(g.backend, graph)
	C.ggml_free(tempCtx)

	if status != C.GGML_STATUS_SUCCESS {
		C.ggml_gallocr_free(alloc)
		return fmt.Errorf("ggml: graph compute failed (status %d)", int(status))
	}

	// Keep allocator alive on the Array.
	a.alloc = alloc
	a.hasData = true
	return nil
}

func (a *Array) Free() {
	if a.alloc != nil {
		C.ggml_gallocr_free(a.alloc)
		a.alloc = nil
	}
	// Release the pinned buffer and registry entry once the last wrapper goes
	// away. Without this a single forward pass retains every intermediate it
	// ever produced.
	if a.refs != nil && a.tensor != nil && a.backend != nil {
		if atomic.AddInt32(a.refs, -1) == 0 {
			b := a.backend
			// Callers free op inputs the moment the op returns, which is safe
			// under eager evaluation but not while a graph is queued: that
			// graph still has to read them. Hold every release until the
			// flush rather than trying to work out which buffers are
			// reachable from the pending nodes.
			b.tempMu.Lock()
			deferred := len(b.pending) > 0
			if deferred {
				b.deferUnpin = append(b.deferUnpin, a.tensor)
			}
			b.tempMu.Unlock()
			if !deferred {
				b.unpin(a.tensor)
			}
			b.arrayMap.Delete(uintptr(unsafe.Pointer(a.tensor)))
		}
		a.refs = nil
	}
	a.tensor = nil
}

func (a *Array) Float32Data() ([]float32, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := a.Size()
	if n == 0 || a.tensor == nil {
		return nil, fmt.Errorf("ggml: Float32Data on empty/null tensor")
	}
	data := make([]float32, n)
	nbytes := C.size_t(n * 4)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&data[0]), 0, nbytes)
	return data, nil
}

func (a *Array) Int64Data() ([]int64, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := a.Size()
	data := make([]int64, n)
	nbytes := C.size_t(n * 8)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&data[0]), 0, nbytes)
	return data, nil
}

func (a *Array) Uint32Data() ([]uint32, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := a.Size()
	data := make([]uint32, n)
	nbytes := C.size_t(n * 4)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&data[0]), 0, nbytes)
	return data, nil
}

// RawBytes returns the raw underlying bytes of the array. Evaluates first.
// The bytes are copied so the caller owns the memory.
func (a *Array) RawBytes() ([]byte, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	if a.tensor == nil {
		return nil, fmt.Errorf("ggml: RawBytes on nil tensor")
	}
	totalBytes := int(C.tensor_nbytes(a.tensor))
	if totalBytes == 0 {
		return []byte{}, nil
	}
	out := make([]byte, totalBytes)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&out[0]), 0, C.size_t(totalBytes))
	return out, nil
}

// ── tensor.Backend: capability ─────────────────────────────────────

func (g *GGMLBackend) NewGPUStream() (tensor.Stream, error)     { return Stream{}, nil }
func (g *GGMLBackend) DefaultGPUStream() (tensor.Stream, error) { return Stream{}, nil }
func (g *GGMLBackend) DefaultStream() (tensor.Stream, error)    { return Stream{}, nil }

// ── tensor.Backend: array creation ─────────────────────────────────

func (g *GGMLBackend) NewArrayFromFloat32(data []float32, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_F32)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data)*4)
	copy(raw, unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*4))
	if err := g.pinTensorData(t, raw); err != nil {
		return nil, err
	}
	arr := g.newArray(t)
	// Preserve the caller's shape. GGML would collapse leading/trailing 1s
	// (e.g. [1, seqLen] → 1D), but the model layer expects the MLX rank
	// convention. The logical shape is used for Shape()/rank reporting while
	// the GGML tensor keeps its ne layout for compute.
	arr.logicalShape = append([]int(nil), shape...)
	g.registerArray(arr)
	return arr, nil
}

// NewArrayQ4_0 quantizes F32 data to GGML Q4_0 format. Enables ARM-optimized
// quantized matmul kernels (NEON/i8mm). Implements the Q4_0Quantizer interface.
func (g *GGMLBackend) NewArrayQ4_0(data []float32, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_Q4_0)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create Q4_0 tensor")
	}
	// DEBUG: log the shape and resulting ne values.
	{
		ne0v := int(C.tensor_ne(t, 0))
		ne1v := int(C.tensor_ne(t, 1))
		debugf("ggml: NewArrayQ4_0 shape=%v ne0=%d ne1=%d\n", shape, ne0v, ne1v)
	}

	// Quantize each row independently. GGML Q4_0 quantizes blocks of 32
	// elements along ne[0] (the contiguous dim). For a 2D tensor
	// [ne0=cols, ne1=rows], each row is ne0 elements.
	ne0 := int(C.tensor_ne(t, 0)) // contiguous dim (= cols)
	totalElements := len(data)

	// Pad data to a multiple of 32 per row if needed.
	if ne0%32 != 0 {
		return nil, fmt.Errorf("ggml: Q4_0 requires ne[0]=%d to be multiple of 32", ne0)
	}

	if statsEnabled {
		var nan, inf int
		for _, v := range data {
			if math.IsNaN(float64(v)) {
				nan++
			} else if math.IsInf(float64(v), 0) {
				inf++
			}
		}
		fmt.Printf("ggml/stat Q4_0-in shape=%v n=%d nan=%d inf=%d ne0=%d\n", shape, len(data), nan, inf, ne0)
	}

	// Quantize the entire flat array — ggml_quantize_chunk handles
	// multi-row by treating the data as contiguous blocks of ne0.
	rowSize := int(C.ggml_row_size_q4_0(C.int(ne0)))
	if rowSize == 0 {
		return nil, fmt.Errorf("ggml: Q4_0 row size is 0 for ne0=%d", ne0)
	}
	totalRows := totalElements / ne0
	totalBytes := rowSize * totalRows

	cBuf := C.malloc(C.size_t(totalBytes))
	if cBuf == nil {
		return nil, fmt.Errorf("ggml: Q4_0 malloc failed for %d bytes", totalBytes)
	}
	written := C.ggml_quantize_q4_0(
		(*C.float)(unsafe.Pointer(&data[0])),
		cBuf,
		C.int(totalRows),
		C.int(ne0),
	)
	if written == 0 {
		C.free(cBuf)
		return nil, fmt.Errorf("ggml: Q4_0 quantization failed")
	}
	raw := C.GoBytes(cBuf, C.int(int(written)))
	C.free(cBuf)
	if err := g.pinTensorData(t, raw); err != nil {
		return nil, err
	}
	arr := g.newArray(t)
	arr.logicalShape = append([]int(nil), shape...)
	g.registerArray(arr)
	return arr, nil
}

func (g *GGMLBackend) NewArrayFromInt64(data []int64, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_I64)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data)*8)
	copy(raw, unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*8))
	if err := g.pinTensorData(t, raw); err != nil {
		return nil, err
	}
	arr := g.newArray(t)
	arr.logicalShape = append([]int(nil), shape...)
	g.registerArray(arr)
	return arr, nil
}

func (g *GGMLBackend) NewArrayFromInt32(data []int32, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_I32)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data)*4)
	copy(raw, unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*4))
	if err := g.pinTensorData(t, raw); err != nil {
		return nil, err
	}
	arr := g.newArray(t)
	arr.logicalShape = append([]int(nil), shape...)
	g.registerArray(arr)
	return arr, nil
}

func (g *GGMLBackend) NewArrayFromBytes(data []byte, shape []int, dtype tensor.Dtype) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	// GGML's CPU ops are F32-centric — ggml_mul and friends reject a
	// half-precision operand outright — so widen BF16/F16 on the way in.
	// Both conversions are lossless, and it keeps the affine dequantiser's
	// view of scales/biases (RawBytes plus Dtype) consistent with storage.
	if dtype == tensor.BFloat16 || dtype == tensor.Float16 {
		data = widenToF32(data, dtype)
		dtype = tensor.Float32
	}
	gt := toGGMLType(dtype)
	t := createTensor(g, shape, gt)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data))
	copy(raw, data)
	if err := g.pinTensorData(t, raw); err != nil {
		return nil, err
	}
	arr := g.newArray(t)
	arr.logicalShape = append([]int(nil), shape...)
	g.registerArray(arr)
	return arr, nil
}

// widenToF32 converts packed BF16 or F16 elements to F32.
func widenToF32(data []byte, dtype tensor.Dtype) []byte {
	n := len(data) / 2
	out := make([]byte, n*4)
	for i := 0; i < n; i++ {
		h := binary.LittleEndian.Uint16(data[i*2:])
		var bits uint32
		if dtype == tensor.BFloat16 {
			bits = uint32(h) << 16
		} else {
			bits = math.Float32bits(float16ToFloat32(h))
		}
		binary.LittleEndian.PutUint32(out[i*4:], bits)
	}
	return out
}

// float16ToFloat32 expands an IEEE 754 half, including subnormals, infinities
// and NaNs.
func float16ToFloat32(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		// Subnormal: renormalise into the f32 exponent range.
		shift := uint32(0)
		for mant&0x400 == 0 {
			mant <<= 1
			shift++
		}
		mant &= 0x3ff
		return math.Float32frombits(sign | (127-15-shift+1)<<23 | mant<<13)
	case 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | mant<<13)
	default:
		return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
	}
}

// materializeTensor allocates a persistent backend buffer for a tensor and
// copies data into it. The allocator is returned and must be kept alive
// (stored on the Array) for as long as the tensor's data is needed.
func (g *GGMLBackend) materializeTensor(t *C.struct_ggml_tensor, data []byte) C.ggml_gallocr_t {
	ctx := g.ctxPtr()
	graph := C.ggml_new_graph(ctx)
	C.ggml_build_forward_expand(graph, t)
	alloc := C.ggml_gallocr_new(C.ggml_backend_get_default_buffer_type(g.backend))
	C.ggml_gallocr_alloc_graph(alloc, graph)
	if len(data) > 0 {
		C.ggml_backend_tensor_set(t, unsafe.Pointer(&data[0]), 0, C.size_t(len(data)))
	}
	C.ggml_backend_graph_compute(g.backend, graph)
	// Do NOT free alloc — caller owns it and must keep it alive.
	return alloc
}

func (g *GGMLBackend) NewScalarInt32(v int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := C.ggml_new_tensor_1d(g.leafCtxPtr(), C.GGML_TYPE_I32, 1)
	data := make([]byte, 4)
	*(*int32)(unsafe.Pointer(&data[0])) = int32(v)
	if err := g.pinTensorData(t, data); err != nil {
		return nil, err
	}
	arr := g.newArray(t)
	g.registerArray(arr)
	return arr, nil
}

func (g *GGMLBackend) Zeros(shape []int, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	data := make([]float32, product(shape))
	return g.NewArrayFromFloat32(data, shape)
}

func (g *GGMLBackend) Arange(start, stop, step float64, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	n := int((stop - start) / step)
	data := make([]float32, n)
	for i := 0; i < n; i++ {
		data[i] = float32(start + float64(i)*step)
	}
	return g.NewArrayFromFloat32(data, []int{n})
}

func (g *GGMLBackend) RetainArray(a tensor.Array) tensor.Array {
	if a == nil {
		return nil
	}
	// Return a fresh wrapper sharing the same C tensor. The model layer
	// stores the retained array in the KV cache and then Free()s the
	// original; a shared Go wrapper would be nil'd by that Free, leaving
	// the cache with a dangling tensor pointer.
	ga := a.(*Array)
	g.registerArray(ga)
	if ga.refs != nil {
		atomic.AddInt32(ga.refs, 1)
	}
	return &Array{
		backend:      ga.backend,
		tensor:       ga.tensor,
		hasData:      ga.hasData,
		refs:         ga.refs,
		logicalShape: append([]int(nil), ga.logicalShape...),
	}
}

func (g *GGMLBackend) AsType(a tensor.Array, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	ga := a.(*Array)
	gt := toGGMLType(dtype)
	// GGML cast: create new tensor with target type and use ggml_cpy (which casts)
	ctx := g.ctxPtr()
	shape := ga.Shape()
	target := createTensor(g, shape, gt)
	result := C.ggml_cpy(ctx, ga.tensor, target)
	return g.evalOp(result)
}

// ── helpers ────────────────────────────────────────────────────────

func product(shape []int) int {
	p := 1
	for _, d := range shape {
		p *= d
	}
	return p
}

func createTensor(g *GGMLBackend, shape []int, gt C.enum_ggml_type) *C.struct_ggml_tensor {
	switch len(shape) {
	case 1:
		return C.ggml_new_tensor_1d(g.leafCtxPtr(), gt, C.int64_t(shape[0]))
	case 2:
		// GGML ne[0] = contiguous dim = cols; ne[1] = rows
		return C.ggml_new_tensor_2d(g.leafCtxPtr(), gt, C.int64_t(shape[1]), C.int64_t(shape[0]))
	case 3:
		return C.ggml_new_tensor_3d(g.leafCtxPtr(), gt, C.int64_t(shape[2]), C.int64_t(shape[1]), C.int64_t(shape[0]))
	case 4:
		return C.ggml_new_tensor_4d(g.leafCtxPtr(), gt, C.int64_t(shape[3]), C.int64_t(shape[2]), C.int64_t(shape[1]), C.int64_t(shape[0]))
	default:
		return nil
	}
}

func shapeToNE(shape []int) []C.int64_t {
	ne := make([]C.int64_t, 4)
	for i := 0; i < 4; i++ {
		if i < len(shape) {
			ne[i] = C.int64_t(shape[i])
		} else {
			ne[i] = 1
		}
	}
	return ne
}

func (a *Array) cTensor() *C.struct_ggml_tensor { return a.tensor }

// scalarF32 creates a 1-element F32 tensor from a Go float32. The caller
// must unpin it once the op that consumes it has been evaluated.
func (g *GGMLBackend) scalarF32(v float32) *C.struct_ggml_tensor {
	ctx := g.ctxPtr()
	t := C.ggml_new_tensor_1d(ctx, C.GGML_TYPE_F32, 1)
	data := []byte{0, 0, 0, 0}
	*(*float32)(unsafe.Pointer(&data[0])) = v
	if err := g.pinTensorData(t, data); err != nil {
		debugf("ggml: scalarF32 pin failed: %v\n", err)
	}
	return t
}

// pendingOp pairs a queued graph node with the Array that will expose it.
type pendingOp struct {
	node *C.struct_ggml_tensor
	arr  *Array
}

// batchEnabled turns on deferred evaluation. Off by default until the batched
// path has as much mileage as the eager one.
var batchEnabled = os.Getenv("SPROUT_GGML_BATCH") != ""

// batchLimit caps how many ops accumulate before a forced flush, bounding
// both graph size and how much pinned memory pending results hold.
const batchLimit = 64

// graphNodeCap sizes the flush graph. Each queued op expands to a handful of
// nodes once its source chain is walked.
const graphNodeCap = 8192

// flush computes every pending op in a single graph. Caller must hold tempMu.
func (g *GGMLBackend) flushLocked() error {
	if len(g.pending) == 0 {
		return nil
	}
	atomic.AddInt64(&batchFlushes, 1)
	tempCtx := C.ggml_new_temp_ctx(tempArenaBytes)
	if tempCtx == nil {
		return fmt.Errorf("ggml: failed to create temp context for flush")
	}
	graph := C.ggml_new_graph_big(tempCtx, C.size_t(graphNodeCap))
	if graph == nil {
		C.ggml_free(tempCtx)
		return fmt.Errorf("ggml: failed to create batch graph")
	}
	for _, p := range g.pending {
		C.ggml_build_forward_expand(graph, p.node)
	}

	alloc := C.ggml_gallocr_new(C.ggml_backend_get_default_buffer_type(g.backend))
	if !C.ggml_gallocr_alloc_graph(alloc, graph) {
		C.ggml_gallocr_free(alloc)
		C.ggml_free(tempCtx)
		return fmt.Errorf("ggml: batch graph allocation failed (%d ops)", len(g.pending))
	}
	status := C.ggml_backend_graph_compute(g.backend, graph)
	C.ggml_gallocr_free(alloc)
	C.ggml_free(tempCtx)
	if status != C.GGML_STATUS_SUCCESS {
		return fmt.Errorf("ggml: batch graph compute failed (status %d)", int(status))
	}

	for _, p := range g.pending {
		// Sever the node from its (about to be recycled) sources; it now owns
		// its data and must behave as a leaf from here on.
		C.ggml_make_leaf(p.node)
		if p.arr != nil {
			p.arr.hasData = true
			p.arr.pending = false
		}
	}
	g.pending = g.pending[:0]

	// Results are materialised; the graph nodes are dead and any Array freed
	// mid-batch can now release its buffer.
	for _, t := range g.deferUnpin {
		g.unpin(t)
	}
	g.deferUnpin = g.deferUnpin[:0]
	g.recycleOpsCtx()
	return nil
}

// flush computes any queued ops.
func (g *GGMLBackend) flush() error {
	g.tempMu.Lock()
	defer g.tempMu.Unlock()
	return g.flushLocked()
}

// enqueueOp defers an op instead of computing it now. The node's output is
// pinned up front so the graph allocator leaves it alone and writes the
// result straight into the buffer the Array will expose.
func (g *GGMLBackend) enqueueOp(opResult *C.struct_ggml_tensor) (*Array, error) {
	g.tempMu.Lock()
	defer g.tempMu.Unlock()

	// Views can claim contiguity while keeping the source's strides, so make
	// the result contiguous — in the ops context, since it must outlive this
	// call and survive until the flush.
	compute := C.ggml_cont(g.leafCtxPtr(), opResult)
	if compute == nil {
		return nil, fmt.Errorf("ggml: cont failed")
	}
	resultShape := g.tensorShape(compute)
	if int(C.tensor_nbytes(compute)) > 0 {
		if leakEnabled {
			g.pinOrigin = C.GoString(C.tensor_op_name(opResult))
			c, _ := opHist.LoadOrStore(g.pinOrigin, new(int64))
			atomic.AddInt64(c.(*int64), 1)
		}
		if err := g.pinTensorData(compute, nil); err != nil {
			return nil, err
		}
	}
	arr := g.newArray(compute)
	arr.logicalShape = resultShape
	// Same rank-preserving propagation the eager path does: GGML collapses
	// trailing 1s, which loses the MLX rank convention. Must happen here,
	// while opResult's source chain is still intact.
	switch int(C.tensor_op(opResult)) {
	case int(C.GGML_OP_ADD), int(C.GGML_OP_SUB), int(C.GGML_OP_MUL), int(C.GGML_OP_DIV),
		int(C.GGML_OP_UNARY), int(C.GGML_OP_SQRT), int(C.GGML_OP_SQR),
		int(C.GGML_OP_RMS_NORM), int(C.GGML_OP_SOFT_MAX),
		int(C.GGML_OP_ROPE), int(C.GGML_OP_SCALE), int(C.GGML_OP_CPY):
		if src := C.get_src(opResult, 0); src != nil {
			if in := g.findArrayWithShape(src); in != nil {
				arr.logicalShape = append([]int(nil), in.logicalShape...)
			}
		}
	}
	arr.pending = true
	g.registerArray(arr)
	g.pending = append(g.pending, pendingOp{node: compute, arr: arr})
	atomic.AddInt64(&batchOps, 1)

	if len(g.pending) >= batchLimit {
		atomic.AddInt64(&batchForced, 1)
		if err := g.flushLocked(); err != nil {
			return nil, err
		}
	}
	return arr, nil
}

// evalOpUnpin evaluates an op, then releases the transient inputs the op
// wrapper built for itself (scalars, causal masks, position ids). Those are
// never handed back to the caller, so nothing else will free their buffers.
func (g *GGMLBackend) evalOpUnpin(node *C.struct_ggml_tensor, transient ...*C.struct_ggml_tensor) (*Array, error) {
	arr, err := g.evalOp(node)
	for _, t := range transient {
		if t == nil {
			continue
		}
		if batchEnabled {
			// The op has only been queued: the graph still has to read these
			// inputs, so their buffers cannot be recycled until it runs.
			g.tempMu.Lock()
			g.deferUnpin = append(g.deferUnpin, t)
			g.tempMu.Unlock()
			continue
		}
		g.unpin(t)
	}
	return arr, err
}

// evalOp builds a single-op graph, allocates, sets input data, computes,
// then COPIES the result into a fresh leaf tensor owned by the returned
// Array. The graph allocator is freed immediately.
//
// This is the key design decision for eager evaluation: each returned Array
// owns its own data buffer, so freeing one result can never invalidate
// another Array's data (the graph allocator would otherwise assign buffers
// to ALL graph tensors, including input weights, and freeing the result's
// allocator would leave dangling pointers).
//
// Uses a TEMPORARY GGML context for graph building, separate from the main
// weight context, so graph metadata never overwrites weight tensor structs.
func (g *GGMLBackend) evalOp(opResult *C.struct_ggml_tensor) (*Array, error) {
	if batchEnabled {
		return g.enqueueOp(opResult)
	}
	// Build the graph in a TEMPORARY context. Graphs are ~64KB each and
	// accumulate in the main context arena, eventually corrupting tensor
	// metadata. By using a temp context, we keep the main arena clean for
	// tensor structs only.
	g.tempMu.Lock()
	defer g.tempMu.Unlock()
	tempCtx := C.ggml_new_temp_ctx(tempArenaBytes)
	if tempCtx == nil {
		return nil, fmt.Errorf("ggml: failed to create temp context")
	}

	graph := C.ggml_new_graph(tempCtx)
	// Views/permutes produce strided results; make contiguous so the data
	// copy below yields a plain row-major buffer. Always cont — views can
	// claim is_contiguous while retaining the source's nb (e.g. a last-dim
	// slice), which makes tensor_nbytes disagree with the logical shape.
	compute := C.ggml_cont(tempCtx, opResult)
	C.ggml_build_forward_expand(graph, compute)

	alloc := C.ggml_gallocr_new(C.ggml_backend_get_default_buffer_type(g.backend))
	if !C.ggml_gallocr_alloc_graph(alloc, graph) {
		C.ggml_gallocr_free(alloc)
		C.ggml_free(tempCtx)
		return nil, fmt.Errorf("ggml: graph allocation failed")
	}

	status := C.ggml_backend_graph_compute(g.backend, graph)
	if status != C.GGML_STATUS_SUCCESS {
		C.ggml_gallocr_free(alloc)
		C.ggml_free(tempCtx)
		return nil, fmt.Errorf("ggml: graph compute failed (status %d)", int(status))
	}

	// Capture the shape and type BEFORE freeing the temp context (the
	// contiguous tensor may live there).
	resultNbytes := int(C.tensor_nbytes(compute))
	resultShape := g.tensorShape(compute)
	resultType := compute._type

	// Create a FRESH leaf tensor in the main context and move the result into
	// it. This Array owns its buffer; freeing it can't hurt other arrays.
	// The move goes buffer-to-buffer: staging it through a Go []byte per op
	// made evalOp the largest single source of garbage in the process, and GC
	// was costing ~30% of decode in a CPU profile.
	leaf := createTensor(g, resultShape, resultType)
	if leaf == nil {
		C.ggml_gallocr_free(alloc)
		C.ggml_free(tempCtx)
		return nil, fmt.Errorf("ggml: evalOp result tensor creation failed")
	}
	if leakEnabled {
		// C.GoString allocates; only pay it when diagnosing a leak.
		g.pinOrigin = C.GoString(C.tensor_op_name(opResult))
	}
	var resultData []byte
	if resultNbytes > 0 {
		if err := g.pinTensorData(leaf, nil); err != nil {
			C.ggml_gallocr_free(alloc)
			C.ggml_free(tempCtx)
			return nil, err
		}
		if C.ggml_copy_tensor_checked(compute, leaf) == 0 {
			// Layouts disagree; fall back to staging through Go memory.
			resultData = make([]byte, resultNbytes)
			C.ggml_backend_tensor_get(compute, unsafe.Pointer(&resultData[0]), 0, C.size_t(resultNbytes))
			C.ggml_backend_tensor_set(leaf, unsafe.Pointer(&resultData[0]), 0, C.size_t(resultNbytes))
		}
	}
	C.ggml_gallocr_free(alloc)
	C.ggml_free(tempCtx)

	g.recycleOpsCtx()

	arr := g.newArray(leaf)
	arr.logicalShape = resultShape
	if statsEnabled {
		if resultData == nil && resultNbytes > 0 {
			resultData = make([]byte, resultNbytes)
			C.ggml_backend_tensor_get(leaf, unsafe.Pointer(&resultData[0]), 0, C.size_t(resultNbytes))
		}
		logResultStats(C.GoString(C.tensor_op_name(opResult)), resultShape, resultType, resultData)
	}
	// Propagate logicalShape from the primary input for RANK-PRESERVING ops
	// only. GGML collapses trailing 1s, losing the MLX rank convention
	// ([1, seqLen, hidden]). Shape-changing ops (reshape/view/permute/
	// transpose/concat) compute their own logical shape and must NOT inherit
	// the input's.
	op := int(C.tensor_op(opResult))
	switch op {
	case int(C.GGML_OP_ADD), int(C.GGML_OP_SUB), int(C.GGML_OP_MUL), int(C.GGML_OP_DIV),
		int(C.GGML_OP_UNARY), int(C.GGML_OP_SQRT), int(C.GGML_OP_SQR),
		int(C.GGML_OP_RMS_NORM), int(C.GGML_OP_SOFT_MAX),
		int(C.GGML_OP_ROPE), int(C.GGML_OP_SCALE), int(C.GGML_OP_CPY):
		if src := C.get_src(opResult, 0); src != nil {
			if in := g.findArrayWithShape(src); in != nil {
				arr.logicalShape = append([]int(nil), in.logicalShape...)
			}
		}
	}
	g.registerArray(arr)
	return arr, nil
}

// findArrayWithShape walks the src chain from t to find a registered Array
// that carries a logical shape.
func (g *GGMLBackend) findArrayWithShape(t *C.struct_ggml_tensor) *Array {
	if t == nil {
		return nil
	}
	if a := g.arrayByTensor(t); a != nil && a.logicalShape != nil {
		return a
	}
	maxSrc := int(C.ggml_max_src())
	for i := 0; i < maxSrc; i++ {
		src := C.get_src(t, C.int(i))
		if src != nil {
			if a := g.findArrayWithShape(src); a != nil {
				return a
			}
		}
	}
	return nil
}

// arrayByTensor finds the Array wrapping a C tensor, or nil. Uses the
// backend's tensor→Array registry so the logicalShape survives op chains.
func (g *GGMLBackend) arrayByTensor(t *C.struct_ggml_tensor) *Array {
	v, ok := g.arrayMap.Load(uintptr(unsafe.Pointer(t)))
	if !ok {
		return nil
	}
	return v.(*Array)
}
func (g *GGMLBackend) tensorShape(t *C.struct_ggml_tensor) []int {
	var ne []int
	for i := 0; i < 4; i++ {
		n := int(C.tensor_ne(t, C.int(i)))
		if n <= 0 {
			break
		}
		ne = append(ne, n)
	}
	shape := make([]int, len(ne))
	for i := range ne {
		shape[i] = ne[len(ne)-1-i]
	}
	return shape
}

func (g *GGMLBackend) Add(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_add(g.ctxPtr(), a.(*Array).tensor, b.(*Array).tensor))
}

func (g *GGMLBackend) Subtract(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ta := a.(*Array).tensor
	tb := b.(*Array).tensor
	debugf("ggml: Sub aShape=%v aNe=[%d %d %d %d] bShape=%v bNe=[%d %d %d %d]\n",
		a.Shape(), int64(ta.ne[0]), int64(ta.ne[1]), int64(ta.ne[2]), int64(ta.ne[3]),
		b.Shape(), int64(tb.ne[0]), int64(tb.ne[1]), int64(tb.ne[2]), int64(tb.ne[3]))
	return g.evalOp(C.ggml_sub(g.ctxPtr(), ta, tb))
}

func (g *GGMLBackend) Multiply(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ta := a.(*Array).tensor
	tb := b.(*Array).tensor
	// Elementwise mul is commutative. GGML requires the SECOND operand to be
	// broadcastable to the FIRST (can_repeat(b, a)); when the caller passes
	// the larger tensor second (e.g. [32] * [1,S,32]), swap so the smaller
	// one is the broadcast operand.
	if !canRepeat(tb, ta) && canRepeat(ta, tb) {
		ta, tb = tb, ta
	}
	// DEBUG: log multiply shapes + GGML ne values.
	debugf("ggml: Mul aShape=%v aNe=[%d %d %d %d] bShape=%v bNe=[%d %d %d %d]\n",
		a.Shape(), int64(ta.ne[0]), int64(ta.ne[1]), int64(ta.ne[2]), int64(ta.ne[3]),
		b.Shape(), int64(tb.ne[0]), int64(tb.ne[1]), int64(tb.ne[2]), int64(tb.ne[3]))
	return g.evalOp(C.ggml_mul(g.ctxPtr(), ta, tb))
}

// canRepeat reports whether b's dims evenly divide a's dims (i.e. b can be
// broadcast up to a's shape in ggml_mul).
func canRepeat(b, a *C.struct_ggml_tensor) bool {
	for i := 0; i < 4; i++ {
		ai := int64(a.ne[i])
		bi := int64(b.ne[i])
		if bi == 0 {
			if ai != 0 {
				return false
			}
			continue
		}
		if ai%bi != 0 {
			return false
		}
	}
	return true
}

// RepeatTo expands a tensor to a target shape by repeating along axes where
// it has extent 1. Used for the DeltaNet outer product, where neither
// operand is already the full [B,Hv,Dv,Dk] shape and GGML's mul can't
// broadcast both ways in one call.
func (g *GGMLBackend) RepeatTo(a tensor.Array, target []int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// Map row-major target to GGML ne.
	nd := len(target)
	rev := make([]C.int64_t, 4)
	for i := 0; i < 4; i++ {
		if i < nd {
			rev[i] = C.int64_t(target[nd-1-i])
		} else {
			rev[i] = 1
		}
	}
	targetTensor := C.ggml_new_tensor(ctx, t._type, C.int(nd), (*C.int64_t)(unsafe.Pointer(&rev[0])))
	result := C.ggml_repeat(ctx, t, targetTensor)
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	out.logicalShape = append([]int(nil), target...)
	g.registerArray(out)
	return out, nil
}

func (g *GGMLBackend) Divide(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_div(g.ctxPtr(), a.(*Array).tensor, b.(*Array).tensor))
}

func (g *GGMLBackend) Maximum(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	// GGML has no max op; compose: max(a,b) = a + relu(b-a)
	ctx := g.ctxPtr()
	ta := a.(*Array).tensor
	tb := b.(*Array).tensor
	diff := C.ggml_sub(ctx, tb, ta)
	relu := C.ggml_relu(ctx, diff)
	return g.evalOp(C.ggml_add(ctx, ta, relu))
}

// ── tensor.Backend: elementwise unary ──────────────────────────────

func (g *GGMLBackend) Abs(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_abs(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Exp(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_exp(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Log(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_log(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Log1p(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// log(1 + x) = log(1+x); GGML has no log1p, compose: add scalar then log
	one := g.scalarF32(1.0)
	return g.evalOpUnpin(C.ggml_log(ctx, C.ggml_add(ctx, t, one)), one)
}

func (g *GGMLBackend) Sqrt(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_sqrt(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Square(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_sqr(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Negative(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_neg(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Sigmoid(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_sigmoid(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Softplus(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// softplus(x) = log(1 + exp(x))
	one := g.scalarF32(1.0)
	return g.evalOpUnpin(C.ggml_log(ctx, C.ggml_add(ctx, one, C.ggml_exp(ctx, t))), one)
}

func (g *GGMLBackend) Sin(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_sin(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Cos(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_cos(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Tanh(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_tanh(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) Power(a tensor.Array, exp float32, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// x^exp = exp(exp * log(x)) for x > 0
	logT := C.ggml_log(ctx, t)
	scaled := C.ggml_scale(ctx, logT, C.float(exp))
	return g.evalOp(C.ggml_exp(ctx, scaled))
}

// ── tensor.Backend: reductions ─────────────────────────────────────

func (g *GGMLBackend) Sum(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	// GGML's ggml_sum reduces ALL dims to a scalar. For a partial reduction
	// (DeltaNet sums over Dk = last dim), compute in Go to keep the logical
	// shape correct. The sums here are small (per-head), so Go is fine.
	if len(axes) == 0 {
		return g.evalOp(C.ggml_sum(g.ctxPtr(), a.(*Array).tensor))
	}
	shape := a.Shape()

	// Reducing only the innermost axis is the hot path — DeltaNet sums over
	// Dk on every step and the MoE router reduces over -1 — and it maps
	// exactly onto ggml_sum_rows, which reduces ne[0]. Taking the Go path
	// here instead costs a full readback per call and showed up as ~15% of
	// decode in a CPU profile.
	if len(axes) == 1 && len(shape) <= 4 && a.Dtype() == tensor.Float32 {
		ax := axes[0]
		if ax < 0 {
			ax += len(shape)
		}
		if ax == len(shape)-1 {
			ls := make([]int, 0, len(shape))
			for i, d := range shape {
				switch {
				case i != ax:
					ls = append(ls, d)
				case keepdims:
					ls = append(ls, 1)
				}
			}
			if len(ls) == 0 {
				ls = []int{1}
			}
			node := C.ggml_sum_rows(g.ctxPtr(), a.(*Array).tensor)
			if !keepdims {
				// sum_rows reduces ne[0] but leaves it in place as a 1, while
				// dropping the axis in row-major terms shifts every remaining
				// dim down one slot. Without this reshape the ne of the result
				// is offset by one against its logical shape, and the next op
				// fails ggml_can_repeat.
				ne := [4]C.int64_t{1, 1, 1, 1}
				for i := range ls {
					ne[i] = C.int64_t(ls[len(ls)-1-i])
				}
				node = C.ggml_reshape_4d(g.ctxPtr(), node, ne[0], ne[1], ne[2], ne[3])
			}
			out, err := g.evalOp(node)
			if err != nil {
				return nil, err
			}
			out.logicalShape = ls
			g.registerArray(out)
			return out, nil
		}
	}

	if err := a.Eval(); err != nil {
		return nil, err
	}
	data, err := g.readDataAsFloat32(a.(*Array))
	if err != nil {
		return nil, fmt.Errorf("ggml: Sum read: %w", err)
	}

	// Normalise negative axes (the MoE router reduces over -1).
	reduce := make([]bool, len(shape))
	for _, ax := range axes {
		if ax < 0 {
			ax += len(shape)
		}
		if ax < 0 || ax >= len(shape) {
			return nil, fmt.Errorf("ggml: Sum axis %d out of range for shape %v", ax, shape)
		}
		reduce[ax] = true
	}

	outShape := make([]int, 0, len(shape))
	for i, d := range shape {
		switch {
		case !reduce[i]:
			outShape = append(outShape, d)
		case keepdims:
			outShape = append(outShape, 1)
		}
	}
	outStride := make([]int, len(outShape))
	acc := 1
	for i := len(outShape) - 1; i >= 0; i-- {
		outStride[i] = acc
		acc *= outShape[i]
	}

	// Scatter every input element onto its output slot. Walking the input
	// once keeps reduced axes of any extent correct — the previous version
	// looped over the RANK rather than the reduced extent, so it added one
	// element to itself len(shape) times.
	out := make([]float32, acc)
	idx := make([]int, len(shape))
	for _, v := range data {
		o, oi := 0, 0
		for d := range shape {
			if reduce[d] {
				if keepdims {
					oi++ // index is always 0 along a kept reduced axis
				}
				continue
			}
			o += idx[d] * outStride[oi]
			oi++
		}
		out[o] += v
		for d := len(shape) - 1; d >= 0; d-- {
			idx[d]++
			if idx[d] < shape[d] {
				break
			}
			idx[d] = 0
		}
	}

	// A full reduction without keepdims has no axes left; carry it as [1].
	arrShape := outShape
	if len(arrShape) == 0 {
		arrShape = []int{1}
	}
	arr, err := g.NewArrayFromFloat32(out, arrShape)
	if err != nil {
		return nil, err
	}
	arr.(*Array).logicalShape = arrShape
	g.registerArray(arr.(*Array))
	return arr, nil
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func dot(idx, stride []int) int {
	var s int
	for i := range idx {
		s += idx[i] * stride[i]
	}
	return s
}

func (g *GGMLBackend) Mean(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	sumT := C.ggml_sum(ctx, t)
	n := g.scalarF32(float32(a.Size()))
	return g.evalOpUnpin(C.ggml_div(ctx, sumT, n), n)
}

func (g *GGMLBackend) Max(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_argmax(g.ctxPtr(), a.(*Array).tensor))
}

// ── tensor.Backend: linear algebra ─────────────────────────────────

func (g *GGMLBackend) MatMul(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	tA := a.(*Array).tensor
	tB := b.(*Array).tensor

	var result *C.struct_ggml_tensor
	if tB._type != C.GGML_TYPE_F32 && tB._type != C.GGML_TYPE_F16 && tB._type != C.GGML_TYPE_BF16 {
		// Quantized weight (Q4_0 etc.) — already in [ne0=K, ne1=N] layout.
		// ggml_mul_mat(w, x) = x @ w^T = [M, N]. No transpose needed.
		if C.ggml_can_mul_mat_check(tB, tA) == 0 {
			return nil, fmt.Errorf("ggml: MatMul shapes incompatible: a=%v b=%v (quantized b must be [out, in])", a.Shape(), b.Shape())
		}
		result = C.ggml_mul_mat(ctx, tB, tA)
	} else {
		// F32 weight — transpose B to align inner dims, then mul_mat.
		tBt := C.ggml_cont(ctx, C.ggml_transpose(ctx, tB))
		if C.ggml_can_mul_mat_check(tBt, tA) == 0 {
			return nil, fmt.Errorf("ggml: MatMul shapes incompatible: a=%v b=%v (float b must be pre-transposed to [in, out])", a.Shape(), b.Shape())
		}
		result = C.ggml_mul_mat(ctx, tBt, tA)
	}
	if result == nil {
		return nil, fmt.Errorf("ggml: mul_mat returned NULL")
	}
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	// Preserve the MLX rank convention: matmul output = a.shape[:-1] + [out].
	// GGML collapses the leading batch dims of a when they're 1.
	aShape := a.Shape()
	var outDim int
	if tB._type != C.GGML_TYPE_F32 && tB._type != C.GGML_TYPE_F16 && tB._type != C.GGML_TYPE_BF16 {
		outDim = b.Shape()[0] // quantized w in [out, in]
	} else {
		outDim = b.Shape()[len(b.Shape())-1] // wT in [in, out]
	}
	logical := make([]int, 0, len(aShape))
	logical = append(logical, aShape[:len(aShape)-1]...)
	logical = append(logical, outDim)
	out.logicalShape = logical
	g.registerArray(out)
	// DEBUG: log matmul shapes.
	debugf("ggml: MatMul aShape=%v bShape=%v -> outShape=%v\n", a.Shape(), b.Shape(), out.Shape())
	return out, nil
}

// ── tensor.Backend: shape manipulation ─────────────────────────────

func (g *GGMLBackend) Reshape(a tensor.Array, shape []int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// GGML ne is reversed from row-major: for shape [d0,d1,...,dn-1],
	// ne = [dn-1, ..., d1, d0] with n_dims = len(shape). Only the meaningful
	// dims are reversed; trailing padding (ne[i]=1) comes after.
	nd := len(shape)
	rev := make([]C.int64_t, 4)
	for i := 0; i < 4; i++ {
		if i < nd {
			rev[i] = C.int64_t(shape[nd-1-i])
		} else {
			rev[i] = 1
		}
	}
	// DEBUG: log reshape element counts.
	debugf("ggml: Reshape srcShape=%v -> target=%v srcNelements=%d targetNelements=%d\n",
		a.Shape(), shape, int(C.tensor_nelements(t)), product(shape))
	result := C.ggml_reshape(ctx, t, C.ggml_new_tensor(ctx, t._type, C.int(nd), (*C.int64_t)(unsafe.Pointer(&rev[0]))))
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	// Reshape is shape-changing — set the logical shape to the requested
	// target (GGML would collapse leading 1s in the ne layout).
	out.logicalShape = append([]int(nil), shape...)
	g.registerArray(out)
	return out, nil
}

func (g *GGMLBackend) Transpose(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_cont(g.ctxPtr(), C.ggml_transpose(g.ctxPtr(), a.(*Array).tensor)))
}

func (g *GGMLBackend) TransposeAxes(a tensor.Array, axes []int, s tensor.Stream) (tensor.Array, error) {
	// GGML only supports 2D transpose directly; for higher dims, compose.
	// For the common case of [0,2,1,3] (attention transpose), use permute.
	t := a.(*Array).tensor
	// Map row-major axes to GGML ne indices (reversed order). ggml_permute's
	// argument i names the DESTINATION of source dim i, whereas axes[i] names
	// the SOURCE of destination i — so the assignment is indexed by the source.
	// Defaults are the identity so ranks below 4 leave the unused trailing ne
	// slots alone instead of aliasing index 0.
	nd := len(a.Shape())
	ne := []C.int{0, 1, 2, 3}
	for i, ax := range axes {
		if ax >= 0 && ax < 4 {
			ne[ggmlAxisIndex(nd, ax)] = C.int(ggmlAxisIndex(nd, i))
		}
	}
	out, err := g.evalOp(C.ggml_cont(g.ctxPtr(), C.ggml_permute(g.ctxPtr(), t, ne[0], ne[1], ne[2], ne[3])))
	if err != nil {
		return nil, err
	}
	// Set logical shape: permuted axes.
	if len(axes) == len(a.Shape()) {
		logical := make([]int, len(axes))
		shape := a.Shape()
		for i, ax := range axes {
			if ax >= 0 && ax < len(shape) {
				logical[i] = shape[ax]
			}
		}
		out.logicalShape = logical
		g.registerArray(out)
	}
	return out, nil
}

func (g *GGMLBackend) SqueezeAxis(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	// GGML doesn't have squeeze; return the tensor as-is (reshape if needed).
	// For our use case (squeezing dim 2 of a [1,1,H] to [1,1]), it's a no-op
	// since GGML uses 4D internally with trailing 1s.
	return a, nil
}

func (g *GGMLBackend) Slice(a tensor.Array, start, stop, strides []int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// General N-D slice via GGML views. Row-major start/stop map to GGML
	// dims in reverse (row-major [B,S,H,D] ↔ GGML ne=[D,H,S,B]).
	nd := len(start)
	if nd < 1 || nd != len(stop) {
		return nil, fmt.Errorf("ggml: slice start/stop length mismatch")
	}
	// Compute per-dim extents and byte offsets.
	var ne [4]C.int64_t
	var off C.size_t
	rowShape := a.Shape()
	for i := 0; i < nd; i++ {
		ggmlDim := nd - 1 - i // reverse: row-major axis i → GGML ne[ggmlDim]
		if ggmlDim < 4 {
			ne[ggmlDim] = C.int64_t(stop[i] - start[i])
			off += C.size_t(start[i]) * C.size_t(t.nb[ggmlDim])
		}
	}
	// Fill trailing GGML dims from the source shape.
	for d := 0; d < 4; d++ {
		if ne[d] == 0 {
			if d < len(rowShape) {
				ne[d] = C.int64_t(rowShape[len(rowShape)-1-d])
			} else {
				ne[d] = 1
			}
		}
	}
	// Validate the view fits within the source tensor before calling
	// ggml_view_* (which asserts and aborts the process otherwise).
	srcNbytes := int64(C.tensor_nbytes(t))
	wantBytes := int64(1)
	for d := 0; d < 4; d++ {
		wantBytes *= int64(ne[d])
	}
	if int64(off)+wantBytes*4 > srcNbytes {
		return nil, fmt.Errorf("ggml: slice %v→%v of shape %v exceeds source (%d + %d > %d bytes): logical=%v",
			start, stop, rowShape, int64(off), wantBytes*4, srcNbytes, a.(*Array).logicalShape)
	}
	// DEBUG: print slice details.
	debugf("ggml: Slice shape=%v start=%v stop=%v nd=%d ne=[%d %d %d %d] off=%d nbytes=%d nb=[%d %d %d %d]\n",
		rowShape, start, stop, nd, int64(ne[0]), int64(ne[1]), int64(ne[2]), int64(ne[3]),
		int(off), int(C.tensor_nbytes(t)), int64(t.nb[0]), int64(t.nb[1]), int64(t.nb[2]), int64(t.nb[3]))
	var result *C.struct_ggml_tensor
	switch nd {
	case 1:
		result = C.ggml_view_1d(ctx, t, ne[0], off)
	case 2:
		result = C.ggml_view_2d(ctx, t, ne[0], ne[1], C.size_t(t.nb[1]), off)
	case 3:
		result = C.ggml_view_3d(ctx, t, ne[0], ne[1], ne[2], C.size_t(t.nb[1]), C.size_t(t.nb[2]), off)
	case 4:
		result = C.ggml_view_4d(ctx, t, ne[0], ne[1], ne[2], ne[3], C.size_t(t.nb[1]), C.size_t(t.nb[2]), C.size_t(t.nb[3]), off)
	default:
		return nil, fmt.Errorf("ggml: slice supports 1-4 dims, got %d", nd)
	}
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	// Preserve the logical shape through the slice: apply start/stop to the
	// input's row-major shape. GGML collapses trailing 1s, which loses the
	// [1, seqLen] rank of token-id tensors.
	logical := make([]int, len(rowShape))
	for i := range rowShape {
		if i < nd {
			logical[i] = stop[i] - start[i]
		} else {
			logical[i] = rowShape[i]
		}
	}
	out.logicalShape = logical
	g.registerArray(out)
	return out, nil
}

func (g *GGMLBackend) SliceUpdate(src, update tensor.Array, start, stop []int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	ga := src.(*Array)
	t := ga.tensor
	u := update.(*Array).tensor

	// Byte offset of the row-major `start` index. Each row-major axis maps to
	// a GGML ne/nb index in reverse order — using start[0]*nb[0] would pair the
	// batch index with the innermost stride and always yield 0, so every
	// windowed KV append would overwrite position 0.
	nd := len(ga.Shape())
	var offset C.size_t
	for i := 0; i < len(start) && i < nd; i++ {
		gi := ggmlAxisIndex(nd, i)
		if gi < 4 {
			offset += C.size_t(start[i]) * C.size_t(t.nb[gi])
		}
	}

	// ggml_set writes b into a view of a (ggml_acc would ADD, which only
	// happens to work while the destination region is still zeroed).
	result := C.ggml_set(ctx, t, u,
		C.size_t(t.nb[1]), C.size_t(t.nb[2]), C.size_t(t.nb[3]), offset)
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	out.logicalShape = append([]int(nil), ga.Shape()...)
	g.registerArray(out)
	return out, nil
}

func (g *GGMLBackend) ConcatenateAxis(arrays []tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	if len(arrays) == 0 {
		return nil, fmt.Errorf("ggml: concatenate requires at least one array")
	}
	// GGML ne is reversed from row-major: [B,S,H,D] ↔ ne=[D,H,S,B], so the
	// row-major axis must be mapped to the GGML ne index.
	nd := len(arrays[0].Shape())
	ggmlAxis := ggmlAxisIndex(nd, axis)
	result := arrays[0].(*Array).tensor
	for _, a := range arrays[1:] {
		result = C.ggml_concat(ctx, result, a.(*Array).tensor, C.int(ggmlAxis))
	}
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	// Concatenation changes the shape along `axis` — the evalOp shape
	// propagation would incorrectly inherit src[0]'s logical shape. Compute
	// the correct one: sum the axis extent across inputs.
	shape := arrays[0].Shape()
	if axis >= 0 && axis < len(shape) {
		logical := append([]int(nil), shape...)
		logical[axis] = 0
		for _, a := range arrays {
			sh := a.Shape()
			if axis < len(sh) {
				logical[axis] += sh[axis]
			}
		}
		out.logicalShape = logical
		g.registerArray(out)
	}
	return out, nil
}

// ggmlAxisIndex maps a row-major axis (shape index, 0 = outermost) to the
// GGML ne index (0 = contiguous/innermost). For an N-D tensor,
// ggmlAxis = N-1-axis.
func ggmlAxisIndex(nd, axis int) int {
	if nd <= 1 || axis < 0 {
		return 0
	}
	return nd - 1 - axis
}

func (g *GGMLBackend) Stack(arrays []tensor.Array, s tensor.Stream) (tensor.Array, error) {
	// Stack = concatenate along a new axis 0. GGML doesn't have stack directly;
	// reshape each to [1, ...original] and concat on axis 0.
	ctx := g.ctxPtr()
	result := arrays[0].(*Array).tensor
	for _, a := range arrays[1:] {
		result = C.ggml_concat(ctx, result, a.(*Array).tensor, 1)
	}
	return g.evalOp(result)
}

func (g *GGMLBackend) SplitAxis(a tensor.Array, indices []int, axis int, s tensor.Stream) ([]tensor.Array, error) {
	// Compute the split in Go. GGML's view_2d only supports 2D splits and
	// its offset math is easy to get wrong for non-contiguous axes. The
	// split sizes here are tiny (DeltaNet conv window), so Go is fine.
	if err := a.Eval(); err != nil {
		return nil, fmt.Errorf("ggml: SplitAxis eval: %w", err)
	}
	shape := a.Shape() // row-major
	if axis < 0 || axis >= len(shape) {
		return nil, fmt.Errorf("ggml: SplitAxis axis %d out of range for shape %v", axis, shape)
	}
	data, err := g.readDataAsFloat32(a.(*Array))
	if err != nil {
		return nil, fmt.Errorf("ggml: SplitAxis read: %w", err)
	}

	// Validate indices are within the axis extent and strictly increasing.
	dim := shape[axis]
	for i, idx := range indices {
		if idx < 0 || idx > dim {
			return nil, fmt.Errorf("ggml: SplitAxis index %d out of range [0,%d]", idx, dim)
		}
		if i > 0 && idx <= indices[i-1] {
			return nil, fmt.Errorf("ggml: SplitAxis indices must be increasing")
		}
	}
	if len(indices) == 0 {
		return []tensor.Array{a}, nil
	}

	// Precompute strides (row-major).
	stride := make([]int, len(shape))
	acc := 1
	for i := len(shape) - 1; i >= 0; i-- {
		stride[i] = acc
		acc *= shape[i]
	}

	// out extracts a sub-slice of data for chunk [start, end) along axis.
	out := func(start, end int) []float32 {
		n := acc / dim * (end - start)
		res := make([]float32, n)
		idx := make([]int, len(shape))
		// Start the walk at the chunk's origin, not at 0 — the wrap below
		// resets to `start`, so leaving this at 0 made the first row read
		// from the head of the axis and shifted the whole chunk.
		idx[axis] = start
		for i := range res {
			off := 0
			for d := 0; d < len(shape); d++ {
				off += idx[d] * stride[d]
			}
			res[i] = data[off]
			// increment row-major multi-index
			for d := len(shape) - 1; d >= 0; d-- {
				idx[d]++
				lim := shape[d]
				if d == axis {
					lim = end
				}
				if idx[d] < lim {
					break
				}
				idx[d] = 0
				if d == axis {
					idx[d] = start
				}
			}
		}
		return res
	}

	results := make([]tensor.Array, 0, len(indices)+1)
	prev := 0
	for _, idx := range indices {
		chunkShape := make([]int, len(shape))
		copy(chunkShape, shape)
		chunkShape[axis] = idx - prev
		arr, err := g.NewArrayFromFloat32(out(prev, idx), chunkShape)
		if err != nil {
			return nil, err
		}
		results = append(results, arr)
		prev = idx
	}
	chunkShape := make([]int, len(shape))
	copy(chunkShape, shape)
	chunkShape[axis] = dim - prev
	arr, err := g.NewArrayFromFloat32(out(prev, dim), chunkShape)
	if err != nil {
		return nil, err
	}
	results = append(results, arr)
	return results, nil
}

func (g *GGMLBackend) RepeatAxis(a tensor.Array, repeats, axis int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// Map row-major axis to GGML ne index.
	nd := len(a.Shape())
	ggmlAxis := ggmlAxisIndex(nd, axis)

	result := t
	if repeats > 1 {
		// Element-wise repeat (k0,k0,k1,k1), NOT tiling (k0,k1,k0,k1) — GQA
		// head expansion needs each KV head duplicated across its own query
		// group, so repeated copies must be adjacent. Chained ggml_concat
		// gives the tiled order, so instead flatten to
		// [inner, 1, n, outer] around the repeated axis, ggml_repeat into the
		// size-1 slot (which sits just below the axis in memory order, making
		// the copies adjacent), and merge the two axes back together.
		inner := 1
		for i := 0; i < ggmlAxis; i++ {
			inner *= int(C.tensor_ne(t, C.int(i)))
		}
		n := int(C.tensor_ne(t, C.int(ggmlAxis)))
		outer := 1
		for i := ggmlAxis + 1; i < 4; i++ {
			outer *= int(C.tensor_ne(t, C.int(i)))
		}

		src := C.ggml_reshape_4d(ctx, C.ggml_cont(ctx, t),
			C.int64_t(inner), 1, C.int64_t(n), C.int64_t(outer))
		tmpl := C.ggml_new_tensor_4d(ctx, t._type,
			C.int64_t(inner), C.int64_t(repeats), C.int64_t(n), C.int64_t(outer))
		rep := C.ggml_repeat(ctx, src, tmpl)

		// Restore the original ne layout with the repeated axis grown, rather
		// than leaving the dims below it merged into ne[0].
		target := make([]C.int64_t, 4)
		for i := 0; i < 4; i++ {
			target[i] = C.int64_t(C.tensor_ne(t, C.int(i)))
		}
		target[ggmlAxis] *= C.int64_t(repeats)
		result = C.ggml_reshape_4d(ctx, rep, target[0], target[1], target[2], target[3])
	}
	out, err := g.evalOp(result)
	if err != nil {
		return nil, err
	}
	// Update the logical shape along `axis`.
	if axis >= 0 && axis < nd {
		logical := append([]int(nil), a.Shape()...)
		logical[axis] *= repeats
		out.logicalShape = logical
		g.registerArray(out)
	}
	return out, nil
}

func (g *GGMLBackend) Pad(a tensor.Array, axes, low, high []int, padValue tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	p := [4]C.int{0, 0, 0, 0}
	for i := range axes {
		if i < 4 {
			p[axes[i]] = C.int(low[i] + high[i])
		}
	}
	result := C.ggml_pad(ctx, t, p[0], p[1], p[2], p[3])
	return g.evalOp(result)
}

func (g *GGMLBackend) Where(condition, x, y tensor.Array, s tensor.Stream) (tensor.Array, error) {
	// GGML has no direct where/cmp ops. Compose:
	// where(c, x, y) = c * x + (1 - c) * y
	// where c is a boolean mask (0 or 1). If the condition is not already
	// 0/1, apply step() to binarize it.
	ctx := g.ctxPtr()
	tc := condition.(*Array).tensor
	tx := x.(*Array).tensor
	ty := y.(*Array).tensor

	// Binarize: step(c) gives 1 where c > 0, 0 elsewhere
	cBin := C.ggml_unary(ctx, tc, C.GGML_UNARY_OP_STEP)
	// result = cBin * x + (1 - cBin) * y
	cx := C.ggml_mul(ctx, cBin, tx)
	// 1 - cBin: create ones tensor, subtract cBin
	ones := C.ggml_fill(ctx, C.ggml_dup_tensor(ctx, tc), 1.0)
	invC := C.ggml_sub(ctx, ones, cBin)
	cy := C.ggml_mul(ctx, invC, ty)
	result := C.ggml_add(ctx, cx, cy)
	return g.evalOp(result)
}

func (g *GGMLBackend) Tril(a tensor.Array, k int, s tensor.Stream) (tensor.Array, error) {
	// ggml_diag_mask_inf sets elements above position n_past to -inf,
	// which effectively zeros them after softmax. But for a general
	// lower-triangular mask (returning 0s above, original below), use
	// ggml_tri if available, or compose with diag_mask_inf + where.
	//
	// For the attention use case (creating a causal mask), diag_mask_inf
	// is exactly what we need.
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	result := C.ggml_diag_mask_inf(ctx, t, C.int(k))
	return g.evalOp(result)
}

// ── tensor.Backend: normalization ──────────────────────────────────

func (g *GGMLBackend) Softmax(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_soft_max(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) SoftmaxAxis(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_soft_max(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) FastRMSNorm(x, weight tensor.Array, eps float32, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	result := C.ggml_rms_norm(ctx, x.(*Array).tensor, C.float(eps))
	if weight != nil {
		result = C.ggml_mul(ctx, result, weight.(*Array).tensor)
	}
	return g.evalOp(result)
}

// ── tensor.Backend: attention ──────────────────────────────────────

// FastScaledDotProductAttention computes softmax(QK^T*scale + mask) @ V.
//
// It deliberately avoids ggml_flash_attn_ext: that op returns its result as
// ne=[Dv, n_head, n_tokens, B] (row-major [B,S,H,Dv]) while every caller here
// follows the MLX convention and expects [B,H,S,Dv], and its CPU path imposes
// F16/padding constraints on the mask. The explicit mul_mat → soft_max_ext →
// mul_mat chain below keeps the MLX layout end to end.
//
// q is [B,H,S,D] (ne=[D,S,H,B]); k and v are [B,H,Skv,D] with the KV heads
// already expanded to H by the caller.
func (g *GGMLBackend) FastScaledDotProductAttention(q, k, v tensor.Array, scale float32, maskMode string, maskArr, sinks tensor.Array, s tensor.Stream) (tensor.Array, error) {
	if sinks != nil {
		return nil, fmt.Errorf("ggml: attention sinks not supported")
	}
	ctx := g.ctxPtr()
	tq := q.(*Array).tensor
	tk := k.(*Array).tensor
	tv := v.(*Array).tensor

	nQ := int(C.tensor_ne(tq, 1))
	nKV := int(C.tensor_ne(tk, 1))

	// kq = K @ Q^T: mul_mat(a,b) treats a as [k cols, n rows] and b as
	// [k cols, m rows], giving [n, m]. With a=K (ne=[D,Skv,H,B]) and
	// b=Q (ne=[D,S,H,B]) the result is ne=[Skv,S,H,B].
	kq := C.ggml_mul_mat(ctx, tk, tq)
	if kq == nil {
		return nil, fmt.Errorf("ggml: attention QK^T failed")
	}

	var mask, ownedMask *C.struct_ggml_tensor
	switch {
	case maskArr != nil:
		mask = maskArr.(*Array).tensor
	case maskMode == "causal" && nQ > 1:
		// Query i sits at absolute position nKV-nQ+i, so it may attend to
		// keys j <= nKV-nQ+i. ne=[nKV,nQ] matches soft_max_ext's expected
		// mask layout and broadcasts over the head and batch dims.
		m, err := g.causalMask(nQ, nKV)
		if err != nil {
			return nil, err
		}
		mask, ownedMask = m, m
	}

	// soft_max_ext folds the scale and the additive mask into the softmax.
	probs := C.ggml_soft_max_ext(ctx, kq, mask, C.float(scale), 0.0)
	if probs == nil {
		return nil, fmt.Errorf("ggml: attention softmax failed")
	}

	// out = probs @ V. mul_mat needs V as [Skv cols, Dv rows] = ne=[Skv,Dv,H,B],
	// so transpose V's two leading dims and make it contiguous.
	vT := C.ggml_cont(ctx, C.ggml_transpose(ctx, tv))
	out := C.ggml_mul_mat(ctx, vT, probs)
	if out == nil {
		return nil, fmt.Errorf("ggml: attention PV failed")
	}

	arr, err := g.evalOpUnpin(out, ownedMask)
	if err != nil {
		return nil, fmt.Errorf("ggml: attention: %w", err)
	}
	// Result ne=[Dv,S,H,B] — MLX [B,H,S,Dv]. Set it explicitly rather than
	// relying on evalOp's ne reversal, which cannot tell a collapsed batch
	// dim from a real one.
	qShape := q.Shape()
	vShape := v.Shape()
	arr.logicalShape = []int{qShape[0], qShape[1], qShape[2], vShape[len(vShape)-1]}
	g.registerArray(arr)
	return arr, nil
}

// causalMask builds an additive [nQ, nKV] attention mask (ne=[nKV,nQ]) with
// 0 on allowed positions and -inf above the diagonal, aligned so the final
// query attends to the full key prefix.
func (g *GGMLBackend) causalMask(nQ, nKV int) (*C.struct_ggml_tensor, error) {
	t := C.ggml_new_tensor_2d(g.ctxPtr(), C.GGML_TYPE_F32, C.int64_t(nKV), C.int64_t(nQ))
	if t == nil {
		return nil, fmt.Errorf("ggml: causal mask allocation failed")
	}
	data := make([]byte, nQ*nKV*4)
	negInf := math.Float32bits(float32(math.Inf(-1)))
	offset := nKV - nQ
	for i := 0; i < nQ; i++ {
		limit := offset + i
		for j := limit + 1; j < nKV; j++ {
			binary.LittleEndian.PutUint32(data[(i*nKV+j)*4:], negInf)
		}
	}
	if err := g.pinTensorData(t, data); err != nil {
		return nil, err
	}
	return t, nil
}

// ── tensor.Backend: positional encoding ────────────────────────────

func (g *GGMLBackend) FastRoPE(x tensor.Array, dims int, traditional bool, base float64, scale float32, offset int, freqs tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	mode := C.int(0)
	if !traditional {
		mode = C.int(2) // GGML_ROPE_TYPE_NEOX
	}
	xt := x.(*Array).tensor

	// GGML's rope expects tokens on ne[2] (layout [n_embd, n_head, n_tokens]).
	// Every caller applies RoPE AFTER the [B,S,H,D]→[B,H,S,D] transpose (MLX
	// convention), giving GGML ne=[D,S,H,B] — tokens on ne[1], heads on ne[2].
	// Swap them before roping and swap back after, so downstream ops see the
	// same layout as MLX. This is driven by the logical rank, not by comparing
	// ne[1] against ne[2]: those are seqLen and numHeads, whose relative sizes
	// say nothing about which axis holds tokens.
	permuted := false
	if len(x.Shape()) == 4 {
		xt = C.ggml_cont(ctx, C.ggml_permute(ctx, xt, 0, 2, 1, 3))
		permuted = true
	}

	// Create position IDs tensor. Unpinned after the op is evaluated.
	nTokens := int(C.tensor_ne(xt, 2))
	if nTokens < 1 {
		nTokens = 1
	}
	posData := make([]int32, nTokens)
	for i := range posData {
		posData[i] = int32(offset + i)
	}
	posBytes := make([]byte, len(posData)*4)
	copy(posBytes, unsafe.Slice((*byte)(unsafe.Pointer(&posData[0])), len(posData)*4))
	pos := C.ggml_new_tensor_1d(ctx, C.GGML_TYPE_I32, C.int64_t(nTokens))
	if err := g.pinTensorData(pos, posBytes); err != nil {
		return nil, err
	}
	// ggml_rope would hardcode freq_base=10000; models like Qwen3 use 1e6, so
	// the caller's base has to go through rope_ext. ext_factor=0 disables
	// YaRN, leaving beta_fast/beta_slow inert.
	result := C.ggml_rope_ext(ctx, xt, pos, nil, C.int(dims), mode, 0,
		C.float(base), C.float(scale), 0.0, 1.0, 32.0, 1.0)

	// If we permuted the input, permute the result back to [D,S,H,B].
	if permuted {
		result = C.ggml_permute(ctx, result, 0, 2, 1, 3)
	}
	out, err := g.evalOpUnpin(result, pos)
	if err != nil {
		return nil, err
	}
	out.logicalShape = append([]int(nil), x.Shape()...)
	g.registerArray(out)
	return out, nil
}

// ── tensor.Backend: indexing ───────────────────────────────────────

func (g *GGMLBackend) GatherAxis(a, indices tensor.Array, axis int, sliceSizes []int, s tensor.Stream) (tensor.Array, error) {
	// Evaluate indices to read their data.
	if err := indices.Eval(); err != nil {
		return nil, fmt.Errorf("ggml: GatherAxis indices eval: %w", err)
	}

	// Read indices.
	var flat []int32
	totalLen := indices.Size()
	switch indices.Dtype() {
	case tensor.Int64:
		i64data, err := indices.Int64Data()
		if err != nil {
			return nil, fmt.Errorf("ggml: GatherAxis read int64 indices: %w", err)
		}
		flat = make([]int32, len(i64data))
		for i, v := range i64data {
			flat[i] = int32(v)
		}
	case tensor.Int32:
		raw, err := indices.RawBytes()
		if err != nil {
			return nil, fmt.Errorf("ggml: GatherAxis read int32 indices: %w", err)
		}
		flat = make([]int32, totalLen)
		for i := range flat {
			flat[i] = int32(uint32(raw[i*4]) | uint32(raw[i*4+1])<<8 | uint32(raw[i*4+2])<<16 | uint32(raw[i*4+3])<<24)
		}
	default:
		return nil, fmt.Errorf("ggml: GatherAxis unsupported dtype %v", indices.Dtype())
	}

	// Gather directly in Go: read the table data, index rows, create result.
	// This avoids passing the weight tensor through evalOp's graph allocator,
	// which would overwrite the weight's data pointer.
	if err := a.Eval(); err != nil {
		return nil, fmt.Errorf("ggml: GatherAxis table eval: %w", err)
	}

	// Quantized tables (Q4_0 etc.) can't be read as F32 directly. Use GGML's
	// native ggml_get_rows, which dequantizes only the gathered rows on the
	// fly (ARM-optimized kernel). Result is F32 [ne0=rowWidth, ne1=n].
	tType := a.(*Array).tensor._type
	if tType != C.GGML_TYPE_F32 && tType != C.GGML_TYPE_F16 && tType != C.GGML_TYPE_BF16 {
		// DEBUG: verify index bounds before ggml_get_rows asserts.
		ne0v := int(C.tensor_ne(a.(*Array).tensor, 0))
		ne1v := int(C.tensor_ne(a.(*Array).tensor, 1))
		maxIdx := -1
		for _, v := range flat {
			if int(v) > maxIdx {
				maxIdx = int(v)
			}
		}
		if maxIdx >= ne1v {
			fmt.Printf("ggml: GatherAxis DEBUG ne0=%d ne1=%d maxIdx=%d nIndices=%d — index out of range!\n", ne0v, ne1v, maxIdx, len(flat))
		}
		idxArr, err := g.NewArrayFromInt32(flat, []int{len(flat)})
		if err != nil {
			return nil, fmt.Errorf("ggml: GatherAxis create index tensor: %w", err)
		}
		defer idxArr.Free()
		result := C.ggml_get_rows(g.ctxPtr(), a.(*Array).tensor, idxArr.(*Array).tensor)
		out, oerr := g.evalOp(result)
		if oerr != nil {
			return nil, oerr
		}
		// GGML collapses trailing 1s (ne=[rowWidth,1] → 1D), but the model
		// layer expects the MLX rank convention: indices shape + table row
		// dims, e.g. [1, seqLen, hidden]. Set the logical shape explicitly.
		idxShape := indices.Shape()
		rowWidth := a.Shape()[len(a.Shape())-1]
		logical := make([]int, 0, len(idxShape)+1)
		logical = append(logical, idxShape...)
		logical = append(logical, rowWidth)
		out.logicalShape = logical
		g.registerArray(out)
		debugf("ggml: GatherAxis quantized result shape=%v idxLen=%d\n", out.Shape(), len(flat))
		return out, nil
	}

	tableData, err := a.Float32Data()
	if err != nil {
		return nil, fmt.Errorf("ggml: GatherAxis read table: %w", err)
	}

	// GGML ne[0] = row width (contiguous dim). In our row-major convention,
	// Shape() reverses this, so Shape()[1] = row width.
	tableShape := a.Shape()
	rowWidth := tableShape[len(tableShape)-1] // last dim = contiguous
	numRows := 1
	for i := 0; i < len(tableShape)-1; i++ {
		numRows *= tableShape[i]
	}

	// Build result: for each index, copy the corresponding row.
	resultData := make([]float32, len(flat)*rowWidth)
	for i, idx := range flat {
		if int(idx) < 0 || int(idx) >= numRows {
			return nil, fmt.Errorf("ggml: GatherAxis index %d out of range [0, %d)", idx, numRows)
		}
		copy(resultData[i*rowWidth:], tableData[int(idx)*rowWidth:(int(idx)+1)*rowWidth])
	}

	// Result shape: [len(flat), rowWidth] in row-major convention.
	out, err := g.NewArrayFromFloat32(resultData, []int{len(flat), rowWidth})
	if err != nil {
		return nil, err
	}
	// Preserve the MLX rank convention (indices shape + table row dims).
	// GGML would collapse [rowWidth,1] to 1D, losing the batch dim.
	idxShape := indices.Shape()
	logical := make([]int, 0, len(idxShape)+1)
	logical = append(logical, idxShape...)
	logical = append(logical, rowWidth)
	outArr := out.(*Array)
	outArr.logicalShape = logical
	g.registerArray(outArr)
	return out, nil
}

func (g *GGMLBackend) ArgMax(a tensor.Array, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_argmax(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) ArgMaxAxis(a tensor.Array, axis int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return g.evalOp(C.ggml_argmax(g.ctxPtr(), a.(*Array).tensor))
}

func (g *GGMLBackend) ArgPartitionAxis(a tensor.Array, kth, axis int, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: ArgPartitionAxis not implemented")
}

func (g *GGMLBackend) TakeAlongAxis(a, indices tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: TakeAlongAxis not implemented")
}

// ── tensor.Backend: convolution ────────────────────────────────────

func (g *GGMLBackend) Conv1D(input, weight tensor.Array, stride, padding, dilation, groups int, s tensor.Stream) (tensor.Array, error) {
	// DeltaNet-style depthwise conv1d. Weight is [C_out, K, C_in/groups]
	// (sanitized MLX layout); input is [B, S, C_in]. GGML's im2col+mul_mat
	// doesn't support grouped/depthwise 1D convs, so compute in Go. The
	// kernel is tiny (K=4) and this runs once per token, so Go is fine.
	if err := input.Eval(); err != nil {
		return nil, fmt.Errorf("ggml: Conv1D input eval: %w", err)
	}
	if err := weight.Eval(); err != nil {
		return nil, fmt.Errorf("ggml: Conv1D weight eval: %w", err)
	}
	inShape := input.Shape() // [B, S, C_in] or [S, C_in] (row-major)
	wShape := weight.Shape() // [C_out, K, C_in/groups]

	// DEBUG: print conv shapes
	debugf("ggml: Conv1D inShape=%v wShape=%v stride=%d padding=%d dilation=%d groups=%d\n", inShape, wShape, stride, padding, dilation, groups)

	// Accept 2D [S, C] input (GGML collapses the batch dim) as [1, S, C].
	B, S, Cin := 1, 0, 0
	switch len(inShape) {
	case 3:
		B, S, Cin = inShape[0], inShape[1], inShape[2]
	case 2:
		S, Cin = inShape[0], inShape[1]
	default:
		return nil, fmt.Errorf("ggml: Conv1D expects 2D/3D input, got %v", inShape)
	}
	Cout, K := 0, 0
	switch len(wShape) {
	case 3:
		Cout, K = wShape[0], wShape[1]
	case 2:
		Cout, K = wShape[0], wShape[1]
	default:
		return nil, fmt.Errorf("ggml: Conv1D expects 2D/3D weight, got %v", wShape)
	}
	if K <= 0 {
		return nil, fmt.Errorf("ggml: Conv1D kernel size %d", K)
	}
	if groups <= 0 || groups > Cin || Cin%groups != 0 {
		return nil, fmt.Errorf("ggml: Conv1D invalid groups %d for Cin=%d", groups, Cin)
	}

	inData, err := g.readDataAsFloat32(input.(*Array))
	if err != nil {
		return nil, fmt.Errorf("ggml: Conv1D read input: %w", err)
	}
	wData, err := g.readDataAsFloat32(weight.(*Array))
	if err != nil {
		return nil, fmt.Errorf("ggml: Conv1D read weight: %w", err)
	}

	// Depthwise/grouped cross-correlation: out[b, so, oc] = sum_k x[b, so*stride + k, ic] * w[oc, k, icg]
	// where oc maps to group g = oc / (Cout/groups) and ic = g*(Cin/groups) + icg.
	groupIn := Cin / groups
	groupOut := Cout / groups
	Sout := 0
	if S >= K {
		Sout = (S-K)/stride + 1
	}
	out := make([]float32, B*Sout*Cout)
	for b := 0; b < B; b++ {
		for so := 0; so < Sout; so++ {
			for oc := 0; oc < Cout; oc++ {
				gIdx := oc / groupOut
				icBase := gIdx * groupIn
				var acc float32
				for k := 0; k < K; k++ {
					si := so*stride + k*dilation - padding
					if si < 0 || si >= S {
						continue
					}
					for icg := 0; icg < groupIn; icg++ {
						acc += inData[(b*S+si)*Cin+icBase+icg] * wData[(oc*K+k)*groupIn+icg]
					}
				}
				out[(b*Sout+so)*Cout+oc] = acc
			}
		}
	}
	convArr, err := g.NewArrayFromFloat32(out, []int{B, Sout, Cout})
	if err != nil {
		return nil, err
	}
	// Preserve the batch/seq rank — GGML would collapse [Cout,1,B] → 1D.
	// Set logical shape [B, Sout, Cout] regardless of input rank.
	convArr.(*Array).logicalShape = []int{B, Sout, Cout}
	g.registerArray(convArr.(*Array))
	return convArr, nil
}

// readDataAsFloat32 reads a tensor's data as F32, converting BF16/F16 on the
// fly. Q4_0 etc. are not supported here (callers use ggml ops for those).
func (g *GGMLBackend) readDataAsFloat32(a *Array) ([]float32, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	if a.tensor == nil {
		return nil, fmt.Errorf("ggml: readDataAsFloat32 on nil tensor")
	}
	switch a.tensor._type {
	case C.GGML_TYPE_F32:
		return a.Float32Data()
	case C.GGML_TYPE_BF16:
		raw, err := a.RawBytes()
		if err != nil {
			return nil, err
		}
		out := make([]float32, len(raw)/2)
		for i := range out {
			b := uint32(binary.LittleEndian.Uint16(raw[i*2 : i*2+2]))
			out[i] = math.Float32frombits(b << 16)
		}
		return out, nil
	case C.GGML_TYPE_F16:
		raw, err := a.RawBytes()
		if err != nil {
			return nil, err
		}
		out := make([]float32, len(raw)/2)
		for i := range out {
			b := binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
			out[i] = float32(f16tof32(b))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("ggml: readDataAsFloat32 unsupported type %d", int(a.tensor._type))
	}
}

// ── tensor.Backend: quantization ───────────────────────────────────

func (g *GGMLBackend) Quantize(w tensor.Array, groupSize, bits int, mode string, s tensor.Stream) ([]tensor.Array, error) {
	// GGML native quantized types (Q4_0, Q4_1, etc.) differ from our affine
	// format (weights/scales/biases). For now, return the weight as-is
	// (full-precision path). The model layer handles the triplet format
	// at load time; the GGML backend sees F32 weights.
	return nil, nil
}

func (g *GGMLBackend) QuantizedMatMul(x, w, scales tensor.Array, biases tensor.Array, transpose bool, groupSize, bits int, mode string, s tensor.Stream) (tensor.Array, error) {
	// Our model layer loads quantized triplets (weights/scales/biases) and
	// dequantizes at load time for the GGML backend. So by the time we get
	// here, w is already F32. Use standard mul_mat.
	// If transpose is true, w is in [out, in] PyTorch layout (GGML convention).
	ctx := g.ctxPtr()
	if transpose {
		// ggml_mul_mat(ctx, w, x) computes x @ w^T = [batch, out]
		result := C.ggml_mul_mat(ctx, w.(*Array).tensor, x.(*Array).tensor)
		return g.evalOp(result)
	}
	result := C.ggml_mul_mat(ctx, w.(*Array).tensor, x.(*Array).tensor)
	return g.evalOp(result)
}

func (g *GGMLBackend) Dequantize(w, scales, biases tensor.Array, groupSize, bits int, mode string, s tensor.Stream) (tensor.Array, error) {
	// No-op for GGML backend — dequantization happens at load time in the
	// model layer before creating the GGML tensor. Return w as-is.
	return w, nil
}

func (g *GGMLBackend) GatherQuantizedMatMul(x, w, scales, biases, lhsIndices, rhsIndices tensor.Array, transpose bool, groupSize, bits int, mode string, sortedIndices bool, s tensor.Stream) (tensor.Array, error) {
	// Not used by the small-model path; stubbed.
	return nil, fmt.Errorf("ggml: GatherQuantizedMatMul not implemented")
}

// ── tensor.Backend: memory management ──────────────────────────────

func (g *GGMLBackend) SetCacheLimit(bytes uint64) error  { return nil }
func (g *GGMLBackend) SetMemoryLimit(bytes uint64) error { return nil }
func (g *GGMLBackend) ClearCache() error                 { return nil }
func (g *GGMLBackend) TotalSystemRAM() uint64 {
	return uint64(C.ggml_sysconf_page_size()) * uint64(C.ggml_sysconf_phys_pages())
}

func (b *GGMLBackend) EnableCompile() error { return nil }

func (g *GGMLBackend) NativeQuantization() bool { return false }

// f16tof32 converts a half-precision float bit pattern to float32.
func f16tof32(h uint16) float32 {
	sign := uint32(h>>15) & 1
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff
	var bits uint32
	switch {
	case exp == 0 && frac == 0:
		bits = sign << 31 // ±0
	case exp == 0:
		// subnormal: normalize
		e := uint32(127 - 15 + 1)
		for frac&0x400 == 0 {
			frac <<= 1
			e--
		}
		frac &= 0x3ff
		bits = sign<<31 | e<<23 | frac<<13
	case exp == 31:
		bits = sign<<31 | 0x7f800000 | frac<<13 // inf/nan
	default:
		bits = sign<<31 | (exp+127-15)<<23 | frac<<13
	}
	return math.Float32frombits(bits)
}
