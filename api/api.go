// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// signatureFieldsRe matches the signature block Dataverse appends to the end of
// a signed URL: ...[?&]until=<ts>&user=<id>&method=<verb>&token=<hex>. It is
// anchored to the end of the string so it reliably extracts these fields even
// when the preceding URL contains a literal '%' (a bare, non-escape percent),
// which would make url.Parse on the whole signed URL fail.
var signatureFieldsRe = regexp.MustCompile(`[?&]until=([^&]*)&user=([^&]*)&method=([^&]*)&token=([^&]*)$`)

type Request struct {
	DataverseServer string
	Path            string
	Method          string
	RequestBody     io.Reader
	RequestHeader   http.Header
	Token           string
	Credentials
}

type Credentials struct {
	User       string
	ApiKey     string
	UnblockKey string
}

type Client struct {
	Server      string
	Token       string
	User        string
	AdminApiKey string
	UnblockKey  string
}

func NewClient(server string) *Client {
	return &Client{
		Server: server,
	}
}

func NewUrlSigningClient(server, user, adminApiKey, unblockKey string) *Client {
	return &Client{
		Server:      server,
		User:        user,
		AdminApiKey: adminApiKey,
		UnblockKey:  unblockKey,
	}
}

func NewTokenAccessClient(server, token string) *Client {
	return &Client{
		Server: server,
		Token:  token,
	}
}

func (client *Client) NewRequest(path, method string, body io.Reader, header http.Header) *Request {
	return &Request{
		DataverseServer: client.Server,
		Path:            path,
		Method:          method,
		RequestBody:     body,
		RequestHeader:   header,
		Token:           client.Token,
		Credentials:     client.getCredentials(),
	}
}

func (client *Client) getCredentials() (res Credentials) {
	if client.AdminApiKey != "" && client.UnblockKey != "" && client.User != "" {
		res = Credentials{
			User:       client.User,
			ApiKey:     client.AdminApiKey,
			UnblockKey: client.UnblockKey,
		}
	}
	return
}

func JsonContentHeader() http.Header {
	res := http.Header{}
	res.Add("Content-Type", "application/json")
	return res
}

// res is where the response will be unmarshalled (e.g., map or a pointer to struct)
func Do(ctx context.Context, req *Request, res interface{}) error {
	stream, err := DoStream(ctx, req)
	if err != nil {
		return err
	}
	return unmarshalAndCloseStream(stream, res)
}

// do not forget to close the stream after reading...
func DoStream(ctx context.Context, req *Request) (io.ReadCloser, error) {
	u, addTokenToHeader, err := signUrl(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("error signing url")
	}
	request, err := http.NewRequestWithContext(ctx, req.Method, u, req.RequestBody)
	if err != nil {
		return nil, err
	}
	if addTokenToHeader && req.Token != "" {
		request.Header.Add("X-Dataverse-key", req.Token)
	}
	for k, v := range req.RequestHeader {
		for _, s := range v {
			request.Header.Add(k, s)
		}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func signUrl(ctx context.Context, req *Request) (string, bool, error) {
	u := req.DataverseServer + req.Path
	if strings.HasPrefix(req.Path, req.DataverseServer) {
		u = req.Path
	}
	if req.ApiKey == "" || req.UnblockKey == "" || req.User == "" {
		return u, true, nil
	}
	// Dataverse signs the URL-decoded form and, on validation, URL-decodes the request before
	// checking the signature. So we un-escape before signing, then build the request from the
	// original (still-encoded) URL plus the returned signature fields; the server decodes that back
	// to the form it signed. (Using the returned signedUrl verbatim would fail: a percent-encoded
	// value decodes to different bytes than were signed.)
	unescaped, err := url.QueryUnescape(u)
	if err != nil {
		return "", false, err
	}
	resp, err := http.DefaultClient.Do(signingRequest(ctx, req, unescaped))
	if err != nil {
		return "", false, err
	}
	res := SignedUrlResponse{}
	err = unmarshalAndCloseStream(resp.Body, &res)
	if err != nil {
		return "", false, err
	}
	if res.Status != "OK" {
		return "", false, fmt.Errorf("%s", res.Message)
	}
	// Extract the appended signature fields (until/user/method/token) directly
	// from the end of the signed URL. We cannot url.Parse the whole signed URL:
	// when the original URL carries a literal '%' (e.g. a value that decoded to
	// "100%"), the echoed-back signed URL contains a bare, non-escape percent
	// and url.Parse rejects it with "invalid URL escape". The signature fields
	// themselves are always safe ASCII, so a tail match is sufficient.
	m := signatureFieldsRe.FindStringSubmatch(res.Data.SignedUrl)
	if m == nil {
		return "", false, fmt.Errorf("signed url missing signature fields: %s", res.Data.SignedUrl)
	}
	until, user, method, token := m[1], m[2], m[3], m[4]
	if user != req.User {
		return "", false, fmt.Errorf("unknown user: %v", req.User)
	}
	qm := "?"
	if strings.Contains(u, "?") {
		qm = "&"
	}
	signedUrl := fmt.Sprintf("%s%suntil=%s&user=%s&method=%s&token=%s", u, qm, until, user, method, token)
	return signedUrl, false, nil
}

func signingRequest(ctx context.Context, req *Request, u string) *http.Request {
	jsonString, _ := json.Marshal(SigningRequest{u, 500, req.User, req.Method})
	signingServiceUrl := req.DataverseServer + "/api/v1/admin/requestSignedUrl?unblock-key=" + req.UnblockKey
	body := bytes.NewBuffer([]byte(jsonString))
	request, _ := http.NewRequestWithContext(ctx, "POST", signingServiceUrl, body)
	request.Header.Add("X-Dataverse-key", req.ApiKey)
	request.Header.Add("Content-Type", "application/json")
	return request
}

func unmarshalAndCloseStream(stream io.ReadCloser, res interface{}) error {
	defer stream.Close()
	b, err := io.ReadAll(stream)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &res)
}
