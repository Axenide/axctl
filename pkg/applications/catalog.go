package applications

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/godbus/dbus/v5"
)

type Action struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Exec string `json:"exec"`
}

type Entry struct {
	ID               string   `json:"id"`
	Path             string   `json:"path"`
	Name             string   `json:"name"`
	GenericName      string   `json:"genericName,omitempty"`
	Comment          string   `json:"comment,omitempty"`
	Icon             string   `json:"icon,omitempty"`
	Exec             string   `json:"exec,omitempty"`
	Command          []string `json:"command,omitempty"`
	Categories       []string `json:"categories,omitempty"`
	Keywords         []string `json:"keywords,omitempty"`
	MimeTypes        []string `json:"mimeTypes,omitempty"`
	Actions          []Action `json:"actions,omitempty"`
	WorkingDirectory string   `json:"workingDirectory,omitempty"`
	StartupWMClass   string   `json:"startupWMClass,omitempty"`
	TryExec          string   `json:"tryExec,omitempty"`
	DBusActivatable  bool     `json:"dbusActivatable,omitempty"`
	NoDisplay        bool     `json:"noDisplay,omitempty"`
	Hidden           bool     `json:"hidden,omitempty"`
	Terminal         bool     `json:"terminal,omitempty"`
	OnlyShowIn       []string `json:"onlyShowIn,omitempty"`
	NotShowIn        []string `json:"notShowIn,omitempty"`
}

type Catalog struct {
	mu      sync.RWMutex
	dirs    []string
	current []string
	entries map[string]Entry
	usage   *UsageStore
	locale  string
}

func NewCatalog() (*Catalog, error) {
	dirs := applicationDirs()
	usage, err := NewUsageStore()
	if err != nil {
		return nil, err
	}
	c := &Catalog{
		dirs:    dirs,
		entries: make(map[string]Entry),
		usage:   usage,
		locale:  localeName(),
	}
	if err := c.Refresh(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Catalog) Refresh() error {
	entries := make(map[string]Entry)
	seen := make(map[string]bool)
	for _, root := range c.dirs {
		err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, os.ErrNotExist) || errors.Is(walkErr, os.ErrPermission) {
					if item != nil && item.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				return walkErr
			}
			if item.IsDir() || !strings.HasSuffix(strings.ToLower(item.Name()), ".desktop") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			id := strings.ReplaceAll(filepath.ToSlash(rel), "/", "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			entry, err := parseEntry(path, id, c.locale)
			if err != nil {
				return nil
			}
			entries[id] = entry
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("scan applications in %s: %w", root, err)
		}
	}
	current := make([]string, 0, len(entries))
	for id, entry := range entries {
		if entry.Hidden || entry.NoDisplay || !entryAllowed(entry) || !tryExecAvailable(entry.TryExec) {
			continue
		}
		current = append(current, id)
	}
	sort.Slice(current, func(i, j int) bool {
		left, right := entries[current[i]], entries[current[j]]
		if left.Name == right.Name {
			return left.ID < right.ID
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	c.mu.Lock()
	c.entries = entries
	c.current = current
	c.mu.Unlock()
	return nil
}

func (c *Catalog) List() []Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]Entry, 0, len(c.current))
	for _, id := range c.current {
		result = append(result, c.entries[id])
	}
	return result
}

func (c *Catalog) Top(limit int) []SearchResult {
	entries := c.List()
	if limit <= 0 {
		limit = len(entries)
	}
	result := make([]SearchResult, 0, len(entries))
	for _, entry := range entries {
		usage := c.usage.Score(entry.ID)
		result = append(result, SearchResult{Entry: entry, UsageScore: usage, TotalScore: usage})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].UsageScore != result[j].UsageScore {
			return result[i].UsageScore > result[j].UsageScore
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	if limit < len(result) {
		result = result[:limit]
	}
	return result
}

func (c *Catalog) Get(id string) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[id]
	return entry, ok && !entry.Hidden
}

