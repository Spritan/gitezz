package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Spritan/gitezz/internal/gitx"
)

func pushAll() {
	showPushDialog(visibleRepos(), "visible repositories")
}

func pushSelected() {
	visible := visibleRepos()
	var chosen []Repo
	for _, r := range visible {
		if isSelected(r.Path) {
			chosen = append(chosen, r)
		}
	}
	showPushDialog(chosen, "selected repositories")
}

func showCommitDialog(r Repo) {
	msg := widget.NewMultiLineEntry()
	msg.SetPlaceHolder("Commit message")
	msg.SetMinRowsVisible(4)

	stageAll := widget.NewCheck("Stage all changes", nil)
	stageAll.SetChecked(true)

	info := widget.NewLabel(r.Name + " · " + r.Branch)
	info.Importance = widget.MediumImportance

	body := container.NewVBox(info, msg, stageAll)

	d := dialog.NewCustomConfirm(
		"Commit",
		"Commit",
		"Cancel",
		body,
		func(ok bool) {
			if !ok {
				return
			}
			text := strings.TrimSpace(msg.Text)
			if text == "" {
				setFooter("Commit message required")
				return
			}

			status := rowStatusFor(r.Path)
			setRowStatus(r.Path, status, "Committing…", widget.MediumImportance)

			go func() {
				start := time.Now()
				var out string
				var err error
				if stageAll.Checked {
					out, err = gitx.CommitAll(r.Path, text)
				} else {
					out, err = gitx.Commit(r.Path, text)
				}
				elapsed := formatDuration(time.Since(start))
				if err != nil {
					kind := handleClassifiedFailure(r, gitx.OpCommit, trimBody(out), status, elapsed, "", false)
					if kind == gitx.KindCommitEmpty {
						fyne.Do(func() {
							showConflictDialog("Nothing to commit", r.Name+"\n\n"+out, []conflictAction{
								{
									Label:   "Stage all & commit",
									Primary: true,
									Run: func() {
										go func() {
											setRowStatus(r.Path, status, "Committing…", widget.MediumImportance)
											o, e := gitx.CommitAll(r.Path, text)
											el := formatDuration(time.Since(start))
											if e != nil {
												handleClassifiedFailure(r, gitx.OpCommit, trimBody(o), status, el, "", true)
												return
											}
											clearAttention(r.Path)
											updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
											setRowStatus(r.Path, status, "Committed in "+el, widget.SuccessImportance)
											setFooter(fmt.Sprintf("%s: committed", r.Name))
										}()
									},
								},
								{
									Label: "Open changes",
									Run:   func() { openDetailPanel(r) },
								},
							})
						})
					} else if kind.Actionable() {
						fyne.Do(func() {
							showAttentionDialog(attentionItem{Repo: r, Kind: kind, Out: trimBody(out), Op: gitx.OpCommit}, status)
						})
					}
					return
				}
				clearAttention(r.Path)
				updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
				setRowStatus(r.Path, status, "Committed in "+elapsed, widget.SuccessImportance)
				setFooter(fmt.Sprintf("%s: committed", r.Name))
				if detailOpen && activeDetailPath == r.Path {
					fyne.Do(func() { reloadDetailPanel(r) })
				}
			}()
		},
		appWindow,
	)
	d.Resize(fyne.NewSize(440, 280))
	d.Show()
}

