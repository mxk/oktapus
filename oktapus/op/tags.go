package op

import (
	"fmt"
	"sort"
	"strings"
)

// specialTags defines tags that require special handling.
var specialTags = map[string]struct{}{"err": {}, "owner": {}}

// tagChars determines which characters are allowed in tag names.
var tagChars [256]bool

func init() {
	for c := '0'; c <= '9'; c++ {
		tagChars[c] = true
	}
	for c := 'A'; c <= 'Z'; c++ {
		tagChars[c] = true
		tagChars[c|32] = true
	}
	for _, c := range []byte("-._") {
		tagChars[c] = true
	}
}

// Tags is a collection of keywords associated with an account. All methods
// assume that tags are sorted, each tag is unique, and no tag is negated.
type Tags []string

// ParseTags splits s into two disjoint sets of non-negated and negated tags.
func ParseTags(s string) (set, clr Tags, err error) {
	if s == "" {
		return
	}
	tags := strings.Split(s, ",")
	m := make(map[string]bool, len(tags))
	for _, t := range tags {
		name, neg, err := parseTag(t, true)
		if err != nil {
			return nil, nil, err
		}
		m[name] = neg
	}
	i, j := 0, len(tags)
	for name, neg := range m {
		if neg {
			j--
			tags[j] = name
		} else {
			tags[i] = name
			i++
		}
	}
	norm := func(t Tags) Tags {
		if len(t) == 0 {
			return nil
		}
		return t.Sort()
	}
	return norm(tags[:i:j]), norm(tags[j:]), nil
}

// Sort sorts tags in place and returns the original slice.
func (t Tags) Sort() Tags {
	// TODO: Natural sort?
	sort.Strings(t)
	return t
}

// String implements fmt.Stringer.
func (t Tags) String() string {
	return strings.Join(t, ",")
}

// Diff returns tags that are set and/or cleared in t relative to u. Calling
// u.Apply(set, clr) would make u == t.
func (t Tags) Diff(u Tags) (set, clr Tags) {
	for len(t) > 0 && len(u) > 0 {
		if s, c := t[0], u[0]; s == c {
			t, u = t[1:], u[1:]
		} else if s < c {
			set, t = append(set, s), t[1:]
		} else {
			clr, u = append(clr, c), u[1:]
		}
	}
	set = append(set, t...)
	clr = append(clr, u...)
	return
}

// Apply updates t by adding tags in set and removing those in clr. Setting tags
// takes priority over clearing them if the sets are not disjoint.
func (t *Tags) Apply(set, clr Tags) {
	if len(set) == 0 && len(clr) == 0 {
		return
	}
	m := make(map[string]struct{}, len(*t)+len(set))
	for _, name := range *t {
		m[name] = struct{}{}
	}
	for _, name := range clr {
		delete(m, name)
	}
	for _, name := range set {
		m[name] = struct{}{}
	}
	u := (*t)[:0]
	if cap(u) < len(m) {
		u = make(Tags, 0, len(m))
	}
	for name := range m {
		u = append(u, name)
	}
	*t = u.Sort()
}

// alias returns true if t and u share the same backing array.
func (t Tags) alias(u Tags) bool {
	// Taken from math/big/nat.go, doesn't work if capacity is changed
	return cap(t) > 0 && cap(u) > 0 &&
		&t[0:cap(t)][cap(t)-1] == &u[0:cap(u)][cap(u)-1]
}

// parseTag returns the name and negation state of tag t. An error is returned
// if t is not a valid tag.
func parseTag(t string, negOK bool) (name string, neg bool, err error) {
	name, val, neg := parseSpec(t)
	if len(name) == 0 || val != "" || (neg && !negOK) || isSpecial(name) {
		goto invalid
	}
	for i := len(name) - 1; i > 0; i-- {
		if !tagChars[name[i]] {
			goto invalid
		}
	}
	if c := name[0] | 32; 'a' <= c && c <= 'z' {
		return name, neg, nil
	}
invalid:
	return "", false, fmt.Errorf("invalid tag %q", t)
}

// isSpecial returns true if tag is special. The tag must not be negated.
func isSpecial(tag string) bool {
	_, ok := specialTags[tag]
	return ok
}
