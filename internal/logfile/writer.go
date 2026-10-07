// Package logfile writes log lines to one file per local calendar day and deletes the files that have aged past a
// retention period. A file problem never stops the caller: it is reported once, and every later write retries.
package logfile

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	namePrefix = "solix-mqtt-bridge-"
	nameSuffix = ".log"
)

// Writer is an io.Writer that appends to the current day's file in a directory. It is safe for concurrent use.
type Writer struct {
	dir           string
	retentionDays int
	now           func() time.Time
	reports       *slog.Logger

	mu      sync.Mutex
	file    *os.File
	day     string
	failing bool
}

// New returns a Writer for dir that keeps today's file plus retentionDays full days before it. Nothing is opened
// until the first write. Failures and recoveries go to reports, which must not write to this Writer.
func New(dir string, retentionDays int, now func() time.Time, reports *slog.Logger) *Writer {
	return &Writer{dir: dir, retentionDays: retentionDays, now: now, reports: reports}
}

// Write appends p to the file named after the current local date. It opens that file, and prunes old ones, on the
// first write, when the date changes, and after a failure.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.write(p)
	w.track(err)
	return n, err
}

// Close closes the current file; a later Write opens it again.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeFile()
}

func (w *Writer) write(p []byte) (int, error) {
	day := w.now().Format(time.DateOnly)
	if w.file == nil || day != w.day {
		if err := w.open(day); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

func (w *Writer) open(day string) error {
	if err := w.closeFile(); err != nil {
		w.reports.Warn("closing the previous log file failed", "dir", w.dir, "err", err)
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(w.dir, namePrefix+day+nameSuffix)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // the operator's LOG_DIR, a name of our own
	if err != nil {
		return err
	}
	w.file, w.day = f, day
	if err := w.prune(day); err != nil {
		w.reports.Warn("pruning old log files failed", "dir", w.dir, "err", err)
	}
	return nil
}

func (w *Writer) closeFile() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *Writer) prune(today string) error {
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return err
	}
	cutoff := day.AddDate(0, 0, -w.retentionDays)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if fileDay, ok := dayOf(e.Name()); ok && fileDay.Before(cutoff) {
			errs = append(errs, os.Remove(filepath.Join(w.dir, e.Name())))
		}
	}
	return errors.Join(errs...)
}

// Only names this package writes parse, so other files in the directory are never deleted.
func dayOf(name string) (time.Time, bool) {
	date, ok := strings.CutPrefix(name, namePrefix)
	if !ok {
		return time.Time{}, false
	}
	if date, ok = strings.CutSuffix(date, nameSuffix); !ok {
		return time.Time{}, false
	}
	day, err := time.Parse(time.DateOnly, date)
	return day, err == nil
}

// Only the edges are reported, so a full disk logs one line instead of one per poll.
func (w *Writer) track(err error) {
	switch {
	case err != nil && !w.failing:
		w.failing = true
		w.reports.Error("log file unavailable; retrying on later writes", "dir", w.dir, "err", err)
	case err == nil && w.failing:
		w.failing = false
		w.reports.Info("log file writable again", "dir", w.dir)
	}
}
