//go:build darwin || linux

package tests

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestResources(t *testing.T) {
	cc := os.Getenv("HOSTCC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Fatal(err)
	}
	src, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(name string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}
	mustRun := func(name string, args ...string) []byte {
		t.Helper()
		out, err := run(name, args...)
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		return out
	}
	generator := filepath.Join(dir, "embed_resources")
	mustRun(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", filepath.Join(src, "embed_resources.c"), "-o", generator)
	// Set process limits in a child so the Go test process stays unchanged.
	helperSource := filepath.Join(dir, "helper.c")
	write(helperSource, []byte(`#define _POSIX_C_SOURCE 200809L
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <unistd.h>
int main(int argc, char **argv) {
 if (argc < 3) return 2;
 if (!strcmp(argv[1], "read")) {
  if (setgid(65534) || setuid(65534)) return 3;
  FILE *file = fopen(argv[2], "rb"); if (!file) return 4;
  int c; while ((c = fgetc(file)) != EOF) putchar(c);
  return fclose(file);
 }
 if (!strcmp(argv[1], "limit")) {
  struct rlimit limit = {256, 256};
  if (signal(SIGXFSZ, SIG_IGN) == SIG_ERR || setrlimit(RLIMIT_FSIZE, &limit)) return 5;
 }
 umask(0077);
 execv(argv[2], argv + 2); return 6;
}
`))
	helper := filepath.Join(dir, "helper")
	mustRun(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", helperSource, "-o", helper)
	header := filepath.Join(dir, "embedded_resources.h")
	generate := func(mode string, sources ...string) ([]byte, error) {
		return run(helper, append([]string{mode, generator, header}, sources...)...)
	}
	cleanTemps := func(t *testing.T) {
		t.Helper()
		paths, err := filepath.Glob(header + ".tmp.*")
		if err != nil || len(paths) != 0 {
			t.Fatalf("temporary outputs: %v, %v", paths, err)
		}
	}
	t.Run("binary-empty-roundtrip-and-permissions", func(t *testing.T) {
		binary := make([]byte, 768)
		for i := range binary {
			binary[i] = byte(i)
		}
		values := [][]byte{binary, {}, []byte("no final newline")}
		names := []string{"binary", "empty", "plain.txt"}
		paths := []string{}
		for i, name := range names {
			path := filepath.Join(dir, name)
			write(path, values[i])
			paths = append(paths, path)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if attempt != 0 {
				if err := os.Chmod(header, 0600); err != nil {
					t.Fatal(err)
				}
			}
			out, err := generate("umask", paths...)
			if err != nil {
				t.Fatalf("generate: %v %s", err, out)
			}
			info, err := os.Stat(header)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0644 {
				t.Fatalf("mode: %v", info.Mode())
			}
		}
		driver := filepath.Join(dir, "roundtrip.c")
		write(driver, []byte(`#include <stdio.h>
#include <string.h>
#include "embedded_resources.h"
int main(void) {
 const struct nh_resource *r = nh_embedded_resources;
 const char *names[] = {"binary", "empty", "plain.txt"};
 for (int i = 0; i < 3; i++, r++) {
  if (!r->name || strcmp(r->name,names[i]) || r->data[r->size]) return 1;
  if (fwrite(r->data,1,r->size,stdout) != r->size) return 2;
 }
 return r->name || r->data || r->size;
}
`))
		checker := filepath.Join(dir, "roundtrip")
		mustRun(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", "-I"+src, driver, "-o", checker)
		got := mustRun(checker)
		if !bytes.Equal(got, bytes.Join(values, nil)) {
			t.Fatal("binary resources changed")
		}
		cleanTemps(t)
	})
	t.Run("bad-input-preserves-output", func(t *testing.T) {
		first := filepath.Join(dir, "first")
		second := filepath.Join(dir, "second")
		for _, folder := range []string{first, second} {
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(folder, "same"), []byte(folder))
		}
		fifo := filepath.Join(dir, "fifo")
		mustRun("mkfifo", fifo)
		for _, sources := range [][]string{{filepath.Join(first, "same"), filepath.Join(second, "same")}, {filepath.Join(first, "same"), filepath.Join(dir, "missing")}, {filepath.Join(dir, "bad-é")}, {first}, {fifo}} {
			write(header, []byte("previous"))
			out, err := generate("umask", sources...)
			if err == nil {
				t.Fatalf("accepted %q: %s", sources, out)
			}
			got, err := os.ReadFile(header)
			if err != nil || string(got) != "previous" {
				t.Fatalf("previous output changed: %q, %v", got, err)
			}
			cleanTemps(t)
		}
	})
	t.Run("partial-write-preserves-output", func(t *testing.T) {
		source := filepath.Join(dir, "large")
		write(source, bytes.Repeat([]byte{255}, 16384))
		write(header, []byte("previous"))
		if out, err := generate("limit", source); err == nil {
			t.Fatalf("output limit ignored: %s", out)
		}
		got, err := os.ReadFile(header)
		if err != nil || string(got) != "previous" {
			t.Fatalf("previous output changed: %q %v", got, err)
		}
		cleanTemps(t)
	})
	t.Run("replacement-does-not-follow-symlink", func(t *testing.T) {
		target := filepath.Join(dir, "target")
		write(target, []byte("keep"))
		if err := os.Remove(header); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, header); err != nil {
			t.Fatal(err)
		}
		if out, err := generate("umask"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		info, err := os.Lstat(header)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("output is still a symlink")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "keep" {
			t.Fatalf("symlink target changed: %q %v", got, err)
		}
	})
	t.Run("failed-rename-cleans-up", func(t *testing.T) {
		if err := os.Remove(header); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(header, 0700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(header, "marker")
		write(marker, []byte("keep"))
		if out, err := generate("umask"); err == nil {
			t.Fatalf("replaced a directory: %s", out)
		}
		cleanTemps(t)
		got, err := os.ReadFile(marker)
		if err != nil || string(got) != "keep" {
			t.Fatal("destination changed")
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(header); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing-output", func(t *testing.T) {
		if out, err := run(generator); err == nil || !bytes.Contains(out, []byte("Usage:")) {
			t.Fatalf("missing usage error: %v %s", err, out)
		}
	})
	t.Run("another-uid-can-read-replacements", func(t *testing.T) {
		if runtime.GOOS != "linux" || os.Geteuid() != 0 {
			t.Skip("requires root Linux")
		}
		for _, path := range []string{filepath.Dir(dir), dir} {
			if err := os.Chmod(path, 0755); err != nil {
				t.Fatal(err)
			}
		}
		for attempt := 0; attempt < 2; attempt++ {
			if attempt != 0 {
				if err := os.Chmod(header, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := generate("umask"); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			expected, err := os.ReadFile(header)
			if err != nil {
				t.Fatal(err)
			}
			got := mustRun(helper, "read", header)
			if !bytes.Equal(got, expected) {
				t.Fatal("other UID could not read generated data")
			}
		}
	})
}

func TestStaticInitializer(t *testing.T) {
	src, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, compiler := range []string{"cc", "c++"} {
		t.Run(compiler, func(t *testing.T) {
			ext, standard := ".c", "gnu11"
			if compiler == "c++" {
				ext, standard = ".cc", "gnu++11"
			}
			source := filepath.Join(dir, "initializer"+ext)
			code := []byte("#include <NickelHook.h>\nstatic struct nh_info info = {.name = \"test\"};\nNickelHook(.info = &info)\nint main(void) { return NickelHook.info != &info; }\n")
			if err := os.WriteFile(source, code, 0600); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(dir, "initializer")
			cmd := exec.Command(compiler, "-std="+standard, "-pedantic-errors", "-Wno-strict-prototypes", "-Wall", "-Wextra", "-Werror", "-Wno-missing-field-initializers", "-I"+src, source, "-o", binary)
			// Designated initialization is a GNU C++11 extension used by the public API.
			if compiler == "c++" {
				cmd = exec.Command(compiler, "-std="+standard, "-Wall", "-Wextra", "-Werror", "-Wno-missing-field-initializers", "-I"+src, source, "-o", binary)
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if out, err := exec.Command(binary).CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}

func TestCMakeResources(t *testing.T) {
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake required")
	}
	src, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project with spaces")
	build := filepath.Join(dir, "build with spaces")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("data", "first")
	write("check.c", "#include <stdio.h>\n#include \"embedded_resources.h\"\nint main(void) { const struct nh_resource *r = nh_embedded_resources; return fwrite(r->data, 1, r->size, stdout) != r->size; }\n")
	write("CMakeLists.txt", "cmake_minimum_required(VERSION 3.18)\nproject(ResourceTest C)\ninclude(\""+filepath.ToSlash(filepath.Join(src, "resources.cmake"))+"\")\nadd_executable(check check.c)\ntarget_include_directories(check PRIVATE \""+filepath.ToSlash(src)+"\")\nnh_embed_resources(check \"${CMAKE_CURRENT_SOURCE_DIR}/data\")\n")
	run := func(name string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(name, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", name, err, out)
		}
		return out
	}
	run("cmake", "-S", project, "-B", build)
	for _, value := range []string{"first", "replacement"} {
		write("data", value)
		run("cmake", "--build", build)
		if got := string(run(filepath.Join(build, "check"))); got != value {
			t.Fatalf("resource: got %q, want %q", got, value)
		}
	}
}
