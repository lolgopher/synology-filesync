package app

import (
	"path"
	"strings"
)

func isExcludedSynologyPath(candidate string, excludePaths []string) bool {
	cleanCandidate := path.Clean(candidate)

	for _, excludePath := range excludePaths {
		cleanExcludePath := path.Clean(excludePath)
		if cleanExcludePath == "/" {
			return strings.HasPrefix(cleanCandidate, "/")
		}
		if cleanCandidate == cleanExcludePath || strings.HasPrefix(cleanCandidate, cleanExcludePath+"/") {
			return true
		}
	}

	return false
}
