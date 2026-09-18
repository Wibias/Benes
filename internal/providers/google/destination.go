package google

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	AIStudioAPI = "https://generativelanguage.googleapis.com"
	VertexAPI   = "https://aiplatform.googleapis.com"
)

var (
	ErrInvalidDestination = fmt.Errorf("Google destination is not a validated HTTPS origin")
	ErrInvalidLocation    = fmt.Errorf("Vertex AI location must be a single lowercase Google Cloud location label")
	ErrMissingProject     = fmt.Errorf("Vertex AI requires a project id")
	locationLabel         = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Endpoint struct {
	Kind     Kind
	BaseURL  string
	Project  string
	Location string
}

func ResolveAIStudioDestination(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AIStudioAPI, nil
	}
	return validateHTTPSHost(raw, "generativelanguage.googleapis.com")
}

func ResolveVertexDestination(location string) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		location = "global"
	}
	if !locationLabel.MatchString(location) {
		return "", ErrInvalidLocation
	}
	if location == "global" {
		return VertexAPI, nil
	}
	return "https://" + location + "-aiplatform.googleapis.com", nil
}

func GenerateURL(ep Endpoint, identity Identity, stream bool) (string, error) {
	method := "generateContent"
	if stream {
		method = "streamGenerateContent"
	}
	switch ep.Kind {
	case KindAIStudio:
		base, err := ResolveAIStudioDestination(ep.BaseURL)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(base, "/") + "/v1beta/models/" + identity.WireID + ":" + method, nil
	case KindVertex:
		if strings.TrimSpace(ep.Project) == "" {
			return "", ErrMissingProject
		}
		if ep.Project == "api-key" {
			return VertexAPI + "/v1/publishers/google/models/" + identity.WireID + ":" + method, nil
		}
		if err := ValidateLocation(ep.Location); err != nil {
			return "", err
		}
		loc := strings.TrimSpace(ep.Location)
		if loc == "" {
			return "", fmt.Errorf("Vertex AI requires a location")
		}
		host, err := ResolveVertexDestination(loc)
		if err != nil {
			return "", err
		}
		return host + "/v1/projects/" + ep.Project + "/locations/" + loc + "/publishers/google/models/" + identity.WireID + ":" + method, nil
	default:
		return "", fmt.Errorf("Google kind %q is unsupported", ep.Kind)
	}
}

func ValidateLocation(location string) error {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil
	}
	if !locationLabel.MatchString(location) {
		return ErrInvalidLocation
	}
	return nil
}

func validateHTTPSHost(raw, allowed string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalidDestination
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", ErrInvalidDestination
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", ErrInvalidDestination
	}
	if !strings.EqualFold(parsed.Hostname(), allowed) {
		return "", ErrInvalidDestination
	}
	return "https://" + strings.ToLower(parsed.Hostname()), nil
}
