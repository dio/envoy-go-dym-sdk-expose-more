package main

/*
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
#include <stdlib.h>
#include "scan.h"
*/
import "C"

import (
	"strconv"
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Use case 2: reuse a native library from a Go filter.
//
// scan.c knows nothing about Go. The filter hands it the stream pointer. The
// library reads the request headers through the ABI and returns a verdict.
// The filter answers 403 on a match.

type nativeConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *nativeConfigFactory) Create(_ shared.HttpFilterConfigHandle, _ []byte) (shared.HttpFilterFactory, error) {
	// The rules live in C memory for the life of the config.
	specs := []struct{ header, needle string }{
		{"user-agent", "badbot"},
		{"", "union select"}, // any header
	}
	rules := (*[1 << 10]C.scan_rule)(C.calloc(C.size_t(len(specs)), C.size_t(unsafe.Sizeof(C.scan_rule{}))))[:len(specs):len(specs)]
	for i, s := range specs {
		if s.header != "" {
			rules[i].header = C.CString(s.header)
		}
		rules[i].needle = C.CString(s.needle)
	}
	return &nativeFactory{rules: &rules[0], n: len(specs)}, nil
}

type nativeFactory struct {
	shared.EmptyHttpFilterFactory
	rules *C.scan_rule
	n     int
}

func (f *nativeFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &nativeFilter{handle: h, f: f}
}

type nativeFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	f      *nativeFactory
}

func (n *nativeFilter) OnRequestHeaders(_ shared.HeaderMap, _ bool) shared.HeadersStatus {
	ptr, ok := abi.HttpFilterEnvoyPointer(n.handle)
	if !ok {
		return shared.HeadersStatusContinue
	}
	// Owner thread, inside the request headers callback.
	v := C.scan_request_headers(ptr, n.f.rules, C.int(n.f.n))
	if v.blocked != 0 {
		n.handle.SendLocalResponse(403, [][2]string{{"x-scan-rule", strconv.Itoa(int(v.rule_index))}}, []byte("blocked\n"), "scan_blocked")
		return shared.HeadersStatusStop
	}
	return shared.HeadersStatusContinue
}
