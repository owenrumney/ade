package settings

import "sync"

// writing serialises read-modify-write within one process. The launcher and
// a checkout's server are two, so a PUT from the other can still overwrite.
var writing sync.Mutex

// Update loads, changes and saves under that lock, so two edits in one
// process cannot each read the same file and write back half the result.
func Update(change func(*Settings) error) error {
	writing.Lock()
	defer writing.Unlock()

	cfg, err := Load()
	if err != nil {
		return err
	}
	if err := change(&cfg); err != nil {
		return err
	}
	return Save(cfg)
}
