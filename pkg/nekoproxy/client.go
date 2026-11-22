package nekoproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const apiPath = "/api/v1/l4"

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var errorCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,80}$`)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type Service struct {
	ID            string `json:"id"`
	Remark        string `json:"remark"`
	ChainID       string `json:"chainId"`
	ChainName     string `json:"chainName"`
	ChainDisabled bool   `json:"chainDisabled"`
	Protocol      string `json:"protocol"`
	TargetHost    string `json:"targetHost"`
	TargetPort    int    `json:"targetPort"`
	Enabled       bool   `json:"enabled"`
	Paused        bool   `json:"paused"`
}

type Session struct {
	SessionID     string `json:"sessionId"`
	ServiceID     string `json:"serviceId"`
	AllocatedPort int    `json:"allocatedPort"`
	ListenAddr    string `json:"listenAddr"`
	NodeHost      string `json:"nodeHost"`
	EntryGroupID  string `json:"entryGroupId"`
	Status        string `json:"status"`
}

type Chain struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EntryGroup struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ListenHost string `json:"listenHost"`
	PortStart  int    `json:"portStart"`
	PortEnd    int    `json:"portEnd"`
}

type CreateService struct {
	ChainID    string `json:"chainId"`
	TargetHost string `json:"targetHost"`
	TargetPort int    `json:"targetPort"`
	Protocol   string `json:"protocol"`
	Remark     string `json:"remark"`
}

type IssueSession struct {
	EntryGroupID  string `json:"entryGroupId"`
	RequestedPort int    `json:"requestedPort"`
}

func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("API address must be an HTTP(S) URL without credentials, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, apiPath)
	u.Path = strings.TrimSuffix(u.Path, "/api")
	u.RawPath = ""
	value := strings.TrimRight(u.String(), "/")
	if len(value) > 512 {
		return "", errors.New("API address is too long")
	}
	return value, nil
}

func New(rawURL, token string) (*Client, error) {
	baseURL, err := NormalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "nkp_") || len(token) <= 4 || strings.ContainsAny(token, " \r\n\t") {
		return nil, errors.New("a Nekoproxy nkp_ API token is required")
	}
	return &Client{baseURL: baseURL, token: token, http: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+apiPath+path, reader)
	if err != nil {
		return errors.New("invalid Nekoproxy request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("Nekoproxy connection failed or timed out")
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(payload) > 4<<20 {
		return errors.New("Nekoproxy response is unreadable or too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(payload, &failure)
		if !errorCode.MatchString(failure.Error) {
			failure.Error = "REQUEST_FAILED"
		}
		return fmt.Errorf("Nekoproxy HTTP %d: %s", resp.StatusCode, failure.Error)
	}
	if out != nil && json.Unmarshal(payload, out) != nil {
		return errors.New("Nekoproxy returned an invalid API response")
	}
	return nil
}

func listAll[T any](ctx context.Context, c *Client, path string, wrapped bool) ([]T, error) {
	all := make([]T, 0)
	for offset := 0; offset < 100000; offset += 500 {
		page := make([]T, 0)
		var err error
		if wrapped {
			result := struct {
				Rows []T `json:"rows"`
			}{}
			err = c.request(ctx, http.MethodGet, fmt.Sprintf("%s?limit=500&offset=%d", path, offset), nil, &result)
			page = result.Rows
		} else {
			err = c.request(ctx, http.MethodGet, fmt.Sprintf("%s?limit=500&offset=%d", path, offset), nil, &page)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < 500 {
			return all, nil
		}
	}
	return nil, errors.New("Nekoproxy result exceeds the synchronization limit")
}

func (c *Client) Check(ctx context.Context) error {
	var services []Service
	return c.request(ctx, http.MethodGet, "/services?limit=1&offset=0", nil, &services)
}
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	return listAll[Service](ctx, c, "/services", false)
}
func (c *Client) Sessions(ctx context.Context) ([]Session, error) {
	return listAll[Session](ctx, c, "/sessions", true)
}
func (c *Client) Chains(ctx context.Context) ([]Chain, error) {
	return listAll[Chain](ctx, c, "/chains", false)
}
func (c *Client) Groups(ctx context.Context) ([]EntryGroup, error) {
	return listAll[EntryGroup](ctx, c, "/entry-groups", false)
}

func (c *Client) Service(ctx context.Context, id string) (*Service, error) {
	if !identifier.MatchString(id) {
		return nil, errors.New("invalid forward ID")
	}
	var result Service
	if err := c.request(ctx, http.MethodGet, "/services/"+id, nil, &result); err != nil {
		return nil, err
	}
	if result.ID != id {
		return nil, errors.New("Nekoproxy returned a different forward ID")
	}
	return &result, nil
}
func (c *Client) UpdateTargetPort(ctx context.Context, id string, expected, target uint16) error {
	if !identifier.MatchString(id) || expected == 0 || target == 0 {
		return errors.New("invalid forward ID or target port")
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := c.request(ctx, http.MethodPatch, "/services/"+id+"/target-port", map[string]uint16{"expectedTargetPort": expected, "targetPort": target}, &result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New("Nekoproxy did not confirm the target port update")
	}
	return nil
}

func (c *Client) Create(ctx context.Context, input CreateService) (*Service, error) {
	var result Service
	if err := c.request(ctx, http.MethodPost, "/services", input, &result); err != nil {
		return nil, err
	}
	if !identifier.MatchString(result.ID) {
		return nil, errors.New("Nekoproxy returned an invalid forward ID")
	}
	return &result, nil
}
func (c *Client) Issue(ctx context.Context, id string, input IssueSession) (*Session, error) {
	if !identifier.MatchString(id) {
		return nil, errors.New("invalid forward ID")
	}
	var result Session
	if err := c.request(ctx, http.MethodPost, "/services/"+id+"/sessions", input, &result); err != nil {
		return nil, err
	}
	if result.ServiceID != id || !identifier.MatchString(result.SessionID) {
		return nil, errors.New("Nekoproxy returned an invalid forwarding session")
	}
	return &result, nil
}
func (c *Client) Delete(ctx context.Context, id string) error {
	if !identifier.MatchString(id) {
		return errors.New("invalid forward ID")
	}
	return c.request(ctx, http.MethodDelete, "/services/"+id, nil, nil)
}

func (s Session) Endpoint() (string, uint16, error) {
	host := strings.Trim(strings.TrimSpace(s.NodeHost), "[]")
	if listenHost, port, err := net.SplitHostPort(s.ListenAddr); err == nil {
		if parsed, _ := strconv.Atoi(port); parsed != s.AllocatedPort {
			return "", 0, errors.New("forwarding session port does not match its listener")
		}
		host = listenHost
	}
	if s.Status != "active" || s.AllocatedPort < 1 || s.AllocatedPort > 65535 || host == "" || len(host) > 255 || strings.ContainsAny(host, "/\\ \t\r\n@?#") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
		return "", 0, errors.New("select an active forwarding session with a valid entry address and port")
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return "", 0, errors.New("forwarding session has a wildcard entry address")
	}
	return host, uint16(s.AllocatedPort), nil
}
