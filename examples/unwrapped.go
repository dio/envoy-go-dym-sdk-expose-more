package main

/*
#cgo CFLAGS: -I${SRCDIR}/../sdk-patched/abi
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
#cgo linux LDFLAGS: -Wl,--unresolved-symbols=ignore-all
#include <stdbool.h>
#include <stdlib.h>
#include "abi.h"
*/
import "C"

import (
	"strconv"
	"strings"
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Use case 1: call an ABI callback that the Go SDK does not wrap.
//
// The filter reads every value of the request header named in its config
// with envoy_dynamic_module_callback_http_get_header_values, through its own
// cgo. It reports the count and the values in two response headers.

type unwrappedConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *unwrappedConfigFactory) Create(_ shared.HttpFilterConfigHandle, cfg []byte) (shared.HttpFilterFactory, error) {
	name := strings.TrimSpace(string(cfg))
	if name == "" {
		name = "x-repeat"
	}
	return &unwrappedFactory{header: name}, nil
}

type unwrappedFactory struct {
	shared.EmptyHttpFilterFactory
	header string
}

func (f *unwrappedFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &unwrappedFilter{handle: h, header: f.header}
}

type unwrappedFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	header string
	count  int
	values []string
}

func (f *unwrappedFilter) OnRequestHeaders(_ shared.HeaderMap, _ bool) shared.HeadersStatus {
	ptr, ok := abi.HttpFilterEnvoyPointer(f.handle)
	if !ok {
		return shared.HeadersStatusContinue
	}
	// Called on the stream's owner thread, inside a callback.
	f.values = headerValues(ptr, f.header)
	f.count = len(f.values)
	return shared.HeadersStatusContinue
}

func (f *unwrappedFilter) OnResponseHeaders(h shared.HeaderMap, _ bool) shared.HeadersStatus {
	h.Set("x-repeat-count", strconv.Itoa(f.count))
	h.Set("x-repeat-values", strings.Join(f.values, "|"))
	return shared.HeadersStatusContinue
}

// headerValues returns a copy of all request header values for key. The
// buffers Envoy returns are borrowed until the end of the hook, so each value
// is copied into Go memory before it is kept.
func headerValues(ptr unsafe.Pointer, key string) []string {
	stream := C.envoy_dynamic_module_type_http_filter_envoy_ptr(ptr)
	k := []byte(key)
	kbuf := C.envoy_dynamic_module_type_module_buffer{
		ptr:    (*C.char)(unsafe.Pointer(unsafe.SliceData(k))),
		length: C.size_t(len(k)),
	}
	// The callback that returns all values needs a result array of the right
	// size, so first ask for the count with the single-value callback.
	var first C.envoy_dynamic_module_type_envoy_buffer
	var n C.size_t
	C.envoy_dynamic_module_callback_http_get_header(
		stream, C.envoy_dynamic_module_type_http_header_type_RequestHeader,
		kbuf, &first, 0, &n)
	if n == 0 {
		return nil
	}
	out := make([]C.envoy_dynamic_module_type_envoy_buffer, int(n))
	if !C.envoy_dynamic_module_callback_http_get_header_values(
		stream, C.envoy_dynamic_module_type_http_header_type_RequestHeader,
		kbuf, &out[0]) {
		return nil
	}
	values := make([]string, 0, int(n))
	for _, b := range out {
		values = append(values, C.GoStringN((*C.char)(unsafe.Pointer(b.ptr)), C.int(b.length)))
	}
	return values
}
