//go:build !windows

package nativemain

func NewOSKeyProvider() KeyProvider { return unavailableKeyProvider{} }

type unavailableKeyProvider struct{}

func (unavailableKeyProvider) Get(string) (*Key, error) {
	return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store is unavailable; no plaintext fallback is permitted.", 503)
}

func (unavailableKeyProvider) Create(string) (*Key, error) {
	return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store is unavailable; no plaintext fallback is permitted.", 503)
}
