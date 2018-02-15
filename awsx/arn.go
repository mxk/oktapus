package awsx

import (
	"path"
	"strconv"
	"strings"
)

const (
	arnPrefix = ARN("arn:")
	arnFields = 5
	arnBuf    = 256
)

// NilARN is an ARN without any fields set.
const NilARN = ARN("arn:::::")

// ARN is an Amazon Resource Name.
type ARN string

// NewARN constructs an ARN from the specified fields. Any of the fields may be
// left blank.
func NewARN(partition, service, region, account string, resource ...string) ARN {
	var buf [arnBuf]byte
	b := append(buf[:0], arnPrefix...)
	b = append(append(b, partition...), ':')
	b = append(append(b, service...), ':')
	b = append(append(b, region...), ':')
	b = append(append(b, account...), ':')
	for _, r := range resource {
		b = append(b, r...)
	}
	return ARN(b)
}

// ARNValue returns the value of the ARN string pointer passed in or "" if the
// pointer is nil.
func ARNValue(s *string) ARN {
	if s == nil {
		return ""
	}
	return ARN(*s)
}

// Str behaves like aws.String() for ARN values.
func (r ARN) Str() *string {
	s := string(r)
	return &s
}

// Field getters.
func (r ARN) Partition() string { return r.Field(0) }
func (r ARN) Service() string   { return r.Field(1) }
func (r ARN) Region() string    { return r.Field(2) }
func (r ARN) Account() string   { return r.Field(3) }
func (r ARN) Resource() string  { return r.Field(4) }

// Field setters.
func (r ARN) WithPartition(v string) ARN { return r.WithField(0, v) }
func (r ARN) WithService(v string) ARN   { return r.WithField(1, v) }
func (r ARN) WithRegion(v string) ARN    { return r.WithField(2, v) }
func (r ARN) WithAccount(v string) ARN   { return r.WithField(3, v) }
func (r ARN) WithResource(v string) ARN  { return r.WithField(4, v) }

// Valid returns true if r has a valid prefix and the required number of fields.
func (r ARN) Valid() bool {
	for n, i := 1, len(arnPrefix); i < len(r); i++ {
		if r[i] == ':' {
			if n++; n == arnFields {
				return r[:len(arnPrefix)] == arnPrefix
			}
		}
	}
	return false
}

// Type returns the resource prefix up to the first '/' or ':' character. It
// returns an empty string if neither character is found.
func (r ARN) Type() string {
	i, j := r.typ()
	return string(r[i:j])
}

// Path returns the resource substring between and including the first and last
// '/' characters. It ignores any part of the resource before the last ':' and
// returns an empty string if the resource does not contain any '/' characters.
func (r ARN) Path() string {
	i, j := r.path()
	return string(r[i:j])
}

// Name returns the resource suffix after the last '/' or ':' character. It
// returns the whole resource field if neither character is found.
func (r ARN) Name() string {
	return string(r[r.name():])
}

// WithPath returns a new ARN with path replaced by v. It panics if r does not
// have a path.
func (r ARN) WithPath(v string) ARN {
	i, j := r.path()
	if i == j {
		panic("awsx: arn has no path: " + r)
	}
	return concat(r[:i], cleanPath(v), "/", r[j:])
}

// WithPathName returns a new ARN with path and name replaced by v. It panics if
// r does not have a path.
func (r ARN) WithPathName(v string) ARN {
	i, j := r.path()
	if i == j {
		panic("awsx: arn has no path: " + r)
	}
	j = strings.LastIndexByte(v, '/')
	return concat(r[:i], cleanPath(v[:j+1]), "/", ARN(v[j+1:]))
}

// WithName returns a new ARN with name replaced by v.
func (r ARN) WithName(v string) ARN {
	return concat(r[:r.name()], ARN(v))
}

// Field returns the ith ARN field.
func (r ARN) Field(i int) string {
	j, k := r.field(i)
	return string(r[j:k])
}

// WithField returns a new ARN with the ith field set to v.
func (r ARN) WithField(i int, v string) ARN {
	j, k := r.field(i)
	return concat(r[:j], ARN(v), r[k:])
}

// With returns a new ARN, with non-empty fields in o replacing those in r.
func (r ARN) With(o ARN) ARN {
	var f [arnFields]string
	for i := 0; i < arnFields; i++ {
		if s := o.Field(i); s != "" {
			f[i] = s
		} else {
			f[i] = r.Field(i)
		}
	}
	return NewARN(f[0], f[1], f[2], f[3], f[4])
}

// field returns slice indices of the ith field.
func (r ARN) field(i int) (int, int) {
	j := len(arnPrefix)
	if len(r) < j || r[:j] != arnPrefix {
		panic("awsx: invalid arn: " + r)
	}
	for n := i; ; n-- {
		k := strings.IndexByte(string(r[j:]), ':')
		if k < 0 {
			panic("awsx: invalid field index " + strconv.Itoa(i) +
				" in arn: " + string(r))
		}
		if n <= 1 {
			if n <= 0 {
				return j, j + k
			} else if i >= arnFields-1 {
				return j + k + 1, len(r)
			}
		}
		j += k + 1
	}
}

// type returns the slice indices of the resource type.
func (r ARN) typ() (int, int) {
	i, _ := r.field(arnFields - 1)
	for j := i; j < len(r); j++ {
		if c := r[j]; c == '/' || c == ':' {
			return i, j
		}
	}
	return i, i
}

// path returns slice indices of the resource path.
func (r ARN) path() (int, int) {
	for i, j, k := len(r)-1, 0, 0; i >= 0; i-- {
		if c := r[i]; c == '/' {
			if j = i; k == 0 {
				k = i + 1
			}
		} else if c == ':' {
			return j, k
		}
	}
	panic("awsx: invalid arn: " + r)
}

// name returns the starting index of the resource name.
func (r ARN) name() int {
	for i := len(r) - 1; i >= 0; i-- {
		if c := r[i]; c == '/' || c == ':' {
			return i + 1
		}
	}
	panic("awsx: invalid arn: " + r)
}

// cleanPath normalizes path p, returning either an empty string or an absolute
// path without a trailing '/'.
func cleanPath(p string) ARN {
	if p != "" {
		if p[0] != '/' {
			p = "/" + p
		}
		if p = path.Clean(p); p == "/" {
			p = ""
		}
	}
	return ARN(p)
}

// concat concatenates all parts of an ARN.
func concat(parts ...ARN) ARN {
	var buf [arnBuf]byte
	b := buf[:0]
	for _, s := range parts {
		b = append(b, s...)
	}
	return ARN(b)
}
