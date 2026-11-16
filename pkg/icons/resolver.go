package icons

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type directory struct {
	name      string
	size      int
	scale     int
	minSize   int
	maxSize   int
	threshold int
	typeName  string
}

type theme struct {
	name        string
	directories []directory
	inherits    []string
}

type Resolver struct {
	mu        sync.RWMutex
	themeName string
	dataDirs  []string
	cache     map[string]string
	themes    map[string]theme
}

func NewResolver() *Resolver {
	return &Resolver{
		themeName: selectedTheme(),
		dataDirs:  iconBaseDirs(),
		cache:     make(map[string]string),
		themes:    make(map[string]theme),
	}
}

func (r *Resolver) Theme() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.themeName
}

func (r *Resolver) Resolve(name string, size, scale int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if filepath.IsAbs(name) {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			return name
		}
		return ""
	}
	if size <= 0 {
		size = 48
	}
	if scale <= 0 {
		scale = 1
	}
	key := name + "\x00" + strconv.Itoa(size) + "\x00" + strconv.Itoa(scale)
	r.mu.RLock()
	if value, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		return value
	}
	themeName := r.themeName
	r.mu.RUnlock()

	path := r.find(name, size, scale, themeName, make(map[string]bool))
	if path == "" && themeName != "hicolor" {
		path = r.find(name, size, scale, "hicolor", make(map[string]bool))
	}
	if path == "" {
		path = r.findFallback(name)
	}

	r.mu.Lock()
	r.cache[key] = path
	r.mu.Unlock()
	return path
}

func (r *Resolver) SetTheme(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "hicolor"
	}
	r.mu.Lock()
	if r.themeName != name {
		r.themeName = name
		r.cache = make(map[string]string)
	}
	r.mu.Unlock()
}

func (r *Resolver) find(name string, size, scale int, themeName string, seen map[string]bool) string {
	if themeName == "" || seen[themeName] {
		return ""
	}
	seen[themeName] = true
	t := r.loadTheme(themeName)
	for _, base := range r.dataDirs {
		for _, dir := range t.directories {
			path := r.findInDirectory(base, themeName, dir, name, size, scale)
			if path != "" && directoryMatches(dir, size, scale) {
				return path
			}
		}
	}
	for _, base := range r.dataDirs {
		for _, dir := range t.directories {
			path := r.findInDirectory(base, themeName, dir, name, size, scale)
			if path != "" {
				return path
			}
		}
	}
	for _, parent := range t.inherits {
		if path := r.find(name, size, scale, parent, seen); path != "" {
			return path
		}
	}
	return ""
}

func (r *Resolver) loadTheme(name string) theme {
	r.mu.RLock()
	if value, ok := r.themes[name]; ok {
		r.mu.RUnlock()
		return value
	}
	r.mu.RUnlock()

	value := theme{name: name}
	for _, base := range r.dataDirs {
		data, err := os.ReadFile(filepath.Join(base, name, "index.theme"))
		if err != nil {
			continue
		}
		sections := parseINI(string(data))
		main := sections["Icon Theme"]
		value.inherits = splitCommaList(main["Inherits"])
		for _, sectionName := range splitCommaList(main["Directories"]) {
			section := sections[sectionName]
			if section == nil {
				continue
			}
			value.directories = append(value.directories, directory{
				name:      sectionName,
				size:      parseInt(section["Size"], 48),
				scale:     parseInt(section["Scale"], 1),
				minSize:   parseInt(section["MinSize"], parseInt(section["Size"], 48)),
				maxSize:   parseInt(section["MaxSize"], parseInt(section["Size"], 48)),
				threshold: parseInt(section["Threshold"], 2),
				typeName:  section["Type"],
			})
		}
		break
	}
	if len(value.inherits) == 0 && name != "hicolor" {
		value.inherits = []string{"hicolor"}
	}
	r.mu.Lock()
	r.themes[name] = value
	r.mu.Unlock()
	return value
}

func (r *Resolver) findInDirectory(base, themeName string, dir directory, name string, size, scale int) string {
	if dir.scale != scale {
		return ""
	}
	for _, ext := range []string{".png", ".svg", ".xpm"} {
		path := filepath.Join(base, themeName, dir.name, name+ext)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func (r *Resolver) findFallback(name string) string {
	for _, base := range r.dataDirs {
		for _, ext := range []string{".png", ".svg", ".xpm"} {
			path := filepath.Join(base, name+ext)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
	}
	return ""
}

func directoryMatches(dir directory, size, scale int) bool {
	target := size * scale
	base := dir.size * dir.scale
	switch strings.ToLower(dir.typeName) {
	case "fixed":
		return target == base
	case "scalable":
		return target >= dir.minSize*dir.scale && target <= dir.maxSize*dir.scale
	default:
		threshold := dir.threshold * dir.scale
		return target >= base-threshold && target <= base+threshold
	}
}

func iconBaseDirs() []string {
	var result []string
	seen := make(map[string]bool)
	add := func(path string) {
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	home, _ := os.UserHomeDir()
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		add(filepath.Join(xdg, "icons"))
	} else if home != "" {
		add(filepath.Join(home, ".local", "share", "icons"))
	}
	add(filepath.Join(home, ".icons"))
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, base := range filepath.SplitList(strings.ReplaceAll(dataDirs, ":", string(os.PathListSeparator))) {
		add(filepath.Join(base, "icons"))
	}
	add("/usr/share/pixmaps")
	return result
}

func selectedTheme() string {
	if value := strings.TrimSpace(os.Getenv("QS_ICON_THEME")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("ICON_THEME")); value != "" {
		return value
	}
	if output, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "icon-theme").Output(); err == nil {
		value := strings.Trim(strings.TrimSpace(string(output)), "'\"")
		if value != "" {
			return value
		}
	}
	return "hicolor"
}

func parseINI(data string) map[string]map[string]string {
	sections := make(map[string]map[string]string)
	section := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
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
	return sections
}

func splitCommaList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func parseInt(value string, fallback int) int {
	if result, err := strconv.Atoi(value); err == nil {
		return result
	}
	return fallback
}
