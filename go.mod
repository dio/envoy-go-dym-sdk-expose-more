module github.com/dio/envoy-go-dym-sdk-expose-more

go 1.25.6

require github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0

// sdk-patched is created by `go run ./cmd/patchsdk`.
replace github.com/envoyproxy/envoy/source/extensions/dynamic_modules => ./sdk-patched
