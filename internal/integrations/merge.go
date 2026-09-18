package integrations

import (
	"encoding/json"
	"sort"
)

func cloneValue(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return value
	}
	return out
}

func isPlainMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func setPath(doc any, path []string, value any) any {
	root, ok := isPlainMap(doc)
	if !ok {
		root = map[string]any{}
	} else {
		cloned, _ := cloneValue(root).(map[string]any)
		root = cloned
	}
	cursor := root
	for _, key := range path[:len(path)-1] {
		next, ok := cursor[key]
		child, isMap := isPlainMap(next)
		if !ok || !isMap {
			child = map[string]any{}
			cursor[key] = child
		}
		cursor = child
	}
	cursor[path[len(path)-1]] = cloneValue(value)
	return root
}

func deletePath(doc any, path []string, created map[string]struct{}) (any, bool) {
	root, ok := isPlainMap(doc)
	if !ok || len(path) == 0 {
		return doc, false
	}
	cloned, _ := cloneValue(root).(map[string]any)
	chain := []map[string]any{cloned}
	cursor := cloned
	for _, key := range path[:len(path)-1] {
		next, ok := cursor[key]
		child, isMap := isPlainMap(next)
		if !ok || !isMap {
			return cloned, false
		}
		cursor = child
		chain = append(chain, cursor)
	}
	leaf := path[len(path)-1]
	if _, exists := cursor[leaf]; !exists {
		return cloned, false
	}
	delete(cursor, leaf)
	for i := len(chain) - 1; i >= 1; i-- {
		if len(chain[i]) > 0 {
			break
		}
		containerPath := joinPath(path[:i])
		if _, ours := created[containerPath]; !ours {
			break
		}
		delete(chain[i-1], path[i-1])
	}
	return cloned, true
}

func joinPath(path []string) string {
	out := ""
	for i, key := range path {
		if i > 0 {
			out += "\x00"
		}
		out += key
	}
	return out
}

func mergeContribution(doc any, fragments []Fragment) any {
	next := doc
	for _, fragment := range fragments {
		next = setPath(next, fragment.Path, fragment.Value)
	}
	return next
}

func removeFragments(doc any, paths [][]string, created []string) (any, bool) {
	set := map[string]struct{}{}
	for _, item := range created {
		set[item] = struct{}{}
	}
	next := doc
	removed := false
	for _, path := range paths {
		var ok bool
		next, ok = deletePath(next, path, set)
		removed = removed || ok
	}
	return next, removed
}

func createdContainerPaths(doc any, fragments []Fragment) []string {
	created := map[string]struct{}{}
	for _, fragment := range fragments {
		var cursor any = doc
		for depth := 0; depth < len(fragment.Path)-1; depth++ {
			key := fragment.Path[depth]
			m, ok := isPlainMap(cursor)
			if !ok {
				created[joinPath(fragment.Path[:depth+1])] = struct{}{}
				cursor = nil
				continue
			}
			next, exists := m[key]
			if !exists {
				created[joinPath(fragment.Path[:depth+1])] = struct{}{}
				cursor = nil
				continue
			}
			cursor = next
		}
	}
	out := make([]string, 0, len(created))
	for path := range created {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func readPath(doc any, path []string) (any, bool) {
	cursor := doc
	for _, key := range path {
		m, ok := isPlainMap(cursor)
		if !ok {
			return nil, false
		}
		next, exists := m[key]
		if !exists {
			return nil, false
		}
		cursor = next
	}
	return cursor, true
}

func blockedContainerPath(doc any, fragments []Fragment) []string {
	for _, fragment := range fragments {
		cursor := doc
		for depth := 0; depth < len(fragment.Path)-1; depth++ {
			if cursor == nil {
				break
			}
			m, ok := isPlainMap(cursor)
			if !ok {
				return fragment.Path[:depth]
			}
			next, exists := m[fragment.Path[depth]]
			if !exists {
				break
			}
			if next == nil {
				return fragment.Path[:depth+1]
			}
			if _, isMap := isPlainMap(next); !isMap {
				return fragment.Path[:depth+1]
			}
			cursor = next
		}
	}
	return nil
}
