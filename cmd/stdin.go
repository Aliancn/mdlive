package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/k1LoW/mo/internal/server"
)

// isStdinRedirected reports whether stdin is redirected (not a terminal).
func isStdinRedirected() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

const maxStdinSize = 10 << 20 // 10MB (same as server upload limit)

// readStdin reads all content from the given reader and returns
// a generated name in the format "stdin-<hash>.md" along with the content.
func readStdin(r io.Reader) (name string, content string, err error) {
	limited := io.LimitReader(r, maxStdinSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", "", fmt.Errorf("failed to read stdin: %w", err)
	}
	if len(data) > maxStdinSize {
		return "", "", fmt.Errorf("stdin content too large (max 10MB)")
	}
	c := string(data)
	return stdinName(c), c, nil
}

// stdinName generates a deterministic name for stdin content
// in the format "stdin-<first 7 hex chars of SHA-256>.md".
func stdinName(content string) string {
	h := sha256.Sum256([]byte(content))
	return "stdin-" + hex.EncodeToString(h[:])[:7] + ".md"
}

type uploadRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// postUploadedFile uploads in-memory content to a running mo server.
func postUploadedFile(client *http.Client, addr, group, name, content string) (deeplinkEntry, error) {
	body, err := json.Marshal(uploadRequest{
		Name:    name,
		Content: content,
	})
	if err != nil {
		return deeplinkEntry{}, err
	}
	resp, err := client.Post(
		fmt.Sprintf("http://%s/_/api/groups/%s/files/upload", addr, url.PathEscape(group)),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return deeplinkEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return deeplinkEntry{}, fmt.Errorf("upload failed: %s", resp.Status)
		}
		errText := strings.TrimSpace(string(errBody))
		if errText != "" {
			return deeplinkEntry{}, fmt.Errorf("upload failed: %s: %s", resp.Status, errText)
		}
		return deeplinkEntry{}, fmt.Errorf("upload failed: %s", resp.Status)
	}
	var entry server.FileEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return deeplinkEntry{}, err
	}
	return deeplinkEntry{
		URL:  buildDeeplink(addr, group, entry.ID),
		Name: entry.Name,
	}, nil
}
