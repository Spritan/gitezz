package main

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Spritan/gitezz/internal/gitx"
)

type conflictAction struct {
	Label      string
	Danger     bool
	Primary    bool
	NeedsConfirm string // if set, show confirm before running
	Run        func()
}

type attentionItem struct {
	Repo   Repo
	Kind   gitx.ConflictKind
	Out    string
	Branch string // for switch conflicts
	Op     string
}

var (
	attentionMu   sync.Mutex
	attentionByPath = map[string]attentionItem{}
)

func rememberAttention(item attentionItem) {
	attentionMu.Lock()
	attentionByPath[item.Repo.Path] = item
	attentionMu.Unlock()
}

func clearAttention(path string) {
	attentionMu.Lock()
	delete(attentionByPath, path)
	attentionMu.Unlock()
}

func attentionFor(path string) (attentionItem, bool) {
	attentionMu.Lock()
	defer attentionMu.Unlock()
	item, ok := attentionByPath[path]
	return item, ok
}

func showConflictDialog(title, body string, actions []conflictAction) {
	msg := widget.NewLabel(body)
	msg.Wrapping = fyne.TextWrapWord

	scroll := container.NewVScroll(msg)
	scroll.SetMinSize(fyne.NewSize(500, 150))

	var d dialog.Dialog

	hide := func() {
		if d != nil {
			d.Hide()
		}
	}

	btns := container.NewHBox()
	for _, a := range actions {
		a := a
		b := widget.NewButton(a.Label, func() {
			run := func() {
				hide()
				if a.Run != nil {
					a.Run()
				}
			}
			if a.NeedsConfirm != "" {
				dialog.ShowConfirm(a.Label, a.NeedsConfirm, func(ok bool) {
					if ok {
						run()
					}
				}, appWindow)
				return
			}
			run()
		})
		if a.Danger {
			b.Importance = widget.DangerImportance
		} else if a.Primary {
			b.Importance = widget.HighImportance
		}
		btns.Add(b)
	}

	cancel := widget.NewButton("Cancel", func() {
		hide()
	})
	btns.Add(cancel)

	root := container.NewBorder(nil, container.NewPadded(btns), nil, nil, scroll)
	d = dialog.NewCustomWithoutButtons(title, root, appWindow)
	d.Resize(fyne.NewSize(560, 320))
	d.Show()
}

func handleClassifiedFailure(r Repo, op string, out string, status *widget.Label, elapsed string, branch string, interactive bool) gitx.ConflictKind {
	kind := gitx.Classify(op, out)
	slog.Error("git op failed",
		"repo", r.Name,
		"op", op,
		"kind", string(kind),
		"out", gitx.FirstLine(out),
	)

	label := kind.StatusLabel()
	if elapsed != "" && kind == gitx.KindGeneric {
		label = "Failed in " + elapsed
	}
	imp := widget.DangerImportance
	if kind == gitx.KindSwitchDirty || kind == gitx.KindPullOverwrite || kind == gitx.KindCommitEmpty {
		imp = widget.WarningImportance
	}
	setRowStatus(r.Path, status, label, imp)
	setFooter(fmt.Sprintf("%s: %s", r.Name, gitx.FirstLine(out)))

	if !kind.Actionable() {
		return kind
	}

	item := attentionItem{Repo: r, Kind: kind, Out: out, Branch: branch, Op: op}
	rememberAttention(item)

	if interactive {
		fyne.Do(func() {
			showAttentionDialog(item, status)
		})
	}
	return kind
}

