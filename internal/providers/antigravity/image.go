package antigravity

import "errors"

var ErrAmbiguousPaidImage = errors.New("Cloud Code Assist image generation failed after dispatch and is not retryable")

func ImageTransportFailure(dispatched bool) error {
	if dispatched {
		return ErrAmbiguousPaidImage
	}
	return nil
}
