package op

import (
	"fmt"
	"strconv"
	"strings"
)

type specType byte

const (
	unknown specType = iota
	ids
	names
	tags
)

// AccountSpec specifies how to filter accounts.
type AccountSpec struct {
	spec    []string        // Original spec split by commas
	idx     map[string]uint // Map of non-special names to spec indices
	owner   map[string]bool // Map of owner names to match criteria
	tagMask uint64          // Tag matching mask
	err     bool            // Include inaccessible accounts
	typ     specType        // Static (ids/names) or dynamic (tags) spec type
}

// ParseAccountSpec parses the account spec string. User argument determines the
// meaning of "owner=me" specification.
func ParseAccountSpec(spec, user string) *AccountSpec {
	s := new(AccountSpec)
	if spec == "" {
		s.typ = tags
		return s
	}
	s.spec = strings.Split(spec, ",")
	s.idx = make(map[string]uint, len(s.spec))
	for i, e := range s.spec {
		if name, val, neg := parseSpec(e); isSpecial(name) {
			switch name {
			case "err":
				s.err = !neg
			case "owner":
				if s.owner == nil {
					s.owner = make(map[string]bool, 2)
				}
				if val == "me" {
					if user == "" {
						continue
					}
					val = user
				}
				// owner[!]=x sets s.owner["x"] = !neg
				// [!]owner sets s.owner[""] = neg
				s.owner[val] = neg == (val == "")
			default:
				panic("unhandled tag: " + name)
			}
		} else {
			if s.idx[name] = uint(i); !neg {
				s.tagMask |= uint64(1) << uint(i)
			}
			if s.typ == unknown && isAWSAccountID(name) {
				s.typ = ids
			}
		}
	}
	if s.typ == unknown && len(s.spec) > 64 {
		s.typ = names
	}
	return s
}

// IsStatic returns true if the spec uses account IDs and/or names.
func (s *AccountSpec) IsStatic(all Accounts) bool {
	if s.typ != unknown {
		return s.typ != tags
	}
	for _, ac := range all {
		if _, ok := s.idx[ac.Name]; ok {
			s.typ = names
			return true
		}
	}
	s.typ = tags
	return false
}

// Filter returns only those accounts that match the spec.
func (s *AccountSpec) Filter(all Accounts) (Accounts, error) {
	if s.IsStatic(all) {
		return s.filterStatic(all)
	}
	return s.filterDynamic(all)
}

// filterStatic filters accounts by names and/or IDs. All non-negated entries in
// s.idx must match an account. Error status is not considered.
func (s *AccountSpec) filterStatic(all Accounts) (Accounts, error) {
	var result Accounts
	if len(s.idx) == 0 {
		return result, nil
	}
	matched := make(map[string]struct{}, len(s.idx))
	for _, ac := range all {
		key := ac.Name
		if s.typ == ids {
			key = ac.ID
		}
		if i, ok := s.idx[key]; ok {
			if _, _, neg := parseSpec(s.spec[i]); !neg {
				result = append(result, ac)
			}
			matched[key] = struct{}{}
		}
	}
	if len(matched) != len(s.idx) {
		for key, i := range s.idx {
			_, _, neg := parseSpec(s.spec[i])
			if _, ok := matched[key]; !(ok || neg) {
				what := "name"
				if s.typ == ids {
					what = "id"
				}
				return nil, fmt.Errorf("account %s %q not found", what, key)
			}
		}
	}
	return result, nil
}

// filterDynamic filters accounts by tags.
func (s AccountSpec) filterDynamic(all Accounts) (Accounts, error) {
	var result Accounts
	for _, ac := range all {
		if ac.Ctl == nil {
			if s.err {
				result = append(result, ac)
			}
			continue
		}
		if s.owner != nil {
			if want, ok := s.owner[ac.Owner]; ok {
				if !want {
					continue
				}
			} else if b, ok := s.owner[""]; !ok || b {
				continue
			}
		}
		var tagMask uint64
		for _, tag := range ac.Tags {
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

// parseSpec splits an account spec entry into its components. The general
// format is: "[!...]name[[!]=value]". If value is a boolean, it determines the
// initial negation state instead of being returned as a string.
func parseSpec(s string) (name, value string, neg bool) {
	if i := strings.IndexByte(s, '='); i != -1 {
		s, value = s[:i], s[i+1:]
		if t, err := strconv.ParseBool(value); err == nil {
			value, neg = "", !t
		}
		if i > 0 && s[i-1] == '!' {
			s, neg = s[:i-1], !neg
		}
	}
	for len(s) > 0 && s[0] == '!' {
		s, neg = s[1:], !neg
	}
	name = s
	return
}
