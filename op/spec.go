package op

import (
	"fmt"
	"sort"
	"strings"
)

// AccountSpec specifies how to filter accounts.
type AccountSpec struct {
	spec    []string        // Original spec split by commas
	idx     map[string]uint // Map of non-special tag names to spec indices
	owner   map[string]bool // Map of owner names to match criteria
	tagMask uint64          // Tag matching mask
	ids     bool            // Filter by account IDs
	err     bool            // Include inaccessible accounts (ac.Error()!=nil)
}

// NewAccountSpec parses the account spec string. User argument determines the
// meaning of "owner=me" specification.
func NewAccountSpec(spec, user string) *AccountSpec {
	s := new(AccountSpec)
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

// Num returns the number of unique account IDs, names, or non-special tags in
// the spec.
func (s *AccountSpec) Num() int {
	return len(s.idx)
}

// Filter returns only those accounts that match the spec.
func (s *AccountSpec) Filter(all Accounts) (Accounts, error) {
	if s.ids || len(s.spec) > 64 {
		return s.filterNamesOrIDs(all)
	}
	// Assume that we're filtering by tags. If a matching account name is found,
	// we switch filters at that point. This eliminates the need to make two
	// passes through all accounts.
	return s.filterTags(all)
}

func (s *AccountSpec) Update(tags []string) ([]string, error) {
	m := make(map[string]struct{}, len(tags)+len(s.spec))
	for _, tag := range tags {
		m[tag] = struct{}{} // TODO: Validate?
	}
	for _, tag := range s.spec {
		if !isTag(tag, true) {
			return nil, fmt.Errorf("invalid tag %q", tag)
		} else if tag, _, neg := parseTag(tag); neg {
			delete(m, tag)
		} else {
			m[tag] = struct{}{}
		}
	}
	tags = make([]string, 0, len(m))
	for tag := range m {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

// filterNamesOrIDs filters accounts by either names or IDs. All non-negated
// entries in s.idx must match an account. Error status is not considered.
func (s *AccountSpec) filterNamesOrIDs(all Accounts) (Accounts, error) {
	var result Accounts
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
func (s AccountSpec) filterTags(all Accounts) (Accounts, error) {
	var result Accounts
	for i, ac := range all {
		if _, ok := s.idx[ac.Name]; ok {
			return s.filterNamesOrIDs(all[i:])
		}
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
