// Command examples builds one Envoy dynamic module that registers four HTTP
// filters. Each filter shows one use of abi.HttpFilterEnvoyPointer.
//
// Build:
//
//	CGO_CFLAGS=-I<envoy>/source/extensions/dynamic_modules/abi \
//	  go build -buildmode=c-shared -o libptrdemo.so .
package main

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

func init() {
	sdk.RegisterHttpFilterConfigFactories(map[string]shared.HttpFilterConfigFactory{
		"unwrapped": &unwrappedConfigFactory{},
		"native":    &nativeConfigFactory{},
		"hotpath":   &hotpathConfigFactory{},
		"host":      &hostConfigFactory{},
	})
}

func main() {}
