package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestBodyWithDataAfterTheJSONValueIsRefused(t *testing.T) {
	type body struct {
		A int `json:"a"`
	}
	cases := []struct {
		name    string
		in      string
		wantErr bool
		wantA   int
	}{
		{name: "single value", in: `{"a":1}`, wantA: 1},
		{name: "trailing whitespace", in: "{\"a\":1}\n  \n", wantA: 1},
		{name: "second value", in: `{"a":1} {"a":2}`, wantErr: true},
		{name: "trailing garbage", in: `{"a":1} garbage`, wantErr: true},
		{name: "trailing bracket", in: `{"a":1}]`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v body
			rec := httptest.NewRecorder()
			err := readJSON(rec, testRequest(http.MethodPost, "/x", strings.NewReader(tc.in)), &v)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrInvalid), err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantA, v.A)
		})
	}
}

func TestRequestBodyEmptyStillYieldsEOF(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	rec := httptest.NewRecorder()
	err := readJSON(rec, testRequest(http.MethodPost, "/x", strings.NewReader("")), &v)
	assert.True(t, errors.Is(err, io.EOF), "an empty body must still wrap io.EOF")
}

func TestSignInRefusesABodyWithTrailingData(t *testing.T) {
	x := newHarness(t)
	rec := x.do(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"alice","password":"whatever-long-password"} {"extra":true}`, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