func (c *Catalog) Resolve(identity string) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	identity = strings.ToLower(strings.TrimSuffix(identity, ".desktop"))
	for _, entry := range c.entries {
		if entry.Hidden {
			continue
		}
		if strings.ToLower(strings.TrimSuffix(entry.ID, ".desktop")) == identity ||
			strings.ToLower(entry.StartupWMClass) == identity ||
			strings.ToLower(entry.Name) == identity ||
			(len(entry.Command) > 0 && strings.ToLower(filepath.Base(entry.Command[0])) == identity) {
			return entry, true
		}
	}
	return Entry{}, false
}

func (c *Catalog) Search(query string, limit int) []SearchResult {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return []SearchResult{}
	}
	if limit <= 0 {
		limit = 10
	}
	entries := c.List()
	results := make([]SearchResult, 0)
	for _, entry := range entries {
		score := scoreEntry(entry, query)
		if score == 0 {
			continue
		}
		usage := c.usage.Score(entry.ID)
		results = append(results, SearchResult{Entry: entry, Score: score, UsageScore: usage, TotalScore: score + usage})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].TotalScore != results[j].TotalScore {
			return results[i].TotalScore > results[j].TotalScore
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

type SearchResult struct {
	Entry
	Score      int `json:"score"`
	UsageScore int `json:"usageScore"`
	TotalScore int `json:"totalScore"`
}

func (c *Catalog) RecordUsage(id string) error {
	return c.usage.Record(id)
}

func (c *Catalog) Launch(id, actionID string, uris []string) error {
	entry, ok := c.Get(id)
	if !ok {
		entry, ok = c.Resolve(id)
	}
	if !ok {
		return fmt.Errorf("application %q not found", id)
	}
	execLine := entry.Exec
	if actionID != "" {
		found := false
		for _, action := range entry.Actions {
			if action.ID == actionID {
				execLine = action.Exec
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("application action %q not found", actionID)
		}
	}
	if entry.DBusActivatable {
		if err := activateDBus(entry, actionID, uris); err == nil {
			return c.RecordUsage(entry.ID)
		} else if execLine == "" {
			return err
		}
	}
	argv, err := expandExec(execLine, entry, uris)
	if err != nil {
		return err
	}
	if len(argv) == 0 {
		return fmt.Errorf("application %q has no executable command", id)
	}
	if entry.Terminal {
		argv, err = terminalCommand(argv)
		if err != nil {
			return err
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if entry.WorkingDirectory != "" {
		cmd.Dir = entry.WorkingDirectory
	}
	cmd.Env = unsetEnv(os.Environ(), "HL_INITIAL_WORKSPACE_TOKEN")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", id, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	return c.RecordUsage(entry.ID)
}

func (c *Catalog) StartWatching(ctx context.Context, onChange func([]Entry)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, root := range c.dirs {
		_ = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil || !item.IsDir() {
				return nil
			}
			return watcher.Add(path)
		})
		if _, statErr := os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
			if parent := filepath.Dir(root); parent != root {
				_ = watcher.Add(parent)
			}
		}
	}
	go func() {
		defer watcher.Close()
		var debounce *time.Timer
		var debounceC <-chan time.Time
		refresh := func() {
			if err := c.Refresh(); err == nil && onChange != nil {
				onChange(c.List())
			}
		}
		for {
			select {
			case <-ctx.Done():
				if debounce != nil {
					debounce.Stop()
				}
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create != 0 {
					if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
						_ = filepath.WalkDir(event.Name, func(path string, item fs.DirEntry, walkErr error) error {
							if walkErr == nil && item.IsDir() {
								_ = watcher.Add(path)
							}
							return nil
						})
					}
				}
				if debounce == nil {
					debounce = time.NewTimer(150 * time.Millisecond)
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(150 * time.Millisecond)
				}
				debounceC = debounce.C
			case <-debounceC:
				debounceC = nil
				refresh()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "[applications] watcher: %v\n", err)
				}
			}
		}
	}()
	return nil
}

