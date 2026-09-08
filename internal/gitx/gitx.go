package gitx

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func CurrentBranch(path string) string {
	r, err := openRepo(path)
	if err != nil {
		return "?"
	}
	ref, err := r.Head()
	if err != nil {
		return "?"
	}
	if ref.Name().IsBranch() {
		return ref.Name().Short()
	}
	return "?"
}

func remoteTrackingName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasSuffix(ref, "/HEAD") {
		return ""
	}
	_, name, ok := strings.Cut(ref, "/")
	if !ok || name == "" || name == "HEAD" {
		return ""
	}
	return name
}

// Branches returns local branch names plus unique remote branch short names.
func Branches(path string) []string {
	r, err := openRepo(path)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	var result []string

	iter, err := r.Branches()
	if err == nil {
		_ = iter.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name().Short()
			if _, ok := seen[name]; ok {
				return nil
			}
			seen[name] = struct{}{}
			result = append(result, name)
			return nil
		})
	}

	refIter, err := r.References()
	if err == nil {
		_ = refIter.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name().String()
			if !strings.HasPrefix(name, "refs/remotes/") {
				return nil
			}
			short := strings.TrimPrefix(name, "refs/remotes/")
			n := remoteTrackingName(short)
			if n == "" {
				return nil
			}
			if _, ok := seen[n]; ok {
				return nil
			}
			seen[n] = struct{}{}
			result = append(result, n)
			return nil
		})
	}

	return result
}

func Fetch(path string) (string, error) {
	r, err := openRepo(path)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	url := remoteURL(r, "origin")
	err = r.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RemoteURL:  url,
		Auth:       defaultAuth(url),
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return "Already up to date.", nil
		}
		werr := wrapErr("fetch", err)
		return errString(werr), werr
	}
	return "Fetched origin", nil
}

func Pull(path string) (string, error) {
	r, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}
	head, err := r.Head()
	if err != nil {
		return errString(err), err
	}
	refName := head.Name()
	if !refName.IsBranch() {
		return "", fmt.Errorf("detached HEAD; checkout a branch before pull")
	}

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	url := remoteURL(r, "origin")
	err = w.PullContext(ctx, &git.PullOptions{
		RemoteName:    "origin",
		RemoteURL:     url,
		Auth:          defaultAuth(url),
		ReferenceName: refName, // required: go-git defaults to remote HEAD otherwise
		SingleBranch:  true,
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return "Already up to date.", nil
		}
		werr := wrapErr("pull", err)
		return errString(werr), werr
	}
	return "Fast-forward", nil
}

func AbortMerge(path string) (string, error) {
	return ResetHard(path)
}

func AbortRebase(path string) (string, error) {
	return ResetHard(path)
}

// AbortIntegration aborts an in-progress merge or rebase via hard reset.
func AbortIntegration(path string) (string, error) {
	return ResetHard(path)
}

// ResetHard discards tracked local modifications (destructive).
func ResetHard(path string) (string, error) {
	_, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}
	err = w.Reset(&git.ResetOptions{Mode: git.HardReset})
	if err != nil {
		return errString(err), err
	}
	if err := cleanUntracked(path, w); err != nil {
		return errString(err), err
	}
	return "Reset hard + cleaned untracked", nil
}

func Switch(path, branch string) (string, error) {
	return checkoutBranch(path, branch, false)
}

// SwitchForce switches branch discarding local modifications.
func SwitchForce(path, branch string) (string, error) {
	return checkoutBranch(path, branch, true)
}

