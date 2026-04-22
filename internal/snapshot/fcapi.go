package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// fcClient talks to the Firecracker HTTP API over a Unix domain socket.
type fcClient struct {
	socketPath string
	http       *http.Client
}

func newFCClient(socketPath string) *fcClient {
	return &fcClient{
		socketPath: socketPath,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
	}
}

// pauseVM sends PATCH /vm with state=Paused.
func (c *fcClient) pauseVM(ctx context.Context) error {
	return c.patchVM(ctx, "Paused")
}

// resumeVM sends PATCH /vm with state=Resumed.
func (c *fcClient) resumeVM(ctx context.Context) error {
	return c.patchVM(ctx, "Resumed")
}

func (c *fcClient) patchVM(ctx context.Context, state string) error {
	body := map[string]string{"state": state}
	return c.request(ctx, "PATCH", "/vm", body)
}

// createSnapshot tells Firecracker to write snapshot artefacts.
// memFilePath and snapshotPath are paths relative to the jailer root.
func (c *fcClient) createSnapshot(ctx context.Context, memFilePath, snapshotPath string) error {
	body := map[string]interface{}{
		"snapshot_type": "Full",
		"snapshot_path": snapshotPath,
		"mem_file_path": memFilePath,
	}
	return c.request(ctx, "PUT", "/snapshot/create", body)
}

// loadSnapshot tells Firecracker to restore from snapshot artefacts.
func (c *fcClient) loadSnapshot(ctx context.Context, memFilePath, snapshotPath string, resumeVM bool) error {
	body := map[string]interface{}{
		"snapshot_path":    snapshotPath,
		"mem_backend": map[string]interface{}{
			"backend_type":  "File",
			"backend_path":  memFilePath,
		},
		"enable_diff_snapshots": false,
		"resume_vm":             resumeVM,
	}
	return c.request(ctx, "PUT", "/snapshot/load", body)
}

func (c *fcClient) request(ctx context.Context, method, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s %s: %w", method, path, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s → %d: %s", method, path, resp.StatusCode, string(errBody))
	}
	return nil
}
