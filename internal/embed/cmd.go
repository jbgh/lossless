package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// cmdEmbedder shells out to LOSSLESS_EMBED_CMD.
//
// stdin:  {"texts":["..."]}
// stdout: {"vectors":[[...]],"model":"all-MiniLM-L6-v2","dim":384}
//
// The command must be local. We never send claim text off-box from here.
type cmdEmbedder struct {
	line string
	name string
	dim  int
}

func openCmd(line string) (Embedder, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("empty embed command")
	}
	return &cmdEmbedder{line: line, name: DefaultModel, dim: DefaultDim}, nil
}

func (c *cmdEmbedder) Name() string { return c.name }
func (c *cmdEmbedder) Dim() int     { return c.dim }

func (c *cmdEmbedder) Embed(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{"texts": texts})
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("sh", "-c", c.line)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("embed cmd: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var out struct {
		Vectors [][]float32 `json:"vectors"`
		Model   string      `json:"model"`
		Dim     int         `json:"dim"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("embed cmd json: %w", err)
	}
	if len(out.Vectors) != len(texts) {
		return nil, fmt.Errorf("embed cmd: got %d vectors want %d", len(out.Vectors), len(texts))
	}
	if out.Model != "" {
		c.name = out.Model
	}
	if out.Dim > 0 {
		c.dim = out.Dim
	}
	for i := range out.Vectors {
		Normalize(out.Vectors[i])
	}
	return out.Vectors, nil
}

func openModelDir(dir string) (Embedder, error) {
	// In-process MiniLM lands here when we have a pure-Go runtime.
	// A model directory alone is not enough to run inference yet.
	if dir == "" {
		return nil, fmt.Errorf("empty model dir")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no model dir")
	}
	return nil, fmt.Errorf("in-process MiniLM not linked")
}
