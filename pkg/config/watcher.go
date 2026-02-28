package config

import (
	"fmt"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ConfigWatcher watches config files for changes and triggers reloads.
type ConfigWatcher struct {
	watcher    *fsnotify.Watcher
	configPath string
	callback   func(*TOMLConfig)
	watched    map[string]bool
	mu         sync.Mutex
	done       chan struct{}
}

// NewConfigWatcher creates a new config file watcher.
func NewConfigWatcher() (*ConfigWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}
	return &ConfigWatcher{
		watcher: w,
		watched: make(map[string]bool),
		done:    make(chan struct{}),
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

func (cw *ConfigWatcher) updateWatchedFiles() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	paths := ResolveIncludePaths(cw.configPath)
	newWatched := make(map[string]bool)

	for _, p := range paths {
		newWatched[p] = true
		if !cw.watched[p] {
			if err := cw.watcher.Add(p); err != nil {
				// File might not exist yet — not an error
				fmt.Printf("[axctl-config] Warning: cannot watch %s: %v\n", p, err)
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
