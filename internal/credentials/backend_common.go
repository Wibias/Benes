package credentials

type unavailableBackend struct{}

func (unavailableBackend) Available() error { return ErrSecureStoreUnavailable }
func (unavailableBackend) Put(string, []byte) error {
	return ErrSecureStoreUnavailable
}
func (unavailableBackend) Get(string) ([]byte, error) {
	return nil, ErrSecureStoreUnavailable
}
func (unavailableBackend) Delete(string) error { return ErrSecureStoreUnavailable }
