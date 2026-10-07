package logfile_test

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/logfile"
)

var cest = time.FixedZone("CEST", 2*3600)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func at(day, hour, minute int) *clock {
	return &clock{time.Date(2026, 10, day, hour, minute, 0, 0, cest)}
}

func fileName(t time.Time) string {
	return "solix-mqtt-bridge-" + t.Format(time.DateOnly) + ".log"
}

func open(t *testing.T, dir string, c *clock) (*logfile.Writer, *bytes.Buffer) {
	t.Helper()
	var reports bytes.Buffer
	w := logfile.New(dir, 7, c.now, slog.New(slog.NewTextHandler(&reports, nil)))
	t.Cleanup(func() { _ = w.Close() })
	return w, &reports
}

func write(t *testing.T, w *logfile.Writer, line string) {
	t.Helper()
	if _, err := w.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
}

func contents(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

func chmod(t *testing.T, dir string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

func TestCreatesTheDirAndAppendsAcrossRestarts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	c := at(7, 12, 0)
	w, _ := open(t, dir, c)
	write(t, w, "one")
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	w, _ = open(t, dir, c)
	write(t, w, "two")

	path := filepath.Join(dir, fileName(c.t))
	if got := contents(t, path); got != "one\ntwo\n" {
		t.Errorf("file = %q, want both lines appended", got)
	}
	for p, want := range map[string]fs.FileMode{dir: 0o700, path: 0o600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode = %v, want %v", p, got, want)
		}
	}
}

func TestRotatesAtLocalMidnight(t *testing.T) {
	dir := t.TempDir()
	c := at(7, 23, 59)
	w, _ := open(t, dir, c)
	write(t, w, "before")
	c.t = c.t.Add(2 * time.Minute)
	write(t, w, "after")

	if got := contents(t, filepath.Join(dir, "solix-mqtt-bridge-2026-10-07.log")); got != "before\n" {
		t.Errorf("old day = %q, want only the line before midnight", got)
	}
	if got := contents(t, filepath.Join(dir, "solix-mqtt-bridge-2026-10-08.log")); got != "after\n" {
		t.Errorf("new day = %q, want only the line after midnight", got)
	}
}

func TestPrunesOnlyItsOwnFilesOlderThanTheRetention(t *testing.T) {
	dir := t.TempDir()
	c := at(20, 12, 0)
	var want []string
	for back := 10; back >= 1; back-- {
		name := fileName(c.t.AddDate(0, 0, -back))
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if back <= 7 {
			want = append(want, name)
		}
	}
	foreign := []string{"notes.txt", "solix-mqtt-bridge-garbage.log"}
	for _, name := range foreign {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w, _ := open(t, dir, c)
	write(t, w, "today")

	want = append(want, fileName(c.t))
	want = append(want, foreign...)
	slices.Sort(want)
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Errorf("after open: files = %v, want today, 7 full days and the foreign files: %v", got, want)
	}

	c.t = c.t.AddDate(0, 0, 1)
	write(t, w, "tomorrow")
	if slices.Contains(names(t, dir), fileName(c.t.AddDate(0, 0, -8))) {
		t.Error("after rotation: the file now 8 days old is still there")
	}
}

func TestUnwritableDirIsReportedOnceAndRecovers(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	chmod(t, dir, 0o500)
	c := at(7, 12, 0)
	w, reports := open(t, dir, c)
	for range 3 {
		if _, err := w.Write([]byte("lost\n")); err == nil {
			t.Fatal("write into a read-only dir: err = nil, want an error")
		}
	}
	chmod(t, dir, 0o700)
	write(t, w, "kept")

	if got := contents(t, filepath.Join(dir, fileName(c.t))); got != "kept\n" {
		t.Errorf("file = %q, want only the line after recovery", got)
	}
	assertReportedOnce(t, reports.String())
}

func TestRotationFailureIsReportedOnceAndRecovers(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	c := at(7, 23, 59)
	w, reports := open(t, dir, c)
	write(t, w, "day one")
	chmod(t, dir, 0o500)
	c.t = c.t.Add(2 * time.Minute)
	for range 2 {
		if _, err := w.Write([]byte("lost\n")); err == nil {
			t.Fatal("rotation into a read-only dir: err = nil, want an error")
		}
	}
	chmod(t, dir, 0o700)
	write(t, w, "day two")

	if got := contents(t, filepath.Join(dir, fileName(c.t))); got != "day two\n" {
		t.Errorf("new day = %q, want only the line after recovery", got)
	}
	assertReportedOnce(t, reports.String())
}

func assertReportedOnce(t *testing.T, reports string) {
	t.Helper()
	if n := strings.Count(reports, "log file unavailable"); n != 1 {
		t.Errorf("failure reported %d times, want once:\n%s", n, reports)
	}
	if n := strings.Count(reports, "log file writable again"); n != 1 {
		t.Errorf("recovery reported %d times, want once:\n%s", n, reports)
	}
}
