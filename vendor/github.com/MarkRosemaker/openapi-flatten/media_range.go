package flatten

import (
	"mime"

	"github.com/MarkRosemaker/openapi"
)

// nameMediaRange returns a human-readable name for the media range.
func nameMediaRange(mr openapi.MediaRange) string {
	switch mt, _, _ := mime.ParseMediaType(string(mr)); mt {
	case openapi.MediaRangeJSON:
		return "JSON"
	case openapi.MediaRangeHTML:
		return "HTML"
	default:
		return "Unknown"
	}
}
