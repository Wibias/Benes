package credentials

import "errors"

var ErrStillReferenced = errors.New("credential is still referenced")

func SameRef(a, b Ref) bool {
	return a.ID != "" && a.ID == b.ID && a.Source == b.Source
}

func Referenced(ref Ref, live []Ref) bool {
	for _, item := range live {
		if SameRef(ref, item) {
			return true
		}
	}
	return false
}

func DeleteUnreferenced(store Store, ref Ref, live []Ref) error {
	if store == nil {
		return ErrCredentialUnavailable
	}
	if Referenced(ref, live) {
		return ErrStillReferenced
	}
	return store.Delete(ref)
}
