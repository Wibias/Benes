package contextprojection

type Registry struct {
	byRef map[string]Artifact
}

func NewRegistry() *Registry {
	return &Registry{byRef: make(map[string]Artifact)}
}

func (r *Registry) Register(identity Identity, messageIndex int, content string, utf8ByteLength, lineCount int, isError bool) Artifact {
	artifact := Artifact{
		Ref:            ArtifactRef(identity),
		Identity:       identity,
		MessageIndex:   messageIndex,
		Content:        content,
		UTF8ByteLength: utf8ByteLength,
		LineCount:      lineCount,
		IsError:        isError,
	}
	if r.byRef == nil {
		r.byRef = make(map[string]Artifact)
	}
	r.byRef[artifact.Ref] = artifact
	return artifact
}

func (r *Registry) Get(ref string) (Artifact, bool) {
	if r == nil || r.byRef == nil {
		return Artifact{}, false
	}
	artifact, ok := r.byRef[ref]
	return artifact, ok
}
