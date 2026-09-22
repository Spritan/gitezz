package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func runningInFlatpak() bool {
	if os.Getenv("FLATPAK_ID") != "" {
		return true
	}
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

// pickFolderHostDialog asks the host for a folder via flatpak-spawn.
// Prefers kdialog (KDE), then zenity. Returns ("", nil) on cancel.
func pickFolderHostDialog(start string) (string, error) {
	if start == "" {
		start = os.Getenv("HOME")
	}

	type candidate struct {
		name string
		args []string
	}
	candidates := []candidate{
		{"kdialog", []string{"--getexistingdirectory", start}},
		{"zenity", []string{"--file-selection", "--directory", "--title=Select folder of Git repositories", "--filename=" + start + "/"}},
	}

	var errs []string
	for _, c := range candidates {
		args := append([]string{"--host", c.name}, c.args...)
		cmd := exec.Command("flatpak-spawn", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		out := strings.TrimSpace(stdout.String())
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && ee.ExitCode() == 1 && out == "" {
				// User cancelled.
				return "", nil
			}
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			errs = append(errs, c.name+": "+msg)
			continue
		}
		if out == "" {
			return "", nil
		}
		return out, nil
	}
	return "", errors.New("host folder dialog unavailable (" + strings.Join(errs, "; ") + ")")
}
