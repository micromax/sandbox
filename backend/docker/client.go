package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/micromax/sandbox"
)

const (
	apiVersion   = "v1.45"
	sandboxLabel = "micromax.sandbox"
)

// Client is a minimal, direct client for the Docker Engine REST API.
type Client struct {
	http       *http.Client
	socketPath string
}

// NewClient probes candidate sockets/named pipes and creates a connected Docker API client.
func NewClient(ctx context.Context, customSocket string) (*Client, error) {
	candidates := defaultSocketCandidates()
	if customSocket != "" {
		candidates = []string{customSocket}
	}

	var lastErr error
	for _, cand := range candidates {
		c := newClientForSocket(cand)
		if err := c.Ping(ctx); err == nil {
			return c, nil
		} else {
			lastErr = err
		}
	}

	return nil, fmt.Errorf("%w: docker daemon is not reachable (check if Docker is running): %v",
		sandbox.ErrBackendUnavailable, lastErr)
}

func newClientForSocket(socket string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if strings.HasPrefix(socket, "tcp://") {
				u, err := url.Parse(socket)
				if err != nil {
					return nil, err
				}
				var d net.Dialer
				return d.DialContext(ctx, "tcp", u.Host)
			}
			if strings.HasPrefix(socket, "unix://") {
				return dialContext(ctx, "unix", strings.TrimPrefix(socket, "unix://"))
			}
			if strings.HasPrefix(socket, "npipe://") {
				return dialContext(ctx, "npipe", socket)
			}
			return dialContext(ctx, "default", socket)
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	return &Client{
		http: &http.Client{
			Transport: transport,
			Timeout:   0, // per-request timeouts handled via context
		},
		socketPath: socket,
	}
}

// Ping checks if the Docker engine responds to health checks.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from ping", resp.StatusCode)
	}
	return nil
}

// ImageInspect reports whether an image exists locally.
func (c *Client) ImageInspect(ctx context.Context, image string) (bool, error) {
	u := fmt.Sprintf("http://docker/%s/images/%s/json", apiVersion, url.PathEscape(image))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("inspect image returned status %d", resp.StatusCode)
}

// ImagePull pulls an image by reference.
func (c *Client) ImagePull(ctx context.Context, image string) error {
	u := fmt.Sprintf("http://docker/%s/images/create?fromImage=%s", apiVersion, url.QueryEscape(image))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull failed with status %d: %s", resp.StatusCode, string(body))
	}
	// Drain the stream until pull completes
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ContainerCreateRequest holds configuration for container creation.
type ContainerCreateRequest struct {
	Image      string
	Cmd        []string
	WorkingDir string
	Env        []string
	Labels     map[string]string

	// Hardening parameters
	NetworkMode    string
	ReadonlyRootfs bool
	CapDrop        []string
	SecurityOpt    []string
	Memory         int64
	MemorySwap     int64
	NanoCPUs       int64
	PidsLimit      int64
	Tmpfs          map[string]string
	Runtime        string
}

