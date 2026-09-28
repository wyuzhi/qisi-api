package common

import (
	"fmt"
	"os"
	"regexp"
)

// PublicBasePath isolates this console from other applications on the same site.
var PublicBasePath string

func InitPublicBasePath() error {
	value := os.Getenv("PUBLIC_BASE_PATH")
	if value != "" && !regexp.MustCompile(`^/[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)*$`).MatchString(value) {
		return fmt.Errorf("PUBLIC_BASE_PATH must be empty or a path of simple segments")
	}
	PublicBasePath = value
	return nil
}
