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
	"strings"
)

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
		return nil, err
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
		return "", false, fmt.Errorf(res.Message)
	}
	parsed, err := url.Parse(res.Data.SignedUrl)
	if err != nil {
		return "", false, err
	}
	q, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", false, err
	}
	if len(q["user"]) != 1 || q["user"][0] != req.User {
		return "", false, fmt.Errorf("unknown user: %v", req.User)
	}
	if len(q["until"]) != 1 || len(q["method"]) != 1 || len(q["token"]) != 1 {
		return "", false, fmt.Errorf("missing one of signature fields: until=%v, method=%v, token=%v", q["until"], q["method"], q["token"])
	}
	qm := "?"
	if (strings.Contains(u, "?")) {
		qm = "&"
	}
	signedUrl := fmt.Sprintf("%s%suntil=%s&user=%s&method=%s&token=%s", u, qm, q["until"][0], q["user"][0], q["method"][0], q["token"][0])
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