// ContainerCreate spawns a new container configured with our security profile.
func (c *Client) ContainerCreate(ctx context.Context, name string, cfg ContainerCreateRequest) (string, error) {
	type hostConfig struct {
		NetworkMode    string            `json:"NetworkMode"`
		ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
		CapDrop        []string          `json:"CapDrop"`
		SecurityOpt    []string          `json:"SecurityOpt"`
		Memory         int64             `json:"Memory"`
		MemorySwap     int64             `json:"MemorySwap"`
		NanoCPUs       int64             `json:"NanoCPUs,omitempty"`
		PidsLimit      *int64            `json:"PidsLimit,omitempty"`
		Tmpfs          map[string]string `json:"Tmpfs,omitempty"`
		Runtime        string            `json:"Runtime,omitempty"`
		AutoRemove     bool              `json:"AutoRemove"`
	}

	type createBody struct {
		Image           string            `json:"Image"`
		Cmd             []string          `json:"Cmd"`
		WorkingDir      string            `json:"WorkingDir"`
		Env             []string          `json:"Env,omitempty"`
		Labels          map[string]string `json:"Labels,omitempty"`
		NetworkDisabled bool              `json:"NetworkDisabled"`
		HostConfig      hostConfig        `json:"HostConfig"`
	}

	hc := hostConfig{
		NetworkMode:    cfg.NetworkMode,
		ReadonlyRootfs: cfg.ReadonlyRootfs,
		CapDrop:        cfg.CapDrop,
		SecurityOpt:    cfg.SecurityOpt,
		Memory:         cfg.Memory,
		MemorySwap:     cfg.MemorySwap,
		NanoCPUs:       cfg.NanoCPUs,
		Tmpfs:          cfg.Tmpfs,
		Runtime:        cfg.Runtime,
		AutoRemove:     false,
	}
	if cfg.PidsLimit > 0 {
		hc.PidsLimit = &cfg.PidsLimit
	}

	body := createBody{
		Image:           cfg.Image,
		Cmd:             cfg.Cmd,
		WorkingDir:      cfg.WorkingDir,
		Env:             cfg.Env,
		Labels:          cfg.Labels,
		NetworkDisabled: cfg.NetworkMode == "none",
		HostConfig:      hc,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	u := fmt.Sprintf("http://docker/%s/containers/create?name=%s", apiVersion, url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create container returned %d: %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", err
	}
	return res.ID, nil
}

// UploadArchive unpacks a tar archive into the container at targetPath.
func (c *Client) UploadArchive(ctx context.Context, containerID, targetPath string, tarContent []byte) error {
	u := fmt.Sprintf("http://docker/%s/containers/%s/archive?path=%s",
		apiVersion, url.PathEscape(containerID), url.QueryEscape(targetPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(tarContent))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-tar")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload archive returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// DownloadArchive fetches a tar archive of targetPath from inside the container.
func (c *Client) DownloadArchive(ctx context.Context, containerID, targetPath string) (io.ReadCloser, error) {
	u := fmt.Sprintf("http://docker/%s/containers/%s/archive?path=%s",
		apiVersion, url.PathEscape(containerID), url.QueryEscape(targetPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, nil // directory does not exist or empty
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("download archive returned %d: %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

// ContainerStart starts the container.
func (c *Client) ContainerStart(ctx context.Context, containerID string) error {
	u := fmt.Sprintf("http://docker/%s/containers/%s/start", apiVersion, url.PathEscape(containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("start container returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ContainerLogs streams stdout and stderr from the container, demuxing Docker multiplexed frames.
func (c *Client) ContainerLogs(ctx context.Context, containerID string, stdout, stderr io.Writer, maxOutput uint64, cancel context.CancelFunc) error {
	u := fmt.Sprintf("http://docker/%s/containers/%s/logs?stdout=1&stderr=1", apiVersion, url.PathEscape(containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("container logs returned %d: %s", resp.StatusCode, string(body))
	}

	return demuxDockerStream(resp.Body, stdout, stderr, maxOutput, cancel)
}

// ContainerWait blocks until the container finishes execution and returns its exit status code.
func (c *Client) ContainerWait(ctx context.Context, containerID string) (int, error) {
	u := fmt.Sprintf("http://docker/%s/containers/%s/wait?condition=not-running", apiVersion, url.PathEscape(containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return -1, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	var res struct {
		StatusCode int `json:"StatusCode"`
		Error      *struct {
			Message string `json:"Message"`
		} `json:"Error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return -1, err
	}
	if res.Error != nil && res.Error.Message != "" {
		return res.StatusCode, errors.New(res.Error.Message)
	}
	return res.StatusCode, nil
}

// ContainerInspectInfo holds container metadata.
type ContainerInspectInfo struct {
	ID    string
	State struct {
		Status     string
		Running    bool
		OOMKilled  bool
		ExitCode   int
		FinishedAt string
	}
}

// ContainerInspect retrieves container state.
func (c *Client) ContainerInspect(ctx context.Context, containerID string) (*ContainerInspectInfo, error) {
	u := fmt.Sprintf("http://docker/%s/containers/%s/json", apiVersion, url.PathEscape(containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inspect container returned %d", resp.StatusCode)
	}

	var info ContainerInspectInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ContainerKill terminates a running container.
func (c *Client) ContainerKill(ctx context.Context, containerID string) error {
	u := fmt.Sprintf("http://docker/%s/containers/%s/kill", apiVersion, url.PathEscape(containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ContainerRemove deletes a container.
func (c *Client) ContainerRemove(ctx context.Context, containerID string) error {
	u := fmt.Sprintf("http://docker/%s/containers/%s?v=1&force=1", apiVersion, url.PathEscape(containerID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListSandboxes returns IDs of containers created by this library.
func (c *Client) ListSandboxes(ctx context.Context) ([]string, error) {
	filters := `{"label":["` + sandboxLabel + `=1"]}`
	u := fmt.Sprintf("http://docker/%s/containers/json?all=1&filters=%s", apiVersion, url.QueryEscape(filters))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sandboxes returned %d", resp.StatusCode)
	}

	var list []struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	ids := make([]string, len(list))
	for i, item := range list {
		ids[i] = item.ID
	}
	return ids, nil
}

// demuxDockerStream reads the 8-byte framed stdout/stderr multiplexed Docker stream.
func demuxDockerStream(r io.Reader, stdout, stderr io.Writer, maxOutput uint64, cancel context.CancelFunc) error {
	header := make([]byte, 8)
	var totalBytes uint64

	for {
		_, err := io.ReadFull(r, header)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}

		streamType := header[0]
		frameSize := binary.BigEndian.Uint32(header[4:8])

		var target io.Writer
		switch streamType {
		case 1:
			target = stdout
		case 2:
			target = stderr
		default:
			target = io.Discard
		}

		lr := io.LimitReader(r, int64(frameSize))
		n, copyErr := io.Copy(target, lr)
		totalBytes += uint64(n)

		if maxOutput != sandbox.Unlimited && totalBytes > maxOutput {
			cancel()
			return sandbox.ErrOutputLimit
		}

		if copyErr != nil {
			return copyErr
		}
	}
}