func checkoutBranch(path, branch string, force bool) (string, error) {
	r, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}

	localRef := branchRefName(branch)
	_, err = r.Reference(localRef, true)
	if err != nil {
		remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
		ref, rerr := r.Reference(remoteRef, true)
		if rerr != nil {
			werr := wrapErr("switch", fmt.Errorf("branch %s not found locally or on origin", branch))
			return errString(werr), werr
		}
		err = r.CreateBranch(&config.Branch{
			Name:   branch,
			Remote: "origin",
			Merge:  localRef,
		})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return errString(err), err
		}
		newRef := plumbing.NewHashReference(localRef, ref.Hash())
		if err := r.Storer.SetReference(newRef); err != nil {
			return errString(err), err
		}
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: localRef,
		Force:  force,
	})
	if err != nil {
		werr := wrapErr("switch", err)
		return errString(werr), werr
	}
	return "Switched to " + branch, nil
}

// SwitchBlockedByChanges reports whether git refused a switch due to local changes.
func SwitchBlockedByChanges(out string) bool {
	return Classify(OpSwitch, out) == KindSwitchDirty
}

// Sync describes how local HEAD relates to its upstream.
type Sync struct {
	HasUpstream bool
	Ahead       int
	Behind      int
	Dirty       bool
	Summary     string
}

func (s Sync) String() string {
	if s.Summary != "" {
		return s.Summary
	}
	if !s.HasUpstream {
		return "no upstream"
	}
	parts := []string{}
	if s.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", s.Behind))
	}
	if s.Ahead > 0 && s.Behind > 0 {
		parts = append([]string{"diverged"}, parts...)
	}
	if s.Dirty {
		parts = append(parts, "dirty")
	}
	if len(parts) == 0 {
		return "synced"
	}
	return strings.Join(parts, " ")
}

// UpstreamSync returns ahead/behind vs upstream. Does not fetch.
func UpstreamSync(path string) Sync {
	s := Sync{}
	s.Dirty = IsDirty(path)

	r, err := openRepo(path)
	if err != nil {
		s.Summary = "no upstream"
		if s.Dirty {
			s.Summary = "no upstream · dirty"
		}
		return s
	}

	head, err := r.Head()
	if err != nil || !head.Name().IsBranch() {
		s.Summary = "no upstream"
		if s.Dirty {
			s.Summary = "no upstream · dirty"
		}
		return s
	}

	branchName := head.Name().Short()
	cfg, err := r.Config()
	var mergeRef plumbing.ReferenceName
	var remoteName string
	if err == nil && cfg != nil {
		if b, ok := cfg.Branches[branchName]; ok && b != nil {
			remoteName = b.Remote
			mergeRef = b.Merge
		}
	}
	if remoteName == "" {
		remoteName = "origin"
	}
	if mergeRef == "" {
		mergeRef = head.Name()
	}

	remoteBranch := mergeRef.Short()
	trackRef := plumbing.NewRemoteReferenceName(remoteName, remoteBranch)
	up, err := r.Reference(trackRef, true)
	if err != nil {
		s.HasUpstream = false
		s.Summary = "no upstream"
		if s.Dirty {
			s.Summary = "no upstream · dirty"
		}
		return s
	}

	s.HasUpstream = true
	localHash := head.Hash()
	remoteHash := up.Hash()

	ahead, _ := countCommitsBetween(r, remoteHash, localHash)
	behind, _ := countCommitsBetween(r, localHash, remoteHash)
	s.Ahead = ahead
	s.Behind = behind
	s.Summary = s.String()
	return s
}

// FetchAndSync fetches origin then returns upstream sync state.
func FetchAndSync(path string) (Sync, string, error) {
	out, err := Fetch(path)
	sync := UpstreamSync(path)
	return sync, out, err
}

// FileChange is one path from porcelain-like status.
type FileChange struct {
	Path   string
	XY     string
	Staged bool
	Work   bool
}

func (f FileChange) Label() string {
	return fmt.Sprintf("%s  %s", f.XY, f.Path)
}

func statusCode(s git.StatusCode) byte {
	switch s {
	case git.Unmodified:
		return ' '
	case git.Modified:
		return 'M'
	case git.Added:
		return 'A'
	case git.Deleted:
		return 'D'
	case git.Renamed:
		return 'R'
	case git.Copied:
		return 'C'
	case git.UpdatedButUnmerged:
		return 'U'
	case git.Untracked:
		return '?'
	default:
		return ' '
	}
}

