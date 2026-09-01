package sceneworker

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
	"os"
	"path"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/scene"
)

var ErrLeaseLost = errors.New("scene worker: lease lost")

const maxControlResponseBytes = 2 << 20

type ControlPlane interface {
	Claim(context.Context) (scene.Claim, bool, error)
	Heartbeat(context.Context, string, string, string, int) (scene.Job, error)
	Upload(context.Context, string, string, LocalArtifact) (scene.Artifact, error)
	Finish(context.Context, string, string, scene.FinishRequest) error
}

type ClientOptions struct {
	BaseURL    string
	Token      string
	Hello      scene.WorkerHello
	HTTPClient *http.Client
}

type Client struct {
	baseURL *url.URL
	token   string
	hello   scene.WorkerHello
	http    *http.Client
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(options.BaseURL))
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, errors.New("scene worker: API URL must be absolute HTTP(S)")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("scene worker: API URL cannot contain credentials, query, or fragment")
	}
	if baseURL.Scheme != "https" && !isLoopback(baseURL.Hostname()) {
		return nil, errors.New("scene worker: non-loopback API URLs require HTTPS")
	}
	if len(strings.TrimSpace(options.Token)) < 32 {
		return nil, errors.New("scene worker: API token must contain at least 32 characters")
	}
	if err := options.Hello.Validate(options.Hello.Targets); err != nil {
		return nil, err
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{}
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	return &Client{baseURL: baseURL, token: strings.TrimSpace(options.Token), hello: options.Hello, http: options.HTTPClient}, nil
}

func (c *Client) Claim(ctx context.Context) (scene.Claim, bool, error) {
	var response scene.ClaimResponse
	status, err := c.json(ctx, http.MethodPost, "/api/v1/scene-jobs/claim", c.hello, &response)
	if err != nil {
		return scene.Claim{}, false, err
	}
	if status == http.StatusNoContent {
		return scene.Claim{}, false, nil
	}
	if status != http.StatusOK || response.ProtocolVersion != scene.WorkerProtocolVersion {
		return scene.Claim{}, false, fmt.Errorf("scene worker: claim returned HTTP %d or an incompatible protocol", status)
	}
	return response.Claim, true, nil
}

func (c *Client) Heartbeat(ctx context.Context, jobID, leaseToken, stage string, progress int) (scene.Job, error) {
	payload := map[string]any{
		"worker_id": c.hello.WorkerID, "lease_token": leaseToken, "stage": stage, "progress": progress,
	}
	var response struct {
		Job scene.Job `json:"job"`
	}
	status, err := c.json(ctx, http.MethodPost, "/api/v1/scene-jobs/"+url.PathEscape(jobID)+"/heartbeat", payload, &response)
	if status == http.StatusConflict {
		return scene.Job{}, ErrLeaseLost
	}
	if err != nil {
		return scene.Job{}, err
	}
	if status != http.StatusOK {
		return scene.Job{}, fmt.Errorf("scene worker: heartbeat returned HTTP %d", status)
	}
	return response.Job, nil
}

func (c *Client) Upload(ctx context.Context, jobID, leaseToken string, artifact LocalArtifact) (scene.Artifact, error) {
	file, err := os.Open(artifact.Path)
	if err != nil {
		return scene.Artifact{}, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 {
		return scene.Artifact{}, errors.New("scene worker: artifact is not a non-empty regular file")
	}
	route := "/api/v1/scene-jobs/" + url.PathEscape(jobID) + "/artifacts/" + url.PathEscape(artifact.Kind)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, c.resolve(route), file)
	if err != nil {
		return scene.Artifact{}, err
	}
	request.ContentLength = info.Size()
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", artifact.ContentType)
	request.Header.Set("X-GoGIF-Worker-ID", c.hello.WorkerID)
	request.Header.Set("X-GoGIF-Lease-Token", leaseToken)
	request.Header.Set("X-GoGIF-Filename", artifact.Filename)
	response, err := c.http.Do(request)
	if err != nil {
		return scene.Artifact{}, fmt.Errorf("scene worker: upload artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict {
		return scene.Artifact{}, ErrLeaseLost
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxControlResponseBytes+1))
	if err != nil || len(data) > maxControlResponseBytes {
		return scene.Artifact{}, errors.New("scene worker: artifact response exceeds safe bounds")
	}
	if response.StatusCode != http.StatusCreated {
		return scene.Artifact{}, fmt.Errorf("scene worker: artifact upload returned HTTP %d: %s", response.StatusCode, compact(data))
	}
	var stored scene.Artifact
	if err := json.Unmarshal(data, &stored); err != nil {
		return scene.Artifact{}, fmt.Errorf("scene worker: decode artifact response: %w", err)
	}
	return stored, nil
}

func (c *Client) Finish(ctx context.Context, jobID, leaseToken string, result scene.FinishRequest) error {
	payload := map[string]any{"worker_id": c.hello.WorkerID, "lease_token": leaseToken, "result": result}
	status, err := c.json(ctx, http.MethodPost, "/api/v1/scene-jobs/"+url.PathEscape(jobID)+"/finish", payload, nil)
	if status == http.StatusConflict {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("scene worker: finish returned HTTP %d", status)
	}
	return nil
}

func (c *Client) json(ctx context.Context, method, route string, payload any, output any) (int, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.resolve(route), body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, fmt.Errorf("scene worker: control request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxControlResponseBytes+1))
	if err != nil || len(data) > maxControlResponseBytes {
		return response.StatusCode, errors.New("scene worker: control response exceeds safe bounds")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("scene worker: control request returned HTTP %d: %s", response.StatusCode, compact(data))
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return response.StatusCode, fmt.Errorf("scene worker: decode control response: %w", err)
		}
	}
	return response.StatusCode, nil
}

func (c *Client) resolve(route string) string {
	copy := *c.baseURL
	copy.Path = path.Join(c.baseURL.Path, route)
	return copy.String()
}

func compact(data []byte) string {
	value := strings.Join(strings.Fields(string(data)), " ")
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
