package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ConfigWatcher watches config files for changes and triggers reloads.
type ConfigWatcher struct {
	watcher     *fsnotify.Watcher
	configPath  string
	callback    func(*TOMLConfig)
	watched     map[string]bool
	watchedDirs map[string]bool
	mu          sync.Mutex
	done        chan struct{}
}

// NewConfigWatcher creates a new config file watcher.
func NewConfigWatcher() (*ConfigWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}
	return &ConfigWatcher{
		watcher:     w,
		watched:     make(map[string]bool),
		watchedDirs: make(map[string]bool),
		done:        make(chan struct{}),
	}, nil
}

// Start begins watching the config file at path and calls callback on changes.
func (cw *ConfigWatcher) Start(path string, callback func(*TOMLConfig)) {
	cw.configPath = path
	cw.callback = callback

	// Watch the main config and all includes
	cw.updateWatchedFiles()

	go cw.loop()
}

// Stop stops the config watcher and releases resources.
func (cw *ConfigWatcher) Stop() {
	close(cw.done)
	cw.watcher.Close()
}

func (cw *ConfigWatcher) loop() {
	var debounceTimer *time.Timer

	for {
		select {
		case <-cw.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			// React to writes, creates, and renames (atomic saves use rename)
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			// Parent directories are watched too (to catch config files that
			// don't exist yet), so filter out events for unrelated files
			// living in the same directory.
			if !cw.isConfigPath(event.Name) {
				continue
			}

			// Debounce: reset timer on each event
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
				cw.reload()
			})

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[axctl-config] Watcher error: %v\n", err)
		}
	}
}

func (cw *ConfigWatcher) reload() {
	cfg, err := LoadConfig(cw.configPath)
	if err != nil {
		fmt.Printf("[axctl-config] Error reloading config: %v\n", err)
		return
	}

	fmt.Printf("[axctl-config] Config reloaded from %s\n", cw.configPath)

	// Update watched files in case includes changed
	cw.updateWatchedFiles()

	if cw.callback != nil {
		cw.callback(cfg)
	}
}

// isConfigPath reports whether an fsnotify event path refers to one of the
// watched config files (as opposed to an unrelated file in a watched dir).
func (cw *ConfigWatcher) isConfigPath(name string) bool {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.watched[filepath.Clean(name)]
}

func (cw *ConfigWatcher) updateWatchedFiles() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	paths := ResolveIncludePaths(cw.configPath)
	newWatched := make(map[string]bool)

	for _, p := range paths {
		p = filepath.Clean(p)
		newWatched[p] = true
		if !cw.watched[p] {
			if err := cw.watcher.Add(p); err != nil {
				// File might not exist yet — the parent-directory watch
				// below picks up its creation.
				fmt.Printf("[axctl-config] Note: cannot watch %s yet: %v\n", p, err)
			}
		}

		// Also watch the parent directory: fsnotify cannot watch a path that
		// does not exist yet, and file-level watches break when editors save
		// via rename. The event loop filters events back down to the config
		// paths, so unrelated files in the directory don't trigger reloads.
		dir := filepath.Dir(p)
		if !cw.watchedDirs[dir] {
			if err := cw.watcher.Add(dir); err != nil {
				fmt.Printf("[axctl-config] Warning: cannot watch dir %s: %v\n", dir, err)
			} else {
				cw.watchedDirs[dir] = true
			}
		}
	}

	// Remove watches for files no longer included
	for p := range cw.watched {
		if !newWatched[p] {
			cw.watcher.Remove(p)
		}
	}

	cw.watched = newWatched
}
