# Go SDK: expose the Envoy pointer of an HTTP filter stream

A proposed change to the Envoy dynamic modules Go SDK
(`source/extensions/dynamic_modules/sdk/go/abi`), against
b2d89e0ae539 (Envoy v1.40.2). This repository holds the patch, runnable
examples for each use case, and tests that run the examples in a real Envoy.

## Problem

The Go SDK wraps the raw stream pointer
(`envoy_dynamic_module_type_http_filter_envoy_ptr`) in `dymHttpFilterHandle`.
The field `hostPluginPtr` is unexported, and no public function returns it.
A Go module therefore cannot pass a stream to native code in the same process,
and cannot call an ABI callback that the Go SDK does not wrap. The same holds
for the filter config pointer in `dymConfigHandle`.

## Change

Two new exported functions in package `abi`:

```go
func HttpFilterEnvoyPointer(h shared.HttpFilterHandle) (unsafe.Pointer, bool)
func HttpFilterConfigEnvoyPointer(h shared.HttpFilterConfigHandle) (unsafe.Pointer, bool)
```

- They do not change the `shared.HttpFilterHandle` or
  `shared.HttpFilterConfigHandle` interfaces, so mocks, fakes and other
  implementations are unaffected.
- They return false for a handle that the package did not create.
- They expose nothing beyond the ABI. The pointers are the ones Envoy passes to
  every module in `on_new_http_filter` and `on_http_filter_config_new`.
- The rules are those of the ABI: owner thread only, during a callback or a
  scheduled function, and not after the stream or config is destroyed. Buffers
  that Envoy returns are borrowed until the end of the hook.

The patch is `patch/http-filter-envoy-pointer.patch`. It adds
`sdk/go/abi/envoy_pointer.go` and `sdk/go/abi/envoy_pointer_test.go`.

## Use cases

Each use case is a filter in `examples/`. One library registers all four, and
each filter runs on its own listener in the tests.

### 1. ABI callbacks the Go SDK does not wrap

At b2d89e0ae539, `abi.h` has HTTP filter callbacks with no Go wrapper, for
example `get_header_values`, `get_filter_state_object`,
`set_filter_state_object`, `set_dynamic_metadata_string_batch`,
`get_upstream_connection_id`, `record_histogram_value_vec` and
`span_disable_local_decision`. With the pointer, a Go module can call a new
callback through its own cgo as soon as it is in `abi.h`, without waiting for a
Go wrapper.

```go
ptr, _ := abi.HttpFilterEnvoyPointer(handle)
stream := C.envoy_dynamic_module_type_http_filter_envoy_ptr(ptr)
C.envoy_dynamic_module_callback_http_get_header(stream, RequestHeader, key, &first, 0, &n)
values := make([]C.envoy_dynamic_module_type_envoy_buffer, n)
C.envoy_dynamic_module_callback_http_get_header_values(stream, RequestHeader, key, &values[0])
for _, v := range values {
    out = append(out, C.GoStringN(v.ptr, v.length)) // copy before the hook ends
}
```

Files: `examples/unwrapped.go`. The filter reports the count and the values of a
repeated request header in two response headers.

### 2. Reusing a native filter library from a Go module

A Go module can use an existing Rust or C library, such as a WAF, a scanner or
a parser, and let it read headers, bodies or metadata of the same stream.
Without the pointer, every access goes back through Go with a copy, or the
library has to be a separate Envoy filter with its own configuration.

```go
ptr, _ := abi.HttpFilterEnvoyPointer(handle)
verdict := C.scan_request_headers(ptr, rules, nRules) // C calls get_headers_size and get_headers
if verdict.blocked != 0 {
    handle.SendLocalResponse(403, nil, []byte("blocked\n"), "scan_blocked")
    return shared.HeadersStatusStop
}
return shared.HeadersStatusContinue
```

Files: `examples/native.go`, `examples/scan.c`, `examples/scan.h`. The library
knows nothing about Go. It only needs a stream pointer.

### 3. Native hot paths

