// Package github provides a GitHub API client and sync polling for Pebble.
package github

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/LucaWahlen/pebble/server/internal/config"
	"github.com/LucaWahlen/pebble/server/internal/files"
)

// ---------- API types ----------

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha,omitempty"`
	Size int64  `json:"size,omitempty"`
}

type tree struct {
	SHA  string      `json:"sha"`
	Tree []treeEntry `json:"tree"`
}

type blob struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type ref struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type commit struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

// ---------- HTTPDoer for testability ----------

// HTTPDoer is a minimal interface for making HTTP requests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ---------- Client ----------

// Client interacts with the GitHub API.
type Client struct {
	HTTP  HTTPDoer
	Files *files.Service
}

// NewClient creates a GitHub client.
func NewClient(httpClient HTTPDoer, filesSvc *files.Service) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{HTTP: httpClient, Files: filesSvc}
}

func (c *Client) request(method, url, token string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTP.Do(req)
}

func jsonDecode[T any](c *Client, method, url, token string, body any) (T, error) {
	var zero T
	resp, err := c.request(method, url, token, body)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(b))
	}
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, err
	}
	return result, nil
}

// TestConnection validates token, repo, and branch access.
func (c *Client) TestConnection(token, repo, branch string) error {
	type repoInfo struct {
		Permissions struct {
			Push bool `json:"push"`
			Pull bool `json:"pull"`
		} `json:"permissions"`
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s", repo)
	resp, err := c.request("GET", url, token, nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Errorf("repository not found: %s", repo)
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("authentication failed (HTTP %d) – check your token", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(b))
	}

	var info repoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("failed to parse repo info: %w", err)
	}

	branchURL := fmt.Sprintf("https://api.github.com/repos/%s/branches/%s", repo, branch)
	branchResp, err := c.request("GET", branchURL, token, nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer branchResp.Body.Close()
	if branchResp.StatusCode == 404 {
		return fmt.Errorf("branch not found: %s", branch)
	}

	if !info.Permissions.Push {
		return fmt.Errorf("connection OK but token has read-only access. " +
			"Push will not work. Grant Contents: Read and Write permission on your fine-grained token, " +
			"or use a classic token with 'repo' scope")
	}

	return nil
}

// Pull fetches the full tree from the repo and writes files to the config directory.
// Files are downloaded to a temp directory first and only swapped in on success,
// preventing partial config states if the pull fails mid-way.
// Returns (written files, commit SHA, error).
func (c *Client) Pull(conf config.Config) ([]string, string, error) {
	r, err := jsonDecode[ref](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/ref/heads/%s", conf.GitHubRepo, conf.GitHubBranch),
		conf.GitHubToken, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get ref: %w", err)
	}

	cm, err := jsonDecode[commit](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/commits/%s", conf.GitHubRepo, r.Object.SHA),
		conf.GitHubToken, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get commit: %w", err)
	}

	t, err := jsonDecode[tree](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", conf.GitHubRepo, cm.Tree.SHA),
		conf.GitHubToken, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get tree: %w", err)
	}

	// Download everything into a temp directory first
	tmpDir, err := os.MkdirTemp(filepath.Dir(c.Files.RootDir), ".pebble-pull-*")
	if err != nil {
		return nil, "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		// Clean up temp dir if it still exists (swap failed or error path)
		_ = os.RemoveAll(tmpDir)
	}()

	var written []string
	for _, entry := range t.Tree {
		if entry.Type == "tree" {
			_ = os.MkdirAll(filepath.Join(tmpDir, entry.Path), 0755)
			continue
		}
		if entry.Type != "blob" {
			continue
		}
		b, err := jsonDecode[blob](c, "GET",
			fmt.Sprintf("https://api.github.com/repos/%s/git/blobs/%s", conf.GitHubRepo, entry.SHA),
			conf.GitHubToken, nil)
		if err != nil {
			return nil, "", fmt.Errorf("get blob %s: %w", entry.Path, err)
		}
		var content []byte
		if b.Encoding == "base64" {
			content, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(b.Content, "\n", ""))
			if err != nil {
				return nil, "", fmt.Errorf("decode blob %s: %w", entry.Path, err)
			}
		} else {
			content = []byte(b.Content)
		}
		filePath := filepath.Join(tmpDir, entry.Path)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return nil, "", fmt.Errorf("mkdir for %s: %w", entry.Path, err)
		}
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			return nil, "", fmt.Errorf("write %s: %w", entry.Path, err)
		}
		written = append(written, entry.Path)
	}

	// All files downloaded successfully — swap the directories atomically.
	// 1. Move old dir out of the way
	backupDir := c.Files.RootDir + ".bak"
	_ = os.RemoveAll(backupDir)
	if err := os.Rename(c.Files.RootDir, backupDir); err != nil {
		return nil, "", fmt.Errorf("backup old config: %w", err)
	}
	// 2. Move new dir into place
	if err := os.Rename(tmpDir, c.Files.RootDir); err != nil {
		// Rollback: restore the backup
		_ = os.Rename(backupDir, c.Files.RootDir)
		return nil, "", fmt.Errorf("swap config dir: %w", err)
	}
	// 3. Remove backup
	_ = os.RemoveAll(backupDir)

	return written, r.Object.SHA, nil
}

