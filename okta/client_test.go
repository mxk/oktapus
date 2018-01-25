package okta

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientAuthenticate(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)

	res := s.Response["/api/v1/authn"].(*result)
	sess := s.Response["/api/v1/sessions"].(*Session)
	s.Response["/api/v1/authn"] = func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, auth.user, in.Username)
		require.Equal(t, auth.pass, in.Password)
		mock.WriteJSON(w, res)
	}
	s.Response["/api/v1/sessions"] = func(w http.ResponseWriter, r *http.Request) {
		var in struct{ SessionToken string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "token", in.SessionToken)
		mock.WriteJSON(w, sess)
	}

	assert.False(t, c.Authenticated())
	require.NoError(t, c.Authenticate(auth))
	assert.True(t, c.Authenticated())
	assert.Equal(t, "sid="+sess.ID, c.sidCookie)
}

func TestClientRefresh(t *testing.T) {
	c, s := newClientServer(true)
	sess := s.Response["/api/v1/sessions"].(*Session)
	s.Response["/api/v1/sessions/me"] = func(w http.ResponseWriter, r *http.Request) {
		sid, err := r.Cookie("sid")
		require.NoError(t, err)
		assert.Equal(t, sess.ID, sid.Value)
		sess.ID = "badid"
		sess.ExpiresAt = sess.ExpiresAt.Add(time.Hour)
		mock.WriteJSON(w, sess)
	}
	sid, exp := sess.ID, c.session.ExpiresAt
	require.NoError(t, c.RefreshSession())
	assert.True(t, c.session.ExpiresAt.After(exp))
	assert.Equal(t, "sid="+sid, c.sidCookie)
}

func TestClientClose(t *testing.T) {
	c, s := newClientServer(true)
	called := false
	s.Response["/api/v1/sessions/me"] = func(w http.ResponseWriter, r *http.Request) {
		called = true
		assert.Equal(t, http.MethodDelete, r.Method)
		mock.WriteJSON(w, struct{}{})
	}
	require.NoError(t, c.CloseSession())
	assert.True(t, called)
}

func TestClientEncodeDecode(t *testing.T) {
	c, s := newClientServer(true)
	b, err := c.GobEncode()
	require.NoError(t, err)

	cc := c.Client
	c = NewClient("localhost")
	c.Client = cc
	require.NoError(t, c.GobDecode(b))
	assert.True(t, c.Authenticated())

	sess := s.Response["/api/v1/sessions"].(*Session)
	s.Response["/api/v1/sessions/me"] = func(w http.ResponseWriter, r *http.Request) {
		sess.ExpiresAt = sess.ExpiresAt.Add(time.Hour)
		mock.WriteJSON(w, sess)
	}
	exp := c.session.ExpiresAt
	require.NoError(t, c.RefreshSession())
	assert.True(t, c.session.ExpiresAt.After(exp))
}

func TestClientError(t *testing.T) {
	c, s := newClientServer(true)
	err := &Error{
		Code:    "code",
		Summary: "summary",
		Link:    "link",
		ReqID:   "id",
	}
	s.Response["/api/v1/sessions/me"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		mock.WriteJSON(w, err)
	}
	assert.Equal(t, err, c.RefreshSession())
	assert.NotEmpty(t, err.Error())
}

type auth struct {
	user  string
	pass  string
	sel   int
	input string
	err   error
}

func newAuth(s *mock.Server) *auth {
	if s != nil {
		now := internal.Time().Truncate(time.Second)
		s.Response["/api/v1/authn"] = &result{
			SessionToken: "token",
			Status:       "SUCCESS",
		}
		s.Response["/api/v1/sessions"] = &Session{
			ID:        "101W_juydrDRByB7fUdRyE2JQ",
			Login:     "user@example.com",
			UserID:    "00ubgaSARVOQDIOXMORI",
			CreatedAt: now,
			ExpiresAt: now.Add(time.Hour),
			Status:    "ACTIVE",
		}
	}
	return &auth{user: "user", pass: "pass", input: "123"}
}

func (a *auth) Username() (string, error)         { return a.user, a.err }
func (a *auth) Password() (string, error)         { return a.pass, a.err }
func (a *auth) Select(c []Choice) (Choice, error) { return c[a.sel], a.err }
func (a *auth) Input(Choice) (string, error)      { return a.input, a.err }
func (a *auth) Notify(string, ...interface{})     {}

func newClientServer(auth bool) (*Client, *mock.Server) {
	var s *mock.Server
	c := NewClient("localhost")
	c.Client, s = mock.ClientServer()
	if auth {
		if err := c.Authenticate(newAuth(s)); err != nil {
			panic(err)
		}
	}
	return c, s
}
