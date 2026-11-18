package applications

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseEntryLocalizedFieldsAndActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.desktop")
	contents := `[Desktop Entry]
Type=Application
Name=Demo
Name[es]=Demostración
GenericName=Viewer
Exec=demo --title "%c" %U
Icon=demo-icon
Keywords=photo;image;
Actions=NewWindow;
StartupWMClass=DemoWindow
DBusActivatable=true

[Desktop Action NewWindow]
Name=New Window
Exec=demo --new-window
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := parseEntry(path, "demo.desktop", "es_MX")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "Demostración" || entry.GenericName != "Viewer" || !entry.DBusActivatable {
		t.Fatalf("unexpected entry fields: %+v", entry)
	}
	if !reflect.DeepEqual(entry.Keywords, []string{"photo", "image"}) || len(entry.Actions) != 1 {
		t.Fatalf("unexpected keywords or actions: %+v", entry)
	}
	argv, err := expandExec(entry.Exec, entry, []string{"file:///tmp/photo.png"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"demo", "--title", "Demostración", "file:///tmp/photo.png"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("expandExec() = %#v, want %#v", argv, want)
	}
}

func TestBottlesDesktopExecKeepsSingleQuotedArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fl-studio.desktop")
	writeDesktopTestFile(t, path, "Name=FL Studio\nExec=bottles-cli run -p 'FL Studio' -b 'FL Studio 25' -- %u\n")
	entry, err := parseEntry(path, "fl-studio.desktop", "C")
	if err != nil {
		t.Fatal(err)
	}

	wantCommand := []string{"bottles-cli", "run", "-p", "FL Studio", "-b", "FL Studio 25", "--", "%u"}
	if !reflect.DeepEqual(entry.Command, wantCommand) {
		t.Fatalf("entry.Command = %#v, want %#v", entry.Command, wantCommand)
	}

	argv, err := expandExec(entry.Exec, entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantArgv := []string{"bottles-cli", "run", "-p", "FL Studio", "-b", "FL Studio 25", "--"}
	if !reflect.DeepEqual(argv, wantArgv) {
		t.Fatalf("expandExec() = %#v, want %#v", argv, wantArgv)
	}
}

func TestRefreshDesktopFilePrecedenceAndVisibility(t *testing.T) {
	user := t.TempDir()
	system := t.TempDir()
	writeDesktopTestFile(t, filepath.Join(system, "editor.desktop"), "Name=System Editor\nExec=editor\n")
	writeDesktopTestFile(t, filepath.Join(user, "editor.desktop"), "Name=User Editor\nExec=editor\n")
	writeDesktopTestFile(t, filepath.Join(user, "nested", "hidden.desktop"), "Name=Hidden\nExec=hidden\nHidden=true\n")
	usage := &UsageStore{path: filepath.Join(t.TempDir(), "usage.json"), values: make(map[string]Usage)}
	catalog := &Catalog{dirs: []string{user, system}, locale: "C", usage: usage}
	if err := catalog.Refresh(); err != nil {
		t.Fatal(err)
	}
	entries := catalog.List()
	if len(entries) != 1 || entries[0].Name != "User Editor" {
		t.Fatalf("List() = %#v", entries)
	}
	if _, ok := catalog.Get("nested-hidden.desktop"); ok {
		t.Fatal("hidden entry should not be returned")
	}
}

func TestScoreEntryMatchesSearchFields(t *testing.T) {
	entry := Entry{Name: "Document Viewer", Comment: "Read documents", GenericName: "Reader", Command: []string{"evince"}, Keywords: []string{"pdf"}}
	if scoreEntry(entry, "document") <= scoreEntry(entry, "read") {
		t.Fatal("name matches should rank above description matches")
	}
	if scoreEntry(entry, "evince") == 0 || scoreEntry(entry, "pdf") == 0 {
		t.Fatal("command and keyword fields should be searchable")
	}
}

func TestUsageStorePersistsLegacyShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	store := &UsageStore{path: path, values: make(map[string]Usage)}
	if err := store.Record("demo.desktop"); err != nil {
		t.Fatal(err)
	}
	reloaded := &UsageStore{path: path, values: make(map[string]Usage)}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot()["demo.desktop"]; got.Count != 1 || got.LastUsed == 0 {
		t.Fatalf("persisted usage = %+v", got)
	}
}

func writeDesktopTestFile(t *testing.T, path, fields string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "[Desktop Entry]\nType=Application\n" + fields
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
