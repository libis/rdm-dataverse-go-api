// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package api

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The client must work against every Dataverse the field runs:
//   - decodedOnly: Dataverse < 6.11 (also < 6.10) validates the signature ONLY against the
//     URL-decoded request URI.
//   - verbatimFirst: Dataverse >= 6.11 validates against the raw request bytes first, then falls
//     back to the decoded form.
//
// Both mock servers sign byte-exactly what is submitted to requestSignedUrl (the pre-6.10 and
// post-fix behavior; 6.10's re-encoding bug is a server defect no client can compensate for).
const testKey = "test-signing-key"

func sha512hex(s string) string {
	h := sha512.Sum512([]byte(s))
	return hex.EncodeToString(h[:])
}

func signLikeDataverse(u string) string {
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	toSign := u + sep + "until=2999-01-01T00:00:00.000&user=alice&method=GET&token="
	return toSign + sha512hex(toSign+testKey)
}

func validates(raw string) bool {
	i := strings.LastIndex(raw, "token=")
	if i < 0 {
		return false
	}
	return raw[i+len("token="):] == sha512hex(raw[:i+len("token=")]+testKey)
}

func mockDataverse(t *testing.T, verbatimFirst bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/requestSignedUrl") {
			req := SigningRequest{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("bad signing request: %v", err)
			}
			resp := SignedUrlResponse{}
			resp.Status = "OK"
			resp.Data.SignedUrl = signLikeDataverse(req.Url)
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encoding signing response: %v", err)
			}
			return
		}
		// Validate like SignedUrlAuthMechanism. r.RequestURI preserves the wire bytes.
		raw := "http://" + r.Host + r.RequestURI
		ok := false
		if verbatimFirst {
			ok = validates(raw)
		}
		if !ok {
			if decoded, err := url.QueryUnescape(raw); err == nil {
				ok = validates(decoded)
			}
		}
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"status":"ERROR","message":"Bad signed URL"}`)
			return
		}
		fmt.Fprint(w, `{"status":"OK"}`)
	}))
}

// Every URL shape rdm-integration sends, raw and percent-encoded alike.
var testPaths = []string{
	"/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=doi:10.5072/FK2/ABC",
	"/api/v1/datasets/:persistentId/userPermissions?persistentId=doi%3A10.5072%2FFK2%2FABC",
	"/api/v1/datasets/:persistentId?persistentId=doi%3A10.5072%2FFK2%2FABC&excludeFiles=true",
	"/api/v1/mydata/retrieve?selected_page=1&mydata_search_term=text%3A%22hello+world%22",
	"/api/v1/admin/permissions/:persistentId?persistentId=doi:10.5072/FK2/ABC&unblock-key=UNBLOCK",
	"/api/v1/users/:me",
}

func doSigned(t *testing.T, server, path string) DvResponse {
	t.Helper()
	client := NewUrlSigningClient(server, "alice", "admin-api-key", "unblock")
	req := client.NewRequest(path, "GET", nil, nil)
	res := DvResponse{}
	if err := Do(context.Background(), req, &res); err != nil {
		t.Fatalf("request failed for %s: %v", path, err)
	}
	return res
}

func TestSignedRequestsAgainstOlderDataverse(t *testing.T) {
	server := mockDataverse(t, false)
	defer server.Close()
	for _, path := range testPaths {
		if res := doSigned(t, server.URL, path); res.Status != "OK" {
			t.Errorf("signed request must validate on a decoded-only (pre-6.11) server: %s -> %+v", path, res)
		}
	}
}

func TestSignedRequestsAgainstVerbatimFirstDataverse(t *testing.T) {
	server := mockDataverse(t, true)
	defer server.Close()
	for _, path := range testPaths {
		if res := doSigned(t, server.URL, path); res.Status != "OK" {
			t.Errorf("signed request must validate on a verbatim-first (6.11+) server: %s -> %+v", path, res)
		}
	}
}

func TestTamperedSignedRequestRejected(t *testing.T) {
	server := mockDataverse(t, true)
	defer server.Close()
	signed := signLikeDataverse(server.URL + "/api/v1/datasets/:persistentId?persistentId=doi:10.5072/FK2/ABC")
	tampered := strings.Replace(signed, "FK2/ABC", "FK2/HACKED", 1)
	resp, err := http.Get(tampered)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("tampered URL must be rejected, got %d", resp.StatusCode)
	}
}