func showAttentionDialog(item attentionItem, status *widget.Label) {
	r := item.Repo
	kind := item.Kind
	out := item.Out
	branch := item.Branch

	title := kind.Title()
	header := fmt.Sprintf("%s · %s", r.Name, r.Branch)
	if branch != "" && kind == gitx.KindSwitchDirty {
		header = fmt.Sprintf("%s: switch to %s blocked", r.Name, branch)
	}
	body := header + "\n\n" + out

	var actions []conflictAction

	switch kind {
	case gitx.KindSwitchDirty:
		actions = []conflictAction{
			{
				Label:  "Discard & switch",
				Danger: true,
				Run: func() {
					setRowStatus(r.Path, status, "Discarding…", widget.WarningImportance)
					go func() {
						start := time.Now()
						o, err := gitx.SwitchForce(r.Path, branch)
						elapsed := formatDuration(time.Since(start))
						if err != nil {
							handleClassifiedFailure(r, gitx.OpSwitch, o, status, elapsed, branch, true)
							fyne.Do(renderRepos)
							return
						}
						clearAttention(r.Path)
						finishSwitchOK(r, branch, status, elapsed)
					}()
				},
			},
			{
				Label:   "Stash & switch",
				Primary: true,
				Run: func() {
					setRowStatus(r.Path, status, "Stashing…", widget.MediumImportance)
					go func() {
						start := time.Now()
						o, err := gitx.Stash(r.Path, fmt.Sprintf("gitezz auto-stash before switch to %s", branch))
						if err != nil {
							elapsed := formatDuration(time.Since(start))
							handleClassifiedFailure(r, gitx.OpStash, o, status, elapsed, "", true)
							fyne.Do(renderRepos)
							return
						}
						o, err = gitx.Switch(r.Path, branch)
						elapsed := formatDuration(time.Since(start))
						if err != nil {
							handleClassifiedFailure(r, gitx.OpSwitch, o, status, elapsed, branch, true)
							fyne.Do(renderRepos)
							return
						}
						clearAttention(r.Path)
						finishSwitchOK(r, branch, status, elapsed)
					}()
				},
			},
			{
				Label: "Open changes",
				Run: func() {
					fyne.Do(renderRepos)
					openDetailPanel(r)
				},
			},
		}
	case gitx.KindPullOverwrite:
		actions = []conflictAction{
			{
				Label:   "Stash & pull",
				Primary: true,
				Run: func() {
					setRowStatus(r.Path, status, "Stashing…", widget.MediumImportance)
					go func() {
						start := time.Now()
						o, err := gitx.Stash(r.Path, "gitezz auto-stash before pull")
						if err != nil {
							elapsed := formatDuration(time.Since(start))
							handleClassifiedFailure(r, gitx.OpStash, o, status, elapsed, "", true)
							return
						}
						clearAttention(r.Path)
						doPull(r, status, start)
					}()
				},
			},
			{
				Label:  "Discard & pull",
				Danger: true,
				NeedsConfirm: "Discard ALL local changes in " + r.Name + ", then pull?",
				Run: func() {
					setRowStatus(r.Path, status, "Discarding…", widget.WarningImportance)
					go func() {
						start := time.Now()
						o, err := gitx.ResetHard(r.Path)
						if err != nil {
							elapsed := formatDuration(time.Since(start))
							handleClassifiedFailure(r, gitx.OpPull, o, status, elapsed, "", true)
							return
						}
						clearAttention(r.Path)
						doPull(r, status, start)
					}()
				},
			},
			{
				Label: "Open changes",
				Run:   func() { openDetailPanel(r) },
			},
		}
	case gitx.KindPullConflict:
		actions = []conflictAction{
			{
				Label:   "Open changes",
				Primary: true,
				Run:     func() { openDetailPanel(r) },
			},
			{
				Label:  "Abort merge/rebase",
				Danger: true,
				NeedsConfirm: "Abort the in-progress merge/rebase in " + r.Name + "?",
				Run: func() {
					go func() {
						o, err := gitx.AbortIntegration(r.Path)
						if err != nil {
							setFooter(fmt.Sprintf("%s: %s", r.Name, gitx.FirstLine(o)))
							return
						}
						clearAttention(r.Path)
						updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
						setRowStatus(r.Path, status, "Aborted", widget.MediumImportance)
						setFooter(r.Name + ": merge/rebase aborted")
						fyne.Do(func() {
							if detailOpen && activeDetailPath == r.Path {
								reloadDetailPanel(r)
							}
						})
					}()
				},
			},
		}
	case gitx.KindPushNonFF:
		actions = []conflictAction{
			{
				Label:   "Pull then push",
				Primary: true,
				Run: func() {
					go func() {
						start := time.Now()
						setRowStatus(r.Path, status, "Pulling…", widget.MediumImportance)
						o, err := gitx.Pull(r.Path)
						if err != nil {
							elapsed := formatDuration(time.Since(start))
							handleClassifiedFailure(r, gitx.OpPull, o, status, elapsed, "", true)
							return
						}
						setRowStatus(r.Path, status, "Pushing…", widget.MediumImportance)
						o, err = gitx.Push(r.Path)
						elapsed := formatDuration(time.Since(start))
						if err != nil {
							handleClassifiedFailure(r, gitx.OpPush, o, status, elapsed, "", true)
							return
						}
						clearAttention(r.Path)
						updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
						setRowStatus(r.Path, status, "Pushed in "+elapsed, widget.SuccessImportance)
						setFooter(fmt.Sprintf("Pushed %s in %s", r.Name, elapsed))
					}()
				},
			},
			{
				Label:        "Force push",
				Danger:       true,
				NeedsConfirm: "Force-push " + r.Name + " with --force-with-lease? This can overwrite remote commits.",
				Run: func() {
					go func() {
						start := time.Now()
						setRowStatus(r.Path, status, "Force pushing…", widget.WarningImportance)
						o, err := gitx.ForcePush(r.Path)
						elapsed := formatDuration(time.Since(start))
						if err != nil {
							handleClassifiedFailure(r, gitx.OpPush, o, status, elapsed, "", true)
							return
						}
						clearAttention(r.Path)
						updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
						setRowStatus(r.Path, status, "Force pushed · "+elapsed, widget.SuccessImportance)
						setFooter(fmt.Sprintf("Force pushed %s in %s", r.Name, elapsed))
					}()
				},
			},
		}
	case gitx.KindPushAuth:
		actions = []conflictAction{
			{
				Label: "Open changes",
				Run:   func() { openDetailPanel(r) },
			},
		}
	case gitx.KindCommitEmpty:
		actions = []conflictAction{
			{
				Label:   "Open changes",
				Primary: true,
				Run:     func() { openDetailPanel(r) },
			},
		}
	}

	showConflictDialog(title, body, actions)
}

