package op

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
)

type specType byte

const (
	stUnknown specType = iota
	stIds
	stNames
	stTags
)

type specFlags byte

const (
	sfErr specFlags = 1 << iota
	sfFree
	sfAlloc
	sfAll = sfFree | sfAlloc
)

// AccountSpec specifies how to filter accounts.
type AccountSpec struct {
	spec    []string        // Original spec split by commas
	idx     map[string]uint // Map of non-special names to spec indices
	owner   map[string]bool // Map of owner names to match criteria
	tagMask uint64          // Tag matching mask
	typ     specType        // Static (ids/names) or dynamic (tags) spec type
	flags   specFlags       // Account selection flags
}

// ParseAccountSpec parses the account spec string. User argument determines the
// meaning of "owner=me" specification.
func ParseAccountSpec(spec, user string) *AccountSpec {
	s := new(AccountSpec)
	if spec == "" {
		s.typ = stTags
		s.flags = sfAll
		return s
	}
	s.spec = strings.Split(spec, ",")
	s.idx = make(map[string]uint, len(s.spec))
	for i, e := range s.spec {
		if name, val, neg := parseSpec(e); isSpecial(name) {
			switch name {
			case "err":
				if neg {
					s.flags &^= sfErr
				} else {
					s.flags |= sfErr
				}
			case "owner":
				switch val {
				case "":
					if neg {
						s.flags = s.flags&^sfAll | sfFree
					} else {
						s.flags = s.flags&^sfAll | sfAlloc
					}
				case "me":
					val = user
					fallthrough
				default:
					if s.owner == nil {
						s.owner = make(map[string]bool, 2)
					}
					s.owner[val] = !neg
				}
			}
		} else {
			if s.idx[name] = uint(i); !neg {
				s.tagMask |= uint64(1) << uint(i)
			}
			if s.typ == stUnknown && awsx.IsAccountID(name) {
				s.typ = stIds
			}
		}
	}
	if s.typ == stUnknown && len(s.spec) > 64 {
		s.typ = stNames
	}
	if s.flags&sfAll == 0 {
		if s.owner == nil {
			s.flags |= sfAll
		} else {
			for _, want := range s.owner {
				if !want {
					s.flags |= sfAll
					break
				}
			}
		}
	}
	return s
}

// IsStatic returns true if the spec uses account IDs and/or names.
func (s *AccountSpec) IsStatic(all Accounts) bool {
	if s.typ != stUnknown {
		return s.typ != stTags
	}
	for _, ac := range all {
		if _, ok := s.idx[ac.Name]; ok {
			s.typ = stNames
			return true
		}
	}
	s.typ = stTags
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
	matched := make(map[string]struct{}, len(s.idx))
	for _, ac := range all {
		key := ac.Name
		if s.typ == stIds {
			key = ac.ID
		}
		if i, ok := s.idx[key]; ok {
			if _, _, neg := parseSpec(s.spec[i]); !neg {
				result = append(result, ac)
				matched[key] = struct{}{}
			}
		}
	}
	if len(matched) != len(s.idx) {
		for key, i := range s.idx {
			_, _, neg := parseSpec(s.spec[i])
			if _, ok := matched[key]; !ok || neg {
				what := "name"
				if s.typ == stIds {
					what = "id"
				}
				msg := "account %s %q not found"
				if neg {
					msg = "account %s %q cannot be negated"
				}
				return nil, fmt.Errorf(msg, what, key)
			}
		}
	}
	return result, nil
}

// filterDynamic filters accounts by tags.
func (s *AccountSpec) filterDynamic(all Accounts) (Accounts, error) {
	var result Accounts
	for _, ac := range all {
		if ac.Ctl == nil {
			if s.flags&sfErr != 0 {
				result = append(result, ac)
			}
			continue
		}
		if ac.Owner == "" {
			if s.flags&sfFree == 0 {
				continue
			}
		} else if want, ok := s.owner[ac.Owner]; ok {
			if !want {
				continue
			}
		} else if s.flags&sfAlloc == 0 {
			continue
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