// Push reads local files and pushes changes to the repo as a single atomic commit
// using the Git Trees API. Unchanged files (same SHA) are skipped.
func (c *Client) Push(conf config.Config, source string) (string, error) {
	// 1. Get current HEAD ref and tree
	r, err := jsonDecode[ref](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/ref/heads/%s", conf.GitHubRepo, conf.GitHubBranch),
		conf.GitHubToken, nil)
	if err != nil {
		return "", fmt.Errorf("get ref: %w", err)
	}
	headSHA := r.Object.SHA

	cm, err := jsonDecode[commit](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/commits/%s", conf.GitHubRepo, headSHA),
		conf.GitHubToken, nil)
	if err != nil {
		return "", fmt.Errorf("get commit: %w", err)
	}
	baseTreeSHA := cm.Tree.SHA

	// Get existing remote tree for SHA comparison
	existingTree, _ := jsonDecode[tree](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", conf.GitHubRepo, baseTreeSHA),
		conf.GitHubToken, nil)
	existingSHAs := map[string]string{}
	for _, e := range existingTree.Tree {
		if e.Type == "blob" {
			existingSHAs[e.Path] = e.SHA
		}
	}

	// 2. Build tree entries from local files
	type newTreeEntry struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		SHA     string `json:"sha,omitempty"`
		Content string `json:"content,omitempty"`
	}
	var treeEntries []newTreeEntry
	localFiles := map[string]bool{}
	hasChanges := false

	c.Files.WalkFiles(func(relPath string, content []byte) {
		localFiles[relPath] = true
		localSHA := gitBlobSHA(content)

		if existingSHA, ok := existingSHAs[relPath]; ok && localSHA == existingSHA {
			// File unchanged — include existing SHA (no re-upload)
			treeEntries = append(treeEntries, newTreeEntry{
				Path: relPath,
				Mode: "100644",
				Type: "blob",
				SHA:  existingSHA,
			})
			return
		}

		// File is new or changed — include content inline
		hasChanges = true
		treeEntries = append(treeEntries, newTreeEntry{
			Path:    relPath,
			Mode:    "100644",
			Type:    "blob",
			Content: string(content),
		})
	})

	// Check for deleted files
	for remotePath := range existingSHAs {
		if !localFiles[remotePath] {
			hasChanges = true
			// Omitting the file from the tree = deletion
		}
	}

	if !hasChanges {
		return headSHA, nil
	}

	// 3. Create a new tree (not using base_tree so omitted files are deleted)
	type createTreeReq struct {
		Tree []newTreeEntry `json:"tree"`
	}
	newTree, err := jsonDecode[tree](c, "POST",
		fmt.Sprintf("https://api.github.com/repos/%s/git/trees", conf.GitHubRepo),
		conf.GitHubToken, createTreeReq{Tree: treeEntries})
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	// 4. Create a new commit
	type createCommitReq struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}
	newCommit, err := jsonDecode[commit](c, "POST",
		fmt.Sprintf("https://api.github.com/repos/%s/git/commits", conf.GitHubRepo),
		conf.GitHubToken, createCommitReq{
			Message: fmt.Sprintf("Update config via Pebble (%s)", source),
			Tree:    newTree.SHA,
			Parents: []string{headSHA},
		})
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	// 5. Update the branch ref
	type updateRefReq struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}
	_, err = c.request("PATCH",
		fmt.Sprintf("https://api.github.com/repos/%s/git/refs/heads/%s", conf.GitHubRepo, conf.GitHubBranch),
		conf.GitHubToken, updateRefReq{SHA: newCommit.SHA})
	if err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	return newCommit.SHA, nil
}

// GetHeadSHA returns the current HEAD SHA for the branch.
func (c *Client) GetHeadSHA(conf config.Config) (string, error) {
	r, err := jsonDecode[ref](c, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/git/ref/heads/%s", conf.GitHubRepo, conf.GitHubBranch),
		conf.GitHubToken, nil)
	if err != nil {
		return "", err
	}
	return r.Object.SHA, nil
}


func gitBlobSHA(content []byte) string {
	header := fmt.Sprintf("blob %d\x00", len(content))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}



