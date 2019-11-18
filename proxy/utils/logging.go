package utils

// CollectHeadersToLog returns an ordered slice of slices of headers to be
// logged and returns a unique, ordered slice of headers to be logged
func CollectHeadersToLog(headerGroups ...[]string) []string {
	// We want the headers to be ordered, so we need a slice and a map
	var (
		collectedHeaders = make([]string, 0)
		seenHeaders      = make(map[string]bool)
	)

	for _, headerGroup := range headerGroups {
		for _, header := range headerGroup {

			if _, seen := seenHeaders[header]; !seen {

				seenHeaders[header] = true
				collectedHeaders = append(collectedHeaders, header)

			}
		}

	}

	return collectedHeaders
}