func parseEntry(path, id, locale string) (Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer file.Close()
	sections := map[string]map[string]string{}
	section := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if sections[section] == nil {
				sections[section] = make(map[string]string)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && section != "" {
			sections[section][strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return Entry{}, err
	}
	main := sections["Desktop Entry"]
	if main["Type"] != "Application" {
		return Entry{}, fmt.Errorf("not an application entry")
	}
	entry := Entry{
		ID:               id,
		Path:             path,
		Name:             localized(main, "Name", locale),
		GenericName:      localized(main, "GenericName", locale),
		Comment:          localized(main, "Comment", locale),
		Icon:             unescape(main["Icon"]),
		Exec:             unescape(main["Exec"]),
		Categories:       parseStringList(main["Categories"]),
		Keywords:         parseStringList(localized(main, "Keywords", locale)),
		MimeTypes:        parseStringList(main["MimeType"]),
		WorkingDirectory: unescape(main["Path"]),
		StartupWMClass:   unescape(main["StartupWMClass"]),
		TryExec:          unescape(main["TryExec"]),
		DBusActivatable:  parseBool(main["DBusActivatable"]),
		NoDisplay:        parseBool(main["NoDisplay"]),
		Hidden:           parseBool(main["Hidden"]),
		Terminal:         parseBool(main["Terminal"]),
		OnlyShowIn:       parseStringList(main["OnlyShowIn"]),
		NotShowIn:        parseStringList(main["NotShowIn"]),
	}
	entry.Command, _ = splitExec(entry.Exec)
	for _, actionID := range parseStringList(main["Actions"]) {
		actionGroup := sections["Desktop Action "+actionID]
		if actionGroup == nil {
			continue
		}
		entry.Actions = append(entry.Actions, Action{
			ID:   actionID,
			Name: localized(actionGroup, "Name", locale),
			Exec: unescape(actionGroup["Exec"]),
		})
	}
	return entry, nil
}

func applicationDirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	result := make([]string, 0)
	seen := make(map[string]bool)
	add := func(base string) {
		if base == "" {
			return
		}
		path, err := filepath.Abs(filepath.Join(base, "applications"))
		if err != nil {
			return
		}
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	add(dataHome)
	for _, base := range strings.Split(dataDirs, ":") {
		add(strings.TrimSpace(base))
	}
	return result
}

func localeName() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(key); value != "" {
			value = strings.SplitN(value, ".", 2)[0]
			value = strings.SplitN(value, "@", 2)[0]
			return value
		}
	}
	return "C"
}

func localized(values map[string]string, key, locale string) string {
	if locale != "" && locale != "C" && locale != "POSIX" {
		parts := strings.Split(locale, "_")
		for end := len(parts); end > 0; end-- {
			candidate := key + "[" + strings.Join(parts[:end], "_") + "]"
			if value, ok := values[candidate]; ok {
				return unescape(value)
			}
		}
		if value, ok := values[key+"["+locale+"]"]; ok {
			return unescape(value)
		}
	}
	return unescape(values[key])
}

