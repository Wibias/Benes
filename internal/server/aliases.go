package server

import (
	"errors"

	"github.com/Wibias/Benes/internal/router"
)

func (h *handler) parseRoute(selector string) (router.Route, error) {
	return router.Resolve(selector, h.codexAccountNamespaces, h.aliases)
}

func ambiguousAliasMessage(err error) (string, bool) {
	var ambiguous *router.AmbiguousAliasError
	if !errors.As(err, &ambiguous) {
		return "", false
	}
	return ambiguous.Error(), true
}
