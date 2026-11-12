package main

/*
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
#include <stdlib.h>
#include "plugin.h"
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Use case 4: a plugin host.
//
// One filter slot runs two plugins for the same stream. One is written in Go
// against the SDK handle. The other is native C and receives the stream
// pointer. Both set a response header.
//
// The config pointer: the config factory gets the filter config pointer with
// abi.HttpFilterConfigEnvoyPointer and gives it to the native plugin, which
// defines a counter on it. The native plugin adds 1 to that counter for each
// response, through the same pointer. The counter shows in the admin stats as
// dynamicmodulescustom.native_plugin_responses.

// plugin is what the host runs. Both kinds implement it.
type plugin interface {
	OnResponseHeaders(h shared.HttpFilterHandle, headers shared.HeaderMap)
}

// goPlugin uses only the Go SDK.
type goPlugin struct{}

func (goPlugin) OnResponseHeaders(_ shared.HttpFilterHandle, headers shared.HeaderMap) {
	headers.Set("x-go-plugin", "ok")
}

// nativePlugin is the C plugin. It holds the config pointer and counter id.
type nativePlugin struct {
	config unsafe.Pointer
	id     C.size_t
}

func (p nativePlugin) OnResponseHeaders(h shared.HttpFilterHandle, _ shared.HeaderMap) {
	stream, ok := abi.HttpFilterEnvoyPointer(h)
	if !ok {
		return
	}
	// Owner thread, inside the response headers callback.
	C.plugin_on_response_headers(stream, p.config, p.id)
}

type hostConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *hostConfigFactory) Create(h shared.HttpFilterConfigHandle, _ []byte) (shared.HttpFilterFactory, error) {
	cfg, ok := abi.HttpFilterConfigEnvoyPointer(h)
	if !ok {
		return nil, errors.New("no config pointer")
	}
	name := C.CString("native_plugin_responses")
	defer C.free(unsafe.Pointer(name))
	var id C.size_t
	// Config thread, while the config is created.
	if C.plugin_config_init(cfg, name, &id) != 0 {
		return nil, errors.New("define counter failed")
	}
	return &hostFactory{plugins: []plugin{goPlugin{}, nativePlugin{config: cfg, id: id}}}, nil
}

type hostFactory struct {
	shared.EmptyHttpFilterFactory
	plugins []plugin
}

func (f *hostFactory) Create(h shared.HttpFilterHandle) shared.HttpFilter {
	return &hostFilter{handle: h, plugins: f.plugins}
}

type hostFilter struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	plugins []plugin
}

func (f *hostFilter) OnResponseHeaders(h shared.HeaderMap, _ bool) shared.HeadersStatus {
	for _, p := range f.plugins {
		p.OnResponseHeaders(f.handle, h)
	}
	return shared.HeadersStatusContinue
}
