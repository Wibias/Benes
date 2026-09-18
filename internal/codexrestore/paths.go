package codexrestore

import "path/filepath"

func configPath(home string) string {
	return filepath.Join(home, "config.toml")
}

func profilePath(home string) string {
	return filepath.Join(home, "benes.config.toml")
}

func journalPath(home string) string {
	return filepath.Join(home, "benes-journal.json")
}
