package mongo

import (
	"net/url"
	"strings"
)

func DatabaseNameFromURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "easychat"
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "" {
		return "easychat"
	}
	if idx := strings.Index(name, "?"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return "easychat"
	}
	return name
}
