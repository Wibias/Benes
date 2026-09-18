package credentials

import "errors"

type Status struct {
	ID        string `json:"id"`
	Source    Source `json:"source"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

func StatusOf(store Store, ref Ref) Status {
	st := Status{ID: ref.ID, Source: ref.Source}
	if store == nil {
		st.Error = ErrCredentialUnavailable.Error()
		return st
	}
	secret, err := store.Get(ref)
	if err != nil {
		st.Error = publicStatusError(err)
		return st
	}
	if len(secret) == 0 {
		st.Error = ErrCredentialUnavailable.Error()
		return st
	}
	st.Available = true
	return st
}

func publicStatusError(err error) string {
	switch {
	case errors.Is(err, ErrSecureStoreUnavailable):
		return ErrSecureStoreUnavailable.Error()
	case errors.Is(err, ErrPlaintextDisabled):
		return ErrPlaintextDisabled.Error()
	default:
		return ErrCredentialUnavailable.Error()
	}
}