func showPushDialog(targets []Repo, label string) {
	if len(targets) == 0 {
		statusLabel.SetText("No repositories selected")
		return
	}

	for _, r := range targets {
		maybeWarnDiverged(r)
	}

	var dirty []Repo
	for _, r := range targets {
		if gitx.IsDirty(r.Path) {
			dirty = append(dirty, r)
		}
	}

	if len(dirty) == 0 {
		runPushRepos(targets, nil, label)
		return
	}

	modeSame := widget.NewRadioGroup([]string{
		"Same commit message for all dirty repos",
		"One message per dirty repo",
	}, nil)
	modeSame.SetSelected("Same commit message for all dirty repos")
	modeSame.Required = true

	sharedMsg := widget.NewMultiLineEntry()
	sharedMsg.SetPlaceHolder("Commit message for all dirty repos")
	sharedMsg.SetMinRowsVisible(3)

	perRepoEntries := make([]*widget.Entry, len(dirty))
	perRepoBox := container.NewVBox()
	for i, r := range dirty {
		e := widget.NewMultiLineEntry()
		e.SetPlaceHolder("Message for " + r.Name)
		e.SetMinRowsVisible(2)
		perRepoEntries[i] = e
		perRepoBox.Add(widget.NewLabel(r.Name))
		perRepoBox.Add(e)
	}

	sharedWrap := container.NewVBox(sharedMsg)
	perWrap := container.NewVBox(perRepoBox)
	perWrap.Hide()

	modeSame.OnChanged = func(v string) {
		if strings.HasPrefix(v, "Same") {
			sharedWrap.Show()
			perWrap.Hide()
		} else {
			sharedWrap.Hide()
			perWrap.Show()
		}
	}

	info := widget.NewLabel(fmt.Sprintf(
		"%d dirty of %d repos need a commit before push.",
		len(dirty), len(targets),
	))
	info.Wrapping = fyne.TextWrapWord

	body := container.NewVBox(
		info,
		modeSame,
		sharedWrap,
		container.NewVScroll(perWrap),
	)

	d := dialog.NewCustomConfirm(
		"Push — commit message",
		"Push",
		"Cancel",
		body,
		func(ok bool) {
			if !ok {
				return
			}

			msgs := map[string]string{}
			if strings.HasPrefix(modeSame.Selected, "Same") {
				msg := strings.TrimSpace(sharedMsg.Text)
				if msg == "" {
					setFooter("Commit message required for dirty repos")
					return
				}
				for _, r := range dirty {
					msgs[r.Path] = msg
				}
			} else {
				for i, r := range dirty {
					msg := strings.TrimSpace(perRepoEntries[i].Text)
					if msg == "" {
						continue
					}
					msgs[r.Path] = msg
				}
			}

			runPushRepos(targets, msgs, label)
		},
		appWindow,
	)
	d.Resize(fyne.NewSize(520, 420))
	d.Show()
}

func runPushRepos(targets []Repo, commitMsgs map[string]string, label string) {
	for _, r := range targets {
		if status := rowStatusFor(r.Path); status != nil {
			setRowStatus(r.Path, status, "Waiting…", widget.MediumImportance)
		}
	}
	statusLabel.SetText("Pushing " + label + "…")

	interactive := len(targets) == 1

	go func() {
		start := time.Now()
		failed := 0
		skipped := 0
		var attention []attentionItem

		for _, r := range targets {
			status := rowStatusFor(r.Path)
			repoStart := time.Now()

			dirty := gitx.IsDirty(r.Path)
			if dirty {
				msg, ok := commitMsgs[r.Path]
				if !ok || strings.TrimSpace(msg) == "" {
					skipped++
					setRowStatus(r.Path, status, "Skipped (dirty)", widget.WarningImportance)
					setFooter(fmt.Sprintf("Skipped dirty %s (no commit message)", r.Name))
					continue
				}

				setRowStatus(r.Path, status, "Committing…", widget.MediumImportance)
				out, err := gitx.CommitAll(r.Path, msg)
				if err != nil {
					failed++
					elapsed := formatDuration(time.Since(repoStart))
					kind := handleClassifiedFailure(r, gitx.OpCommit, trimBody(out), status, elapsed, "", interactive)
					if kind.Actionable() && !interactive {
						attention = append(attention, attentionItem{
							Repo: r, Kind: kind, Out: trimBody(out), Op: gitx.OpCommit,
						})
					}
					continue
				}
			}

			if !gitx.NeedsPush(r.Path) && !dirty {
				setRowStatus(r.Path, status, "Up to date", widget.SuccessImportance)
				continue
			}

			if !gitx.NeedsPush(r.Path) {
				sync := gitx.UpstreamSync(r.Path)
				if sync.HasUpstream && sync.Ahead == 0 {
					elapsed := formatDuration(time.Since(repoStart))
					updateRepoSync(r.Path, sync)
					setRowStatus(r.Path, status, "Up to date · "+elapsed, widget.SuccessImportance)
					continue
				}
			}

			setRowStatus(r.Path, status, "Pushing…", widget.MediumImportance)
			out, err := gitx.Push(r.Path)
			elapsed := formatDuration(time.Since(repoStart))
			if err != nil {
				failed++
				kind := handleClassifiedFailure(r, gitx.OpPush, trimBody(out), status, elapsed, "", interactive)
				if kind.Actionable() && !interactive {
					attention = append(attention, attentionItem{
						Repo: r, Kind: kind, Out: trimBody(out), Op: gitx.OpPush,
					})
				}
				continue
			}

			clearAttention(r.Path)
			updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
			setRowStatus(r.Path, status, "Pushed in "+elapsed, widget.SuccessImportance)
			setFooter(fmt.Sprintf("Pushed %s in %s", r.Name, elapsed))
		}

		total := formatDuration(time.Since(start))
		summary := fmt.Sprintf("Push finished in %s (%d repos)", total, len(targets))
		if failed > 0 || skipped > 0 {
			summary = fmt.Sprintf("Push finished in %s · %d failed · %d skipped", total, failed, skipped)
		}
		setFooter(summary)
		showAttentionSummary(attention)
	}()
}