// StatusPorcelain maps go-git worktree status to FileChange list.
func StatusPorcelain(path string) ([]FileChange, error) {
	_, w, err := openWorktree(path)
	if err != nil {
		return nil, err
	}
	st, err := w.Status()
	if err != nil {
		return nil, err
	}
	var files []FileChange
	for file, fs := range st {
		xy := string([]byte{statusCode(fs.Staging), statusCode(fs.Worktree)})
		if fs.Staging == git.Untracked || fs.Worktree == git.Untracked {
			xy = "??"
		}
		staged := fs.Staging != git.Unmodified && fs.Staging != git.Untracked
		work := fs.Worktree != git.Unmodified || fs.Staging == git.Untracked
		if !staged && !work {
			continue
		}
		files = append(files, FileChange{
			Path:   file,
			XY:     xy,
			Staged: staged,
			Work:   work,
		})
	}
	return files, nil
}

func IsDirty(path string) bool {
	files, err := StatusPorcelain(path)
	return err == nil && len(files) > 0
}

func Add(path string, paths ...string) (string, error) {
	_, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		st, err := w.Status()
		if err != nil {
			return "", err
		}
		for file := range st {
			if _, err := w.Add(file); err != nil {
				return errString(err), err
			}
		}
		return "Added all", nil
	}
	for _, p := range paths {
		if _, err := w.Add(p); err != nil {
			return errString(err), err
		}
	}
	return "Added", nil
}

func Unstage(path string, paths ...string) (string, error) {
	r, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		head, err := r.Head()
		if err != nil {
			return errString(err), err
		}
		err = w.Reset(&git.ResetOptions{Mode: git.MixedReset, Commit: head.Hash()})
		if err != nil {
			return errString(err), err
		}
		return "Unstaged all", nil
	}
	err = w.Restore(&git.RestoreOptions{Staged: true, Files: paths})
	if err != nil {
		return errString(err), err
	}
	return "Unstaged", nil
}

// Discard restores worktree files (and removes untracked if listed).
func Discard(path string, paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths to discard")
	}
	_, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}
	st, _ := w.Status()
	var tracked []string
	for _, p := range paths {
		fs, ok := st[p]
		if !ok || fs.Staging == git.Untracked || fs.Worktree == git.Untracked {
			_ = os.RemoveAll(filepath.Join(path, p))
			continue
		}
		tracked = append(tracked, p)
	}
	if len(tracked) > 0 {
		err = w.Reset(&git.ResetOptions{Mode: git.HardReset, Files: tracked})
		if err != nil {
			return errString(err), err
		}
	}
	return "Discarded", nil
}

func Stash(path, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return runGit(path, defaultTimeout, "stash", "push", "-u")
	}
	return runGit(path, defaultTimeout, "stash", "push", "-u", "-m", message)
}

func Commit(path, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("empty commit message")
	}
	r, w, err := openWorktree(path)
	if err != nil {
		return "", err
	}
	st, err := w.Status()
	if err != nil {
		return "", err
	}
	hasStaged := false
	for _, fs := range st {
		if fs.Staging != git.Unmodified && fs.Staging != git.Untracked {
			hasStaged = true
			break
		}
	}
	if !hasStaged {
		werr := fmt.Errorf("nothing to commit, working tree clean")
		return errString(werr), werr
	}
	hash, err := w.Commit(message, &git.CommitOptions{
		Author: signatureFromConfig(r),
	})
	if err != nil {
		werr := wrapErr("commit", err)
		return errString(werr), werr
	}
	return "Committed " + hash.String()[:7], nil
}

// CommitAll stages everything then commits.
func CommitAll(path, message string) (string, error) {
	if _, err := Add(path); err != nil {
		return "", err
	}
	return Commit(path, message)
}

type LogEntry struct {
	Hash    string
	Subject string
}

func Log(path string, n int) ([]LogEntry, error) {
	if n <= 0 {
		n = 30
	}
	r, err := openRepo(path)
	if err != nil {
		return nil, err
	}
	iter, err := r.Log(&git.LogOptions{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var entries []LogEntry
	for len(entries) < n {
		c, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return entries, err
		}
		sub := strings.TrimSpace(c.Message)
		if i := strings.IndexByte(sub, '\n'); i >= 0 {
			sub = sub[:i]
		}
		entries = append(entries, LogEntry{
			Hash:    c.Hash.String()[:7],
			Subject: sub,
		})
	}
	return entries, nil
}

// Push pushes HEAD to origin.
func Push(path string) (string, error) {
	return pushRepo(path, false)
}

// ForcePush force-pushes the current branch.
func ForcePush(path string) (string, error) {
	return pushRepo(path, true)
}

func pushRepo(path string, force bool) (string, error) {
	r, err := openRepo(path)
	if err != nil {
		return "", err
	}
	head, err := r.Head()
	if err != nil {
		return errString(err), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	url := remoteURL(r, "origin")
	refSpec := config.RefSpec(fmt.Sprintf("%s:refs/heads/%s", head.Name().String(), head.Name().Short()))
	err = r.PushContext(ctx, &git.PushOptions{
		RemoteName: "origin",
		RemoteURL:  url,
		Auth:       defaultAuth(url),
		RefSpecs:   []config.RefSpec{refSpec},
		Force:      force,
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return "Everything up-to-date", nil
		}
		werr := wrapErr("push", err)
		return errString(werr), werr
	}
	if force {
		return "Force pushed", nil
	}
	return "Pushed", nil
}

// NeedsPush reports whether there are local commits not on upstream.
func NeedsPush(path string) bool {
	sync := UpstreamSync(path)
	if !sync.HasUpstream {
		r, err := openRepo(path)
		if err != nil {
			return false
		}
		_, err = r.Head()
		return err == nil
	}
	return sync.Ahead > 0
}

// Diverged reports ahead and behind both positive.
func (s Sync) Diverged() bool {
	return s.HasUpstream && s.Ahead > 0 && s.Behind > 0
}

// SummarizePull turns pull result text into a short status line.
func SummarizePull(out string) string {
	lower := strings.ToLower(out)
	trimmed := strings.TrimSpace(out)

	switch {
	case trimmed == "":
		return "Pulled"
	case strings.Contains(lower, "already up to date"),
		strings.Contains(lower, "is up to date"),
		strings.Contains(lower, "already up-to-date"):
		return "Already up to date"
	case strings.Contains(lower, "fast-forward"):
		if n := countChangedFiles(out); n > 0 {
			return fmt.Sprintf("Fast-forward · %s", fileCountLabel(n))
		}
		return "Fast-forward"
	case strings.Contains(lower, "merge made by"),
		strings.Contains(lower, "merge made using"),
		strings.Contains(lower, "merged"):
		if n := countChangedFiles(out); n > 0 {
			return fmt.Sprintf("Merged · %s", fileCountLabel(n))
		}
		return "Merged"
	case strings.Contains(lower, "updating "):
		if n := countChangedFiles(out); n > 0 {
			return fmt.Sprintf("Updated · %s", fileCountLabel(n))
		}
		return "Updated"
	}

	if n := countChangedFiles(out); n > 0 {
		return fmt.Sprintf("Updated · %s", fileCountLabel(n))
	}

	first := FirstLine(out)
	if len(first) > 40 {
		first = first[:37] + "…"
	}
	if first == "" {
		return "Pulled"
	}
	return first
}

func fileCountLabel(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func countChangedFiles(out string) int {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if !strings.Contains(line, "file") || !strings.Contains(line, "changed") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			n, err := strconv.Atoi(fields[0])
			if err == nil {
				return n
			}
		}
	}
	return 0
}
