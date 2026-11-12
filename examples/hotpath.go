package main

/*
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
#include <stdlib.h>
#include "hotpath.h"
*/
import "C"

import (
	"strings"
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Use case 3: a native hot path.
//
// The filter buffers the request body. When the body is complete, C code
// searches it in Envoy's buffers through the body callbacks. The body is never
// copied into Go. The pattern comes from the filter config.

type hotpathConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *hotpathConfigFactory) Create(_ shared.HttpFilterConfigHandle, cfg []byte) (shared.HttpFilterFactory, error) {
	p := strings.TrimSpace(string(cfg))
	if p == "" {
		p = "forbidden"
	}
	return &hotpathFactory{pattern: p}, nil
}

type hotpathFactory struct {
	shared.EmptyHttpFilterFactory
	pattern string
}

func (f *hotpathFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &hotpathFilter{handle: h, pattern: f.pattern}
}

type hotpathFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	pattern string
}

// OnRequestHeaders holds the headers until the body has been checked.
func (f *hotpathFilter) OnRequestHeaders(_ shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	if endOfStream {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (f *hotpathFilter) OnRequestBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}
	ptr, ok := abi.HttpFilterEnvoyPointer(f.handle)
	if !ok {
		return shared.BodyStatusContinue
	}
	pat := C.CString(f.pattern)
	defer C.free(unsafe.Pointer(pat))
	// Owner thread, inside the request body callback.
	if C.body_contains(ptr, pat, C.size_t(len(f.pattern))) != 0 {
		f.handle.SendLocalResponse(403, nil, []byte("body rejected\n"), "body_rejected")
		return shared.BodyStatusStopAndBuffer
	}
	return shared.BodyStatusContinue
}