func showAttentionSummary(items []attentionItem) {
	if len(items) == 0 {
		return
	}
	if len(items) == 1 {
		fyne.Do(func() {
			showAttentionDialog(items[0], rowStatusFor(items[0].Repo.Path))
		})
		return
	}

	fyne.Do(func() {
		list := container.NewVBox()
		var d dialog.Dialog
		for _, item := range items {
			item := item
			line := fmt.Sprintf("%s — %s", item.Repo.Name, item.Kind.StatusLabel())
			btn := widget.NewButton("Resolve "+item.Repo.Name, func() {
				if d != nil {
					d.Hide()
				}
				showAttentionDialog(item, rowStatusFor(item.Repo.Path))
			})
			list.Add(widget.NewLabel(line))
			list.Add(btn)
		}
		scroll := container.NewVScroll(list)
		scroll.SetMinSize(fyne.NewSize(420, 220))
		info := widget.NewLabel(fmt.Sprintf("%d repositories need attention after the batch operation.", len(items)))
		info.Wrapping = fyne.TextWrapWord
		body := container.NewBorder(info, nil, nil, nil, scroll)
		d = dialog.NewCustom("Needs attention", "Close", body, appWindow)
		d.Resize(fyne.NewSize(480, 340))
		d.Show()
	})
}

func doPull(r Repo, status *widget.Label, start time.Time) {
	setRowStatus(r.Path, status, "Pulling…", widget.MediumImportance)
	out, err := gitx.Pull(r.Path)
	elapsed := formatDuration(time.Since(start))
	if err != nil {
		setRowLog(r.Path, out)
		handleClassifiedFailure(r, gitx.OpPull, out, status, elapsed, "", true)
		return
	}
	finishPullOK(r, status, out, elapsed)
}

func maybeWarnDiverged(r Repo) {
	sync := r.Sync
	if sync.Summary == "" {
		sync = gitx.UpstreamSync(r.Path)
	}
	if sync.Diverged() {
		setFooter(fmt.Sprintf("%s is diverged (↑%d ↓%d) — pull recommended before push", r.Name, sync.Ahead, sync.Behind))
	}
}

func openAttentionIfAny(r Repo) bool {
	item, ok := attentionFor(r.Path)
	if !ok {
		return false
	}
	showAttentionDialog(item, rowStatusFor(r.Path))
	return true
}

// trimBody keeps dialog text from being huge.
func trimBody(s string) string {
	s = strings.TrimSpace(s)
	const max = 2500
	if len(s) > max {
		return s[:max] + "\n…"
	}
	return s
}
