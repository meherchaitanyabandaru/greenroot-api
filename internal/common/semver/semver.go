// Package semver parses and compares MAJOR.MINOR.PATCH app versions.
// A build suffix ("1.4.2+37") and pre-release tag ("1.4.2-beta") are
// ignored for ordering: store releases are what the update policy compares.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed MAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// Parse reads "1", "1.4" or "1.4.2" (missing parts are 0).
func Parse(s string) (Version, error) {
	core := strings.TrimSpace(s)
	core = strings.TrimPrefix(core, "v")
	if i := strings.IndexAny(core, "+-"); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 3 {
		return Version{}, fmt.Errorf("%q is not a version like 1.4.2", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, fmt.Errorf("%q is not a version like 1.4.2", s)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, nil
}

// Less reports whether v is older than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }
