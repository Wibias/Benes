package grok

import "os"

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

func readFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
