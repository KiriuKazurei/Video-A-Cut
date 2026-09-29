package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type routerError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestZZRouterErrorsHaveStrictJSONContracts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "router-errors.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	if err := svc.CreateAsset(t.Context(), model.Asset{
		AssetID: "clip_1", Status: model.AssetStatusIngested,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(svc).Handler())
	defer srv.Close()

	cases := []struct {
		name, method, path string
		status             int
		code, message      string
		allow              string
	}{
		{
			name: "unknown route", method: http.MethodGet, path: "/not-a-route",
			status: http.StatusNotFound, code: "not_found",
			message: "no route for GET /not-a-route",
		},
		{
			name: "known route wrong method", method: http.MethodPut, path: "/api/assets/clip_1",
			status: http.StatusMethodNotAllowed, code: "method_not_allowed",
			message: "method PUT is not allowed for /api/assets/clip_1",
			allow:   "GET, HEAD, PATCH",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.status, body)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var env routerError
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("body is not the JSON error envelope: %v (%s)", err, body)
			}
			if env.Error.Code != tc.code {
				t.Errorf("error code = %q, want %q", env.Error.Code, tc.code)
			}
			if env.Error.Message != tc.message {
				t.Errorf("error message = %q, want %q", env.Error.Message, tc.message)
			}
			if got := resp.Header.Get("Allow"); got != tc.allow {
				t.Errorf("Allow = %q, want %q", got, tc.allow)
			}
		})
	}
}

func TestZZMissingAsset404RemainsHandlerError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "missing-asset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := httptest.NewServer(api.New(service.New(st)).Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/assets/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env routerError
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound || env.Error.Code != "not_found" {
		t.Fatalf("handler 404 = status %d, code %q, want 404/not_found", resp.StatusCode, env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "does-not-exist") || !strings.Contains(env.Error.Message, "not found") {
		t.Fatalf("handler 404 message = %q, want missing asset identity and not-found reason", env.Error.Message)
	}
}
