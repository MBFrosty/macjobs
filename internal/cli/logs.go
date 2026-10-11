package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
)

const maxChunk = 4 * 1024 * 1024

// Tail reads backwards in bounded chunks rather than loading an entire log.
func Tail(f *os.File, n int) ([]string, int64, error) {
	stat, e := f.Stat()
	if e != nil {
		return nil, 0, e
	}
	end := stat.Size()
	if n == 0 {
		return []string{}, end, nil
	}
	start := end
	b := []byte{}
	for start > 0 && strings.Count(strings.TrimSuffix(string(b), "\n"), "\n") < n {
		size := int64(8192)
		if start < size {
			size = start
		}
		start -= size
		block := make([]byte, size)
		if _, e := f.ReadAt(block, start); e != nil && e != io.EOF {
			return nil, 0, e
		}
		b = append(block, b...)
		if len(b) > maxChunk {
			return nil, 0, errors.New("requested log tail is too large")
		}
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(b) == 0 {
		return []string{}, end, nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, end, nil
}

type logState struct {
	Path    string
	Offset  int64
	Info    os.FileInfo
	Pending string
}

func Logs(ctx context.Context, w io.Writer, item model.Item, n int, follow, jsonOut bool, r redact.Redactor) error {
	states := []logState{}
	emit := func(path, line string) error {
		line = r.Text(line)
		path = r.Text(path)
		if jsonOut {
			return json.NewEncoder(w).Encode(struct {
				SchemaVersion int    `json:"schemaVersion"`
				ID            string `json:"id"`
				Path          string `json:"path"`
				Line          string `json:"line"`
			}{model.SchemaVersion, r.Text(item.ID), path, line})
		}
		_, e := fmt.Fprintln(w, line)
		return e
	}
	for _, path := range item.Logs {
		f, e := os.Open(path)
		if e != nil {
			return errors.New("configured log is unavailable")
		}
		lines, offset, e := Tail(f, n)
		info, se := f.Stat()
		pending := ""
		if follow && offset > 0 {
			last := make([]byte, 1)
			if _, readErr := f.ReadAt(last, offset-1); readErr != nil {
				f.Close()
				return errors.New("cannot read configured log")
			}
			if last[0] != '\n' {
				if len(lines) > 0 {
					pending = lines[len(lines)-1]
					lines = lines[:len(lines)-1]
				} else {
					partial, _, tailErr := Tail(f, 1)
					if tailErr != nil {
						f.Close()
						return errors.New("cannot read incomplete log line")
					}
					if len(partial) > 0 {
						pending = partial[0]
					}
				}
			}
		}
		f.Close()
		if e != nil || se != nil {
			return errors.New("cannot read configured log")
		}
		if !jsonOut {
			if _, e := fmt.Fprintln(w, "==>", r.Text(path), "<=="); e != nil {
				return e
			}
		}
		for _, line := range lines {
			if e := emit(path, line); e != nil {
				return e
			}
		}
		states = append(states, logState{Path: path, Offset: offset, Info: info, Pending: pending})

	}
	if !follow {
		return nil
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			for j := range states {
				s := &states[j]
				f, e := os.Open(s.Path)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return errors.New("cannot follow configured log")
				}
				info, e := f.Stat()
				if e != nil {
					f.Close()
					return errors.New("cannot inspect configured log")
				}
				if !os.SameFile(info, s.Info) || info.Size() < s.Offset {
					s.Offset = 0
					s.Pending = ""
				}
				s.Info = info
				if _, e = f.Seek(s.Offset, io.SeekStart); e != nil {
					f.Close()
					return errors.New("cannot seek configured log")
				}
				b, e := io.ReadAll(io.LimitReader(f, maxChunk))
				f.Close()
				if e != nil {
					return errors.New("cannot follow configured log")
				}
				s.Offset += int64(len(b))
				s.Pending += string(b)
				if len(s.Pending) > maxChunk {
					return errors.New("log line is too large")
				}
				for {
					line, rest, ok := strings.Cut(s.Pending, "\n")
					if !ok {
						break
					}
					s.Pending = rest
					if e := emit(s.Path, line); e != nil {
						return e
					}
				}
			}
		}
	}
}
