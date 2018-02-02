package op

import (
	"sort"
	"strconv"
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
// assume that the list of tags is sorted and each tag is unique.
type Tags []string

// diff returns tags that are set and/or cleared in t relative to u. Calling
// u.apply(set, clr) would make u == t.
func (t Tags) diff(u Tags) (set, clr Tags) {
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

// apply updates t by adding tags in set and removing those in clr. Setting tags
// takes priority over clearing them if there is any overlap.
func (t *Tags) apply(set, clr Tags) {
	if len(set) == 0 && len(clr) == 0 {
		return
	}
	m := make(map[string]struct{}, len(*t)+len(set))
	for _, x := range *t {
		m[x] = struct{}{}
	}
	for _, x := range clr {
		delete(m, x)
	}
	for _, x := range set {
		m[x] = struct{}{}
	}
	u := (*t)[:0]
	for x := range m {
		u = append(u, x)
	}
	sort.Strings(u)
	*t = u
}

// parseTag splits a tag into its components. The general format is:
// "[!...]name[[!]=value]". If value is a boolean, it determines the initial
// negation state instead of being returned as a string.
func parseTag(tag string) (name, value string, neg bool) {
	if i := strings.IndexByte(tag, '='); i != -1 {
		tag, value = tag[:i], tag[i+1:]
		if t, err := strconv.ParseBool(value); err == nil {
			value, neg = "", !t
		}
		if i > 0 && tag[i-1] == '!' {
			tag, neg = tag[:i-1], !neg
		}
	}
	for len(tag) > 0 && tag[0] == '!' {
		tag, neg = tag[1:], !neg
	}
	name = tag
	return
}

// isTag returns true if s contains a valid tag name.
func isTag(s string, negOK bool) bool {
	name, val, neg := parseTag(s)
	if len(name) == 0 || val != "" || (neg && !negOK) || isSpecial(name) {
		return false
	}
	for i := len(name) - 1; i > 0; i-- {
		if !tagChars[name[i]] {
			return false
		}
	}
	c := name[0] | 32
	return 'a' <= c && c <= 'z'
}

// isSpecial returns true if tag is special. The tag must not be negated.
func isSpecial(tag string) bool {
	_, ok := specialTags[tag]
	return ok
}
