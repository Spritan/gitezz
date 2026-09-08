package gitx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	sshgit "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

const defaultTimeout = 60 * time.Second
const fetchTimeout = 45 * time.Second

func openRepo(path string) (*git.Repository, error) {
	r, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	return r, nil
}

func openWorktree(path string) (*git.Repository, *git.Worktree, error) {
	r, err := openRepo(path)
	if err != nil {
		return nil, nil, err
	}
	w, err := r.Worktree()
	if err != nil {
		return nil, nil, fmt.Errorf("worktree: %w", err)
	}
	return r, w, nil
}

func defaultAuth(remoteURL string) transport.AuthMethod {
	if strings.HasPrefix(remoteURL, "http://") || strings.HasPrefix(remoteURL, "https://") {
		return httpsAuth(remoteURL)
	}
	return sshAuth()
}

func sshAuth() transport.AuthMethod {
	const user = "git"
	var signers []ssh.Signer

	if agentAuth, err := sshgit.NewSSHAgentAuth(user); err == nil && agentAuth.Callback != nil {
		if ss, err := agentAuth.Callback(); err == nil {
			signers = append(signers, ss...)
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa", "id_ed25519_sk", "id_ecdsa_sk"} {
			path := filepath.Join(home, ".ssh", name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			pk, err := sshgit.NewPublicKeysFromFile(user, path, "")
			if err != nil {
				continue // passphrase-protected keys need ssh-agent
			}
			signers = append(signers, pk.Signer)
		}
	}

	if len(signers) == 0 {
		return nil
	}

	auth := &sshgit.PublicKeysCallback{
		User: user,
		Callback: func() ([]ssh.Signer, error) {
			return signers, nil
		},
	}
	auth.HostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // desktop manager; matches prior behavior
	return auth
}

func httpsAuth(remoteURL string) transport.AuthMethod {
	u, err := parseGitHTTPURL(remoteURL)
	if err != nil {
		return &http.BasicAuth{Username: "git", Password: ""}
	}
	user, pass, ok := gitCredential(u.scheme, u.host, u.path)
	if !ok {
		return &http.BasicAuth{Username: "git", Password: ""}
	}
	return &http.BasicAuth{Username: user, Password: pass}
}

type httpURLParts struct {
	scheme, host, path string
}

func parseGitHTTPURL(raw string) (httpURLParts, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		return httpURLParts{}, fmt.Errorf("not http url")
	}
	rest := raw
	scheme, rest, _ := strings.Cut(rest, "://")
	host, path, ok := strings.Cut(rest, "/")
	if !ok {
		host, path = rest, ""
	}
	if i := strings.IndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	return httpURLParts{scheme: scheme, host: host, path: "/" + path}, nil
}

func gitCredential(protocol, host, path string) (user, pass string, ok bool) {
	input := fmt.Sprintf("protocol=%s\nhost=%s\npath=%s\n\n", protocol, host, strings.TrimPrefix(path, "/"))
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch k {
		case "username":
			user = v
		case "password":
			pass = v
		}
	}
	if user == "" && pass == "" {
		return "", "", false
	}
	if user == "" {
		user = "git"
	}
	return user, pass, true
}

func remoteURL(r *git.Repository, name string) string {
	rem, err := r.Remote(name)
	if err != nil || rem == nil {
		return ""
	}
	cfg := rem.Config()
	if cfg == nil || len(cfg.URLs) == 0 {
		return ""
	}
	return applyInsteadOf(r, cfg.URLs[0])
}