Code that scans bodies or rewrites headers can read Envoy's buffers directly
with the body and header callbacks, instead of crossing into Go and copying
each chunk.

```go
func OnRequestBody(_ BodyBuffer, endOfStream bool) BodyStatus {
    if !endOfStream { return BodyStatusStopAndBuffer }
    ptr, _ := abi.HttpFilterEnvoyPointer(handle)
    // C walks get_body_chunks for the buffered and received body, in place.
    if C.body_contains(ptr, pattern, len(pattern)) != 0 {
        handle.SendLocalResponse(403, nil, []byte("body rejected\n"), "body_rejected")
        return BodyStatusStopAndBuffer
    }
    return BodyStatusContinue
}
```

Files: `examples/hotpath.go`, `examples/hotpath.c`, `examples/hotpath.h`. A
pattern split over two chunks is found.

### 4. Plugin hosts

A Go module that runs several plugins, in different languages, inside one HTTP
filter can let each plugin access Envoy state for the stream directly. The
example also uses the config pointer. The config factory gets it with
`HttpFilterConfigEnvoyPointer`, and a native plugin defines a counter on it and
increments it for each response.

```go
cfg, _ := abi.HttpFilterConfigEnvoyPointer(configHandle)
id := C.plugin_config_init(cfg, "native_plugin_responses") // define_counter on the config pointer
plugins := []plugin{goPlugin{}, nativePlugin{cfg, id}}

func OnResponseHeaders(headers HeaderMap, _ bool) HeadersStatus {
    for _, p := range plugins { p.OnResponseHeaders(handle, headers) } // Go sets x-go-plugin
    return HeadersStatusContinue // native: set_header x-native-plugin, config_increment_counter
}
```

Files: `examples/host.go`, `examples/plugin.c`, `examples/plugin.h`. Both headers
appear on the response, and the admin stats show the counter.

## Run the examples

Requirements: Go 1.25.6 or newer, a C compiler, and an Envoy binary of exactly
v1.40.2 (the commit b2d89e0ae539) for the Envoy tests.

```sh
go run ./cmd/patchsdk
ENVOY_BIN=/path/to/envoy go test -v ./examples/...
```

`cmd/patchsdk` copies the pinned SDK module from the Go module cache into
`sdk-patched/` (ignored by git) and adds the files of the patch. The module
`replace` in `go.mod` points at that directory. To use your own Envoy checkout
instead, apply the patch there with `git apply patch/*.patch` and point the
`replace` at `<checkout>/source/extensions/dynamic_modules`. The examples find
`abi.h` through `sdk-patched/abi`, so edit the `#cgo CFLAGS` line in
`examples/unwrapped.go` as well.

Without `ENVOY_BIN` the Envoy test is skipped with a message. The test builds
the library with `go build -buildmode=c-shared`, writes a bootstrap from
`examples/envoy.yaml` with one listener per filter, starts Envoy, waits for the
admin `/ready`, and checks a positive and a negative request for each filter.
It sets `GODEBUG=cgocheck=0` for Envoy because the Go SDK hands Go pointers to
Envoy as opaque handles.

The unit test of the patch runs in the patched SDK:

```sh
cd sdk-patched/sdk/go/abi
go test -v .                                              # macOS
CGO_LDFLAGS=-Wl,--unresolved-symbols=ignore-all go test -v .   # Linux
```

The `abi` package refers to symbols that Envoy provides. The test never calls
them, so Linux needs the flag to link the test binary. macOS links with
dynamic lookup already. The examples set the flag for themselves in their
`#cgo` lines.

The unit tests and the examples run on macOS and Linux with an Envoy binary of
the same version. CI (`.github/workflows/ci.yml`) runs them on Linux x86_64 and macOS arm64
with the v1.40.2 builds from
[tetratelabs/archive-envoy](https://github.com/tetratelabs/archive-envoy),
checked against the SHA-256 values published there. It also checks that `envoy --version` reports the commit b2d89e0ae539, which is
the SDK pin, so the ABI matches.
## License

Apache-2.0, as Envoy.
