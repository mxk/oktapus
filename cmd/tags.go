package cmd

import (
	"fmt"
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
	for c := range []byte("-._") {
		tagChars[c] = true
	}
}

// accountSpec specifies how to filter accounts.
type accountSpec struct {
	spec    []string        // Original spec split by commas
	idx     map[string]uint // Map of non-special tag names to spec indices
	owner   map[string]bool // Map of owner names to match criteria
	tagMask uint64          // Tag matching mask
	ids     bool            // Filter by account IDs
	err     bool            // Include inaccessible accounts (ac.Error()!=nil)
}

// newAccountSpec parses the account spec string. User argument determines the
// meaning of "owner=me" specification.
func newAccountSpec(spec, user string) *accountSpec {
	s := new(accountSpec)
	if spec == "" {
		return s
	}
	s.spec = strings.Split(spec, ",")
	s.idx = make(map[string]uint, len(s.spec))
	for i, tag := range s.spec {
		if name, val, neg := parseTag(tag); isSpecial(name) {
			switch name {
			case "err":
				s.err = !neg
			case "owner":
				if s.owner == nil {
					s.owner = make(map[string]bool, 2)
				}
				if val == "me" {
					val = user
				}
				// owner[!]=x sets s.owner["x"] = !neg
				// [!]owner sets s.owner[""] = neg
				s.owner[val] = neg == (val == "")
			}
		} else if s.idx[name] = uint(i); !s.ids && isAWSAccountID(name) {
			s.ids = true
		} else if !neg {
			// tagMask is ignored if s.ids == true, in which case i > 63 is ok
			// as well.
			s.tagMask |= uint64(1) << uint(i)
		}
	}
	return s
}

// Filter removes accounts from all that do not match the spec.
func (s *accountSpec) Filter(all *[]*Account) error {
	var result []*Account
	var err error
	if s.ids || len(s.spec) > 64 {
		result, err = s.filterNamesOrIDs(*all)
	} else {
		// Assume that we're filtering by tags. If a matching account name is
		// found, we switch filters at that point. This eliminates the need to
		// make two passes through all accounts.
		result, err = s.filterTags(*all)
	}
	*all = result
	return err
}

// filterNamesOrIDs filters accounts by either names or IDs. All non-negated
// entries in s.idx must match an account. Error status is not considered.
func (s *accountSpec) filterNamesOrIDs(all []*Account) ([]*Account, error) {
	result := all[:0]
	if len(s.idx) == 0 {
		return result, nil
	}
	matched := make(map[string]struct{}, len(s.idx))
	for _, ac := range all {
		key := ac.Name
		if s.ids {
			key = ac.ID
		}
		if i, ok := s.idx[key]; ok {
			if _, _, neg := parseTag(s.spec[i]); !neg {
				result = append(result, ac)
			}
			// No early termination just in case there are any duplicate names
			matched[key] = struct{}{}
		}
	}
	if len(matched) != len(s.idx) {
		for key, i := range s.idx {
			_, _, neg := parseTag(s.spec[i])
			if _, ok := matched[key]; !(ok || neg) {
				what := "name"
				if s.ids {
					what = "id"
				}
				return nil, fmt.Errorf("account %s %q not found", what, key)
			}
		}
	}
	return result, nil
}

// filterTags filters accounts by tags, switching over to names if an account
// with a matching name is found.
func (s accountSpec) filterTags(all []*Account) ([]*Account, error) {
	result := all[:0]
	for i, ac := range all {
		if _, ok := s.idx[ac.Name]; ok {
			return s.filterNamesOrIDs(all[i:])
		}
		ctl := ac.Ctl()
		if ac.Error() != nil {
			if s.err {
				result = append(result, ac)
			}
			continue
		}
		if s.owner != nil {
			if want, ok := s.owner[ctl.Owner]; ok {
				if !want {
					continue
				}
			} else if b, ok := s.owner[""]; !ok || b {
				continue
			}
		}
		var tagMask uint64
		for _, tag := range ctl.Tags {
			if i, ok := s.idx[tag]; ok {
				tagMask |= uint64(1) << i
			}
		}
		if tagMask == s.tagMask {
			result = append(result, ac)
		}
	}
	return result, nil
}

// isAWSAccountID tests whether id is a valid AWS account ID.
func isAWSAccountID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for i := 11; i >= 0; i-- {
		if c := id[i]; c < '0' || '9' < c {
			return false
		}
	}
	return true
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

// makeTag is the inverse of parseTag.
func makeTag(name, value string, neg bool) string {
	if value == "" {
		if neg {
			return "!" + name
		}
		return name
	} else if neg {
		return name + "!=" + value
	}
	return name + "=" + value
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