// applyInsteadOf mirrors git's url.<base>.insteadOf rewriting (e.g. HTTPS→SSH).
func applyInsteadOf(r *git.Repository, raw string) string {
	bestLen := -1
	best := raw

	consider := func(cfg *config.Config) {
		if cfg == nil || cfg.URLs == nil {
			return
		}
		for base, u := range cfg.URLs {
			if u == nil || u.InsteadOf == "" {
				continue
			}
			if !strings.HasPrefix(raw, u.InsteadOf) || len(u.InsteadOf) <= bestLen {
				continue
			}
			name := u.Name
			if name == "" {
				name = base
			}
			bestLen = len(u.InsteadOf)
			best = name + strings.TrimPrefix(raw, u.InsteadOf)
		}
	}

	if cfg, err := r.Config(); err == nil {
		consider(cfg)
	}
	if cfg, err := config.LoadConfig(config.GlobalScope); err == nil {
		consider(cfg)
	}
	if cfg, err := config.LoadConfig(config.SystemScope); err == nil {
		consider(cfg)
	}
	return best
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case errors.Is(err, git.NoErrAlreadyUpToDate) || strings.Contains(lower, "already up-to-date") || strings.Contains(lower, "already up to date"):
		return nil
	case errors.Is(err, transport.ErrAuthenticationRequired) || strings.Contains(lower, "authentication failed") || strings.Contains(lower, "authentication required"):
		return fmt.Errorf("authentication failed: %w", err)
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "publickey"):
		return fmt.Errorf("permission denied (publickey): %w", err)
	case errors.Is(err, git.ErrNonFastForwardUpdate) || strings.Contains(lower, "non-fast-forward"):
		if op == "pull" {
			return fmt.Errorf("cannot fast-forward: local and remote branches have diverged: %w", err)
		}
		return fmt.Errorf("non-fast-forward: updates were rejected, fetch first: %w", err)
	case strings.Contains(lower, "would be overwritten") || strings.Contains(lower, "uncommitted"):
		return fmt.Errorf("your local changes to the following files would be overwritten by checkout: %w", err)
	case op == "commit" && (strings.Contains(lower, "clean worktree") || strings.Contains(lower, "cannot create empty commit") || strings.Contains(lower, "empty commit")):
		return fmt.Errorf("nothing to commit, working tree clean: %w", err)
	default:
		return err
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// runGit is reserved for stash (go-git has no mature stash API).
func runGit(repoPath string, timeout time.Duration, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	if timeout > 0 {
		// best-effort; stash is local and usually fast
		_ = timeout
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("%s: %w", FirstLine(text), err)
	}
	return text, nil
}

func signatureFromConfig(r *git.Repository) *object.Signature {
	name := "gitezz"
	email := "gitezz@localhost"
	cfg, err := r.Config()
	if err == nil && cfg != nil {
		if cfg.User.Name != "" {
			name = cfg.User.Name
		}
		if cfg.User.Email != "" {
			email = cfg.User.Email
		}
	}
	// Also try global via scoped config
	if name == "gitezz" || email == "gitezz@localhost" {
		if global, err := config.LoadConfig(config.GlobalScope); err == nil && global != nil {
			if name == "gitezz" && global.User.Name != "" {
				name = global.User.Name
			}
			if email == "gitezz@localhost" && global.User.Email != "" {
				email = global.User.Email
			}
		}
	}
	return &object.Signature{Name: name, Email: email, When: time.Now()}
}

func cleanUntracked(repoPath string, w *git.Worktree) error {
	status, err := w.Status()
	if err != nil {
		return err
	}
	for file, st := range status {
		if st.Worktree == git.Untracked {
			p := filepath.Join(repoPath, file)
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func branchRefName(name string) plumbing.ReferenceName {
	if strings.HasPrefix(name, "refs/") {
		return plumbing.ReferenceName(name)
	}
	return plumbing.NewBranchReferenceName(name)
}

func countCommitsBetween(r *git.Repository, from, to plumbing.Hash) (int, error) {
	if from == to {
		return 0, nil
	}
	iter, err := r.Log(&git.LogOptions{From: to})
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	n := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == from {
			return stopline
		}
		n++
		if n > 10000 {
			return stopline
		}
		return nil
	})
	if err != nil && !errors.Is(err, stopline) {
		return n, err
	}
	return n, nil
}

var stopline = errors.New("stop")
