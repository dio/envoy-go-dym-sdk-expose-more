package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
)

// These tests build the module, start a real Envoy, and send requests.
//
//	ENVOY_BIN=/path/to/envoy \
//	CGO_CFLAGS=-I<envoy>/source/extensions/dynamic_modules/abi \
//	go test -v
//
// Without ENVOY_BIN the tests are skipped.

type filterPort struct {
	Name   string
	Config string
	Port   int
}

// startEnvoy builds the library, writes the bootstrap, starts Envoy and waits
// until the admin endpoint reports ready.
func startEnvoy(t *testing.T) map[string]int {
	t.Helper()
	envoyBin := os.Getenv("ENVOY_BIN")
	if envoyBin == "" {
		t.Skip("ENVOY_BIN is not set; set it to an Envoy binary to run these tests")
	}
	dir := t.TempDir()

	build := exec.Command("go", "build", "-buildmode=c-shared", "-o", filepath.Join(dir, "libptrdemo.so"), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build module: %v\n%s", err, out)
	}

	admin := freePort(t)
	filters := []filterPort{
		{Name: "unwrapped", Config: "x-repeat", Port: freePort(t)},
		{Name: "native", Port: freePort(t)},
		{Name: "hotpath", Config: "forbidden", Port: freePort(t)},
		{Name: "host", Port: freePort(t)},
	}
	tmpl, err := template.ParseFiles("envoy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg bytes.Buffer
	if err := tmpl.Execute(&cfg, map[string]any{"Admin": admin, "Filters": filters}); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "envoy.yaml")
	if err := os.WriteFile(cfgPath, cfg.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var logBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, envoyBin, "-c", cfgPath, "--concurrency", "2", "--base-id", fmt.Sprint(admin))
	// The Go SDK returns Go pointers to Envoy as opaque handles, which the cgo
	// pointer check rejects, so the check is off for the module.
	cmd.Env = append(os.Environ(), "ENVOY_DYNAMIC_MODULES_SEARCH_PATH="+dir,
		"GODEBUG=cgocheck=0")
	cmd.Stdout, cmd.Stderr = &logBuf, &logBuf
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		cancel()
		<-exited
		if t.Failed() {
			t.Logf("envoy log:\n%s", logBuf.String())
		}
	})

	adminURL = fmt.Sprintf("http://127.0.0.1:%d", admin)
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-exited:
			t.Fatalf("envoy exited early:\n%s", logBuf.String())
		default:
		}
		if resp, err := http.Get(adminURL + "/ready"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("envoy not ready:\n%s", logBuf.String())
		}
		time.Sleep(50 * time.Millisecond)
		continue
	}
	m := map[string]int{}
	for _, f := range filters {
		m[f.Name] = f.Port
	}
	return m
}

var adminURL string

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// do sends a request and returns the response and its body.
func do(t *testing.T, port int, method, body string, hdr http.Header) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d/", port), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// TestFilters starts Envoy once and runs every use case against it.
func TestFilters(t *testing.T) {
	p := startEnvoy(t)

	t.Run("unwrapped", func(t *testing.T) {
		resp, _ := do(t, p["unwrapped"], "GET", "", http.Header{"X-Repeat": {"a", "b", "c"}})
		if got := resp.Header.Get("x-repeat-count"); got != "3" {
			t.Errorf("x-repeat-count = %q, want 3", got)
		}
		if got := resp.Header.Get("x-repeat-values"); got != "a|b|c" {
			t.Errorf("x-repeat-values = %q, want a|b|c", got)
		}
		resp, _ = do(t, p["unwrapped"], "GET", "", nil)
		if got := resp.Header.Get("x-repeat-count"); got != "0" {
			t.Errorf("without the header, x-repeat-count = %q, want 0", got)
		}
	})

	t.Run("native", func(t *testing.T) {
		resp, _ := do(t, p["native"], "GET", "", http.Header{"User-Agent": {"Mozilla BadBot/1.0"}})
		if resp.StatusCode != 403 || resp.Header.Get("x-scan-rule") != "0" {
			t.Errorf("bad user agent: status %d rule %q, want 403 rule 0", resp.StatusCode, resp.Header.Get("x-scan-rule"))
		}
		resp, _ = do(t, p["native"], "GET", "", http.Header{"X-Query": {"1 UNION  SELECT"}})
		if resp.StatusCode != 200 {
			t.Errorf("two spaces must not match: status %d, want 200", resp.StatusCode)
		}
		resp, _ = do(t, p["native"], "GET", "", http.Header{"X-Query": {"1 Union Select 2"}})
		if resp.StatusCode != 403 || resp.Header.Get("x-scan-rule") != "1" {
			t.Errorf("sql pattern: status %d rule %q, want 403 rule 1", resp.StatusCode, resp.Header.Get("x-scan-rule"))
		}
		resp, body := do(t, p["native"], "GET", "", http.Header{"User-Agent": {"curl/8"}})
		if resp.StatusCode != 200 || body != "ok\n" {
			t.Errorf("clean request: status %d body %q, want 200 ok", resp.StatusCode, body)
		}
	})

	t.Run("hotpath", func(t *testing.T) {
		resp, _ := do(t, p["hotpath"], "POST", "hello forbidden world", nil)
		if resp.StatusCode != 403 {
			t.Errorf("body with pattern: status %d, want 403", resp.StatusCode)
		}
		resp, body := do(t, p["hotpath"], "POST", "hello world", nil)
		if resp.StatusCode != 200 || body != "ok\n" {
			t.Errorf("clean body: status %d body %q, want 200 ok", resp.StatusCode, body)
		}
		resp, _ = do(t, p["hotpath"], "GET", "", nil)
		if resp.StatusCode != 200 {
			t.Errorf("no body: status %d, want 200", resp.StatusCode)
		}
	})

	t.Run("hotpath_chunks", func(t *testing.T) {
		// The pattern is split over two chunks of a chunked request.
		pr, pw := io.Pipe()
		go func() {
			io.WriteString(pw, "aaa forb")
			io.WriteString(pw, "idden bbb")
			pw.Close()
		}()
		req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/", p["hotpath"]), pr)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("split pattern: status %d, want 403", resp.StatusCode)
		}
	})

	t.Run("host", func(t *testing.T) {
		const n = 3
		for i := 0; i < n; i++ {
			resp, _ := do(t, p["host"], "GET", "", nil)
			if resp.Header.Get("x-go-plugin") != "ok" || resp.Header.Get("x-native-plugin") != "ok" {
				t.Fatalf("headers x-go-plugin=%q x-native-plugin=%q, want both ok",
					resp.Header.Get("x-go-plugin"), resp.Header.Get("x-native-plugin"))
			}
		}
		// The native plugin counts through the config pointer.
		resp, err := http.Get(adminURL + "/stats?filter=native_plugin_responses")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), "native_plugin_responses: 3") {
			t.Errorf("stats = %q, want native_plugin_responses: 3", b)
		}
	})

	// Other filters must not have the host's headers.
	resp, _ := do(t, p["unwrapped"], "GET", "", nil)
	if resp.Header.Get("x-native-plugin") != "" {
		t.Errorf("x-native-plugin leaked to another listener")
	}
}
