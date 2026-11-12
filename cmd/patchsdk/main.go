// Command patchsdk copies the pinned Envoy dynamic modules SDK module from the
// Go module cache into ./sdk-patched and applies patch/*.patch to it. The
// examples use that directory through a replace directive.
//
// The patch must only add files. Run it from the repository root.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	module  = "github.com/envoyproxy/envoy/source/extensions/dynamic_modules"
	version = "v0.0.0-20261027011328-b2d89e0ae539"
	// The patch paths are relative to the Envoy repository root. The module
	// root is this directory below it.
	moduleRoot = "source/extensions/dynamic_modules/"
	outDir     = "sdk-patched"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "patchsdk:", err)
		os.Exit(1)
	}
}

func run() error {
	src, err := cacheDir()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := copyTree(src, outDir); err != nil {
		return err
	}
	patches, err := filepath.Glob("patch/*.patch")
	if err != nil {
		return err
	}
	for _, p := range patches {
		files, err := parseAdded(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for path, content := range files {
			rel, ok := strings.CutPrefix(path, moduleRoot)
			if !ok {
				return fmt.Errorf("%s: path %s is outside %s", p, path, moduleRoot)
			}
			dst := filepath.Join(outDir, rel)
			if _, err := os.Stat(dst); err == nil {
				return fmt.Errorf("%s already exists; the patch only adds files", dst)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, content, 0o644); err != nil {
				return err
			}
			fmt.Println("added", dst)
		}
	}
	return nil
}

// cacheDir returns the directory of the pinned module in the module cache,
// downloading it when needed.
func cacheDir() (string, error) {
	tmp, err := os.MkdirTemp("", "patchsdk")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	cmd := exec.Command("go", "mod", "download", "-json", module+"@"+version)
	cmd.Dir = tmp // outside this module, so its replace is not consulted
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go mod download: %v\n%s", err, errb.String())
	}
	var info struct{ Dir string }
	if err := json.Unmarshal(out.Bytes(), &info); err != nil || info.Dir == "" {
		return "", fmt.Errorf("unexpected go mod download output: %s", out.String())
	}
	return info.Dir, nil
}

// copyTree copies a directory and makes the files writable.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// parseAdded reads a unified diff that only adds files and returns the new
// content by path (without the b/ prefix).
func parseAdded(path string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	files := map[string][]byte{}
	var cur string
	inHunk := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur, inHunk = "", false
		case strings.HasPrefix(line, "--- "):
			if line != "--- /dev/null" {
				return nil, fmt.Errorf("only new files are supported: %q", line)
			}
		case strings.HasPrefix(line, "+++ b/") && !inHunk:
			cur = strings.TrimPrefix(line, "+++ b/")
			files[cur] = nil
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(line, "+"):
			files[cur] = append(files[cur], line[1:]+"\n"...)
		case inHunk && strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file": not expected.
		}
	}
	return files, sc.Err()
}
