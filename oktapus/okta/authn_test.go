package okta

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiFactorSecurityQuestion(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "question")
	auth.input = "answer"
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		var in response
		want := response{
			FID:        "question",
			StateToken: "state",
			Answer:     auth.input,
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		assert.Equal(t, &want, &in)
		mock.WriteJSON(w, &result{
			SessionToken: "session",
			Status:       "SUCCESS",
		})
	}
	assert.NoError(t, c.Authenticate(auth))
}

func TestMultiFactorCall(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "call")
	auth.input = "12345"
	challenge := false
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		var in response
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		if in.PassCode == "" {
			require.False(t, challenge)
			challenge = true
			want := response{
				FID:        "call",
				StateToken: "state",
			}
			require.Equal(t, &want, &in)
			mock.WriteJSON(w, &result{
				StateToken: "state",
				Status:     "MFA_CHALLENGE",
			})
		} else {
			require.True(t, challenge)
			want := response{
				FID:        "call",
				StateToken: "state",
				PassCode:   auth.input,
			}
			assert.Equal(t, &want, &in)
			mock.WriteJSON(w, &result{
				SessionToken: "session",
				Status:       "SUCCESS",
			})
		}
	}
	assert.NoError(t, c.Authenticate(auth))
}

func TestMultiFactorSMS(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "sms")
	auth.input = "123456"
	challenge := false
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		var in response
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		if in.PassCode == "" {
			require.False(t, challenge)
			challenge = true
			mock.WriteJSON(w, &result{
				StateToken: "state",
				Status:     "MFA_CHALLENGE",
			})
		} else {
			require.True(t, challenge)
			assert.Equal(t, auth.input, in.PassCode)
			mock.WriteJSON(w, &result{
				SessionToken: "session",
				Status:       "SUCCESS",
			})
		}
	}
	assert.NoError(t, c.Authenticate(auth))
}

func TestMultiFactorGoogleAuth(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "google_auth")
	auth.input = "123456"
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		var in response
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		want := response{
			FID:        "google_auth",
			StateToken: "state",
			PassCode:   auth.input,
		}
		assert.Equal(t, want, in)
		mock.WriteJSON(w, &result{
			SessionToken: "session",
			Status:       "SUCCESS",
		})
	}
	assert.NoError(t, c.Authenticate(auth))
}

func TestMultiFactorOktaAuth(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "okta_auth")
	auth.input = "123456"
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		var in response
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		want := response{
			FID:        "okta_auth",
			StateToken: "state",
			PassCode:   auth.input,
		}
		assert.Equal(t, want, in)
		mock.WriteJSON(w, &result{
			SessionToken: "session",
			Status:       "SUCCESS",
		})
	}
	assert.NoError(t, c.Authenticate(auth))
}

func TestMultiFactorPush(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "push")
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		var in response
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		want := response{
			FID:        "push",
			StateToken: "state",
		}
		assert.Equal(t, want, in)
		mock.WriteJSON(w, &result{
			StateToken:   "state",
			Status:       "MFA_CHALLENGE",
			FactorResult: "WAITING",
			Links: struct{ Next, Prev *link }{
				Next: &link{Href: "https://localhost/next"},
			},
		})
	}
	s.Response["/next"] = func(w http.ResponseWriter, r *http.Request) {
		mock.WriteJSON(w, &result{
			SessionToken: "session",
			Status:       "SUCCESS",
		})
	}
	fast.MockSleep(-1)
	defer fast.MockSleep(0)
	assert.NoError(t, c.Authenticate(auth))
}

func TestMultiFactorPushRejected(t *testing.T) {
	c, s := newClientServer(false)
	auth := newAuth(s)
	auth.sel = mfaRequired(s, "push")
	s.Response["/verify"] = func(w http.ResponseWriter, r *http.Request) {
		mock.WriteJSON(w, &result{
			StateToken:   "state",
			Status:       "MFA_CHALLENGE",
			FactorResult: "REJECTED",
			Links: struct{ Next, Prev *link }{
				Prev: &link{Href: "https://localhost/next"},
			},
		})
	}
	s.Response["/prev"] = func(w http.ResponseWriter, r *http.Request) {
		mock.WriteJSON(w, &result{
			SessionToken: "session",
			Status:       "LOCKED_OUT",
		})
	}
	assert.Error(t, c.Authenticate(auth), "okta: authentication failed (LOCKED_OUT)")
}

func mfaRequired(s *mock.Server, id string) int {
	links := struct{ Verify *link }{
		Verify: &link{Href: "https://localhost/verify"},
	}
	factors := []*Factor{{
		ID:         "question",
		FactorType: "question",
		Provider:   "OKTA",
		Profile:    profile{QuestionText: "?"},
		Links:      links,
	}, {
		ID:         "call",
		FactorType: "call",
		Provider:   "OKTA",
		Profile:    profile{PhoneNumber: "+1 XXX-XXX-1337", PhoneExtension: "1"},
		Links:      links,
	}, {
		ID:         "sms",
		FactorType: "sms",
		Provider:   "OKTA",
		Profile:    profile{PhoneNumber: "+1 XXX-XXX-1337"},
		Links:      links,
	}, {
		ID:         "google_auth",
		FactorType: "token:software:totp",
		Provider:   "GOOGLE",
		Links:      links,
	}, {
		ID:         "okta_auth",
		FactorType: "token:software:totp",
		Provider:   "OKTA",
		Links:      links,
	}, {
		ID:         "push",
		FactorType: "push",
		Provider:   "OKTA",
		Links:      links,
	}, {
		ID:         "unsupported",
		FactorType: "unsupported",
		Provider:   "OKTA",
		Links:      links,
	}}
	s.Response["/api/v1/authn"] = &result{
		StateToken: "state",
		Status:     "MFA_REQUIRED",
		Embedded:   struct{ Factors []*Factor }{Factors: factors},
	}
	for i, f := range factors {
		if f.ID == id {
			return i
		}
	}
	return 0
}