func parseStringList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	result := make([]string, 0)
	for _, item := range strings.Split(value, ";") {
		if item = unescape(strings.TrimSpace(item)); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func unescape(value string) string {
	replacer := strings.NewReplacer(`\s`, " ", `\n`, "\n", `\t`, "\t", `\r`, "\r", `\\`, `\`)
	return replacer.Replace(value)
}

func parseBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func entryAllowed(entry Entry) bool {
	current := make(map[string]bool)
	for _, desktop := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if desktop = strings.TrimSpace(desktop); desktop != "" {
			current[desktop] = true
		}
	}
	if len(entry.OnlyShowIn) > 0 {
		allowed := false
		for _, desktop := range entry.OnlyShowIn {
			allowed = allowed || current[desktop]
		}
		if !allowed {
			return false
		}
	}
	for _, desktop := range entry.NotShowIn {
		if current[desktop] {
			return false
		}
	}
	return true
}

func tryExecAvailable(command string) bool {
	if command == "" {
		return true
	}
	if strings.ContainsRune(command, filepath.Separator) {
		info, err := os.Stat(command)
		return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func scoreEntry(entry Entry, query string) int {
	name := strings.ToLower(entry.Name)
	command := strings.ToLower(strings.Join(entry.Command, " "))
	executable := ""
	if len(entry.Command) > 0 {
		executable = strings.ToLower(filepath.Base(entry.Command[0]))
	}
	comment := strings.ToLower(entry.Comment)
	generic := strings.ToLower(entry.GenericName)
	score := 0
	if name == query {
		score += 100
	} else if strings.HasPrefix(name, query) {
		score += 80
	} else if strings.Contains(name, query) {
		score += 60
	}
	if strings.Contains(command, query) {
		score += 40
	}
	if strings.Contains(executable, query) {
		score += 50
	}
	if strings.Contains(comment, query) {
		score += 30
	}
	if strings.Contains(generic, query) {
		score += 25
	}
	for _, keyword := range entry.Keywords {
		if strings.Contains(strings.ToLower(keyword), query) {
			score += 20
			break
		}
	}
	return score
}

func splitExec(value string) ([]string, error) {
	var args []string
	var token strings.Builder
	quoted := false
	escaped := false
	started := false
	for _, r := range value {
		if escaped {
			token.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' {
			escaped = true
			started = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			started = true
			continue
		}
		if (r == ' ' || r == '\t') && !quoted {
			if started {
				args = append(args, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteRune(r)
		started = true
	}
	if escaped || quoted {
		return nil, fmt.Errorf("invalid desktop Exec value: unmatched quote or escape")
	}
	if started {
		args = append(args, token.String())
	}
	return args, nil
}

func expandExec(value string, entry Entry, uris []string) ([]string, error) {
	args, err := splitExec(value)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(args)+2)
	for _, arg := range args {
		if arg == "%i" {
			if entry.Icon != "" {
				result = append(result, "--icon", entry.Icon)
			}
			continue
		}
		if arg == "%f" || arg == "%u" {
			if len(uris) > 0 {
				result = append(result, uris[0])
			}
			continue
		}
		if arg == "%F" || arg == "%U" {
			result = append(result, uris...)
			continue
		}
		var expanded strings.Builder
		for i := 0; i < len(arg); i++ {
			if arg[i] != '%' || i+1 >= len(arg) {
				expanded.WriteByte(arg[i])
				continue
			}
			i++
			switch arg[i] {
			case 'c':
				expanded.WriteString(entry.Name)
			case 'k':
				expanded.WriteString(entry.Path)
			case '%':
				expanded.WriteByte('%')
			case 'f', 'F', 'u', 'U', 'i':
			default:
				return nil, fmt.Errorf("unsupported desktop Exec field code %%%c", arg[i])
			}
		}
		if expanded.Len() > 0 {
			result = append(result, expanded.String())
		}
	}
	return result, nil
}

func terminalCommand(command []string) ([]string, error) {
	terminal := strings.TrimSpace(os.Getenv("TERMINAL"))
	if terminal == "" {
		return nil, fmt.Errorf("application requires a terminal but TERMINAL is not set")
	}
	args, err := splitExec(terminal)
	if err != nil || len(args) == 0 {
		return nil, fmt.Errorf("invalid TERMINAL value")
	}
	args = append(args, "-e")
	return append(args, command...), nil
}

func activateDBus(entry Entry, actionID string, uris []string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	appID := strings.TrimSuffix(entry.ID, ".desktop")
	object := strings.ReplaceAll(strings.ReplaceAll(appID, ".", "/"), "-", "_")
	path := dbus.ObjectPath("/" + object)
	if !path.IsValid() {
		return fmt.Errorf("invalid D-Bus application object path for %s", entry.ID)
	}
	busObject := conn.Object(appID, path)
	platformData := map[string]dbus.Variant{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var method string
	var args []any
	switch {
	case actionID != "":
		method = "org.freedesktop.Application.ActivateAction"
		args = []any{actionID, []dbus.Variant{}, platformData}
	case len(uris) > 0:
		method = "org.freedesktop.Application.Open"
		args = []any{uris, platformData}
	default:
		method = "org.freedesktop.Application.Activate"
		args = []any{platformData}
	}
	return busObject.CallWithContext(ctx, method, 0, args...).Err
}

func unsetEnv(environment []string, key string) []string {
	result := make([]string, 0, len(environment))
	prefix := key + "="
	for _, value := range environment {
		if !strings.HasPrefix(value, prefix) {
			result = append(result, value)
		}
	}
	return result
}
