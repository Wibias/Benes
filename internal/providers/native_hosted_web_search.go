package providers

type NativeHostedWebSearch interface {
	SupportsNativeHostedWebSearch(modelID string) bool
}

func SupportsNativeHostedWebSearch(provider Responses, modelID string) bool {
	if provider == nil {
		return false
	}
	native, ok := provider.(NativeHostedWebSearch)
	return ok && native.SupportsNativeHostedWebSearch(modelID)
}
