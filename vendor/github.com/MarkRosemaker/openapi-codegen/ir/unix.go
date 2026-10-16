package ir

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// unixMode is how a document's values of one Go type that are integers of unix time are written and read.
type unixMode struct {
	// all is set when every value of the type is one: a marshaler for the type writes them all.
	all bool
	// members are, where others are strings, the names of the object members holding the integers.
	members []string
}

// markUnix decides, for time.Time and civil.Date, how the values that are integers of unix time are written and read:
// all of them by one marshaler, or, among strings, by the names of the members holding them.
func markUnix(schemas []Schema) (times, dates unixMode, err error) {
	for goType, mode := range map[string]*unixMode{"time.Time": &times, "civil.Date": &dates} {
		unix, other := map[string]bool{}, map[string]bool{}

		for _, s := range schemas {
			for _, f := range s.Fields {
				switch {
				case f.Embedded || !containsType(f.Type, goType):
				case f.IsDateTimeOrInt:
					// either form will do: it is written as the others are, or as a string where they differ
				case f.IsUnix:
					if strings.Contains(f.Type, "map[") {
						return unixMode{}, unixMode{}, fmt.Errorf("%s.%s: a map of integer %s is not supported", s.Name, f.Name, goType)
					}

					unix[f.JSONName] = true
				default:
					other[f.JSONName] = true
				}
			}
		}

		switch {
		case len(unix) == 0:
		case len(other) == 0:
			mode.all = true
		default:
			for name := range unix {
				switch {
				case other[name]:
					return unixMode{}, unixMode{}, fmt.Errorf("member %q holds an integer %s in one schema and a string in another, which its name cannot tell apart", name, goType)
				case strings.Trim(name, "0123456789") == "":
					return unixMode{}, unixMode{}, fmt.Errorf("member %q: an integer %s among strings needs a name that is no number", name, goType)
				}
			}

			mode.members = slices.Sorted(maps.Keys(unix))
		}
	}

	return times, dates, nil
}
