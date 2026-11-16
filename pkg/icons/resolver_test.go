package icons

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveThemeInheritanceAndScale(t *testing.T) {
	base := t.TempDir()
	writeIconTestFile(t, filepath.Join(base, "custom", "index.theme"), "[Icon Theme]\nDirectories=apps/48\nInherits=parent\n\n[apps/48]\nSize=48\nType=Fixed\nScale=1\n")
	writeIconTestFile(t, filepath.Join(base, "custom", "apps/48", "local.png"), "local")
	writeIconTestFile(t, filepath.Join(base, "parent", "index.theme"), "[Icon Theme]\nDirectories=apps/32\n\n[apps/32]\nSize=32\nType=Fixed\n")
	writeIconTestFile(t, filepath.Join(base, "parent", "apps/32", "inherited.png"), "inherited")
	resolver := &Resolver{
		themeName: "custom",
		dataDirs:  []string{base},
		cache:     make(map[string]string),
		themes:    make(map[string]theme),
	}

	if got := resolver.Resolve("local", 48, 1); got != filepath.Join(base, "custom", "apps/48", "local.png") {
		t.Fatalf("Resolve(local) = %q", got)
	}
	if got := resolver.Resolve("inherited", 32, 1); got != filepath.Join(base, "parent", "apps/32", "inherited.png") {
		t.Fatalf("Resolve(inherited) = %q", got)
	}
	if got := resolver.Resolve("local", 48, 2); got != "" {
		t.Fatalf("Resolve at a missing scale = %q", got)
	}
}

func TestResolveAbsoluteAndMissingIcons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.svg")
	writeIconTestFile(t, path, "svg")
	resolver := &Resolver{cache: make(map[string]string), themes: make(map[string]theme)}
	if got := resolver.Resolve(path, 32, 1); got != path {
		t.Fatalf("Resolve(absolute) = %q", got)
	}
	if got := resolver.Resolve(path+"-missing", 32, 1); got != "" {
		t.Fatalf("Resolve(missing absolute) = %q", got)
	}
}

func writeIconTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
