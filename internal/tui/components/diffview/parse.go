package diffview

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// LineKind classifies a single line inside a hunk.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdd
	LineDel
	// LineNoNewline is the trailing "\ No newline at end of file" marker.
	LineNoNewline
)

// Line is one rendered row inside a hunk.
type Line struct {
	Kind   LineKind
	OldNum int    // 1-based; 0 when not applicable (LineAdd)
	NewNum int    // 1-based; 0 when not applicable (LineDel)
	Text   string // payload without the leading +/-/space marker
}

// Hunk corresponds to a `@@ -a,b +c,d @@` block.
type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Header   string // the trailing text after "@@ ... @@"
	Lines    []Line
}

// File is a single changed file in a unified diff.
type File struct {
	OldPath string
	NewPath string
	Hunks   []Hunk
}

// Path returns the most useful display path for a file.
func (f File) Path() string {
	if f.NewPath != "" && f.NewPath != "/dev/null" {
		return f.NewPath
	}
	return f.OldPath
}

// ParseUnified parses a unified diff (the format `gh pr diff` emits) into
// a slice of File entries. The parser is intentionally forgiving: lines it
// doesn't recognise are silently ignored so noise in the input doesn't kill
// the whole diff. Errors are returned only for malformed hunk headers.
func ParseUnified(input string) ([]File, error) {
	var files []File
	var current *File
	var hunk *Hunk
	var oldLineNum, newLineNum int

	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)

	flushHunk := func() {
		if hunk != nil && current != nil {
			current.Hunks = append(current.Hunks, *hunk)
		}
		hunk = nil
	}

	flushFile := func() {
		flushHunk()
		if current != nil {
			files = append(files, *current)
		}
		current = nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			current = &File{}
		case strings.HasPrefix(line, "--- "):
			if current == nil {
				current = &File{}
			}
			current.OldPath = strings.TrimPrefix(line, "--- ")
			current.OldPath = stripPathPrefix(current.OldPath)
		case strings.HasPrefix(line, "+++ "):
			if current == nil {
				current = &File{}
			}
			current.NewPath = strings.TrimPrefix(line, "+++ ")
			current.NewPath = stripPathPrefix(current.NewPath)
		case strings.HasPrefix(line, "@@ "):
			flushHunk()
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			hunk = &h
			oldLineNum = h.OldStart
			newLineNum = h.NewStart
		case hunk != nil && len(line) > 0 && line[0] == '+':
			hunk.Lines = append(hunk.Lines, Line{
				Kind: LineAdd, NewNum: newLineNum, Text: line[1:],
			})
			newLineNum++
		case hunk != nil && len(line) > 0 && line[0] == '-':
			hunk.Lines = append(hunk.Lines, Line{
				Kind: LineDel, OldNum: oldLineNum, Text: line[1:],
			})
			oldLineNum++
		case hunk != nil && strings.HasPrefix(line, `\ `):
			hunk.Lines = append(hunk.Lines, Line{
				Kind: LineNoNewline, Text: strings.TrimPrefix(line, `\ `),
			})
		case hunk != nil && len(line) > 0 && line[0] == ' ':
			hunk.Lines = append(hunk.Lines, Line{
				Kind: LineContext, OldNum: oldLineNum, NewNum: newLineNum,
				Text: line[1:],
			})
			oldLineNum++
			newLineNum++
		case hunk != nil && line == "":
			// "git diff" can emit zero-prefix blank lines as context.
			hunk.Lines = append(hunk.Lines, Line{
				Kind: LineContext, OldNum: oldLineNum, NewNum: newLineNum,
			})
			oldLineNum++
			newLineNum++
		default:
			// header noise (`index ...`, `new file mode`, etc.) — ignore
		}
	}

	flushFile()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan diff: %w", err)
	}
	return files, nil
}

// stripPathPrefix removes the leading "a/" or "b/" used by git.
func stripPathPrefix(p string) string {
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

func parseHunkHeader(line string) (Hunk, error) {
	// @@ -<oldStart>[,<oldLines>] +<newStart>[,<newLines>] @@[ trailing]
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return Hunk{}, fmt.Errorf("invalid hunk header: %q", line)
	}
	ranges := rest[:end]
	trailing := strings.TrimSpace(strings.TrimPrefix(rest[end:], " @@"))

	parts := strings.Fields(ranges)
	if len(parts) != 2 {
		return Hunk{}, fmt.Errorf("invalid hunk ranges: %q", ranges)
	}
	oldStart, oldLines, err := parseRange(parts[0], '-')
	if err != nil {
		return Hunk{}, err
	}
	newStart, newLines, err := parseRange(parts[1], '+')
	if err != nil {
		return Hunk{}, err
	}
	return Hunk{
		OldStart: oldStart, OldLines: oldLines,
		NewStart: newStart, NewLines: newLines,
		Header: trailing,
	}, nil
}

func parseRange(s string, expectPrefix byte) (start, length int, err error) {
	if len(s) == 0 || s[0] != expectPrefix {
		return 0, 0, fmt.Errorf("hunk range missing %q prefix: %q", expectPrefix, s)
	}
	body := s[1:]
	length = 1
	if comma := strings.IndexByte(body, ','); comma >= 0 {
		start, err = strconv.Atoi(body[:comma])
		if err != nil {
			return 0, 0, fmt.Errorf("hunk range start: %w", err)
		}
		length, err = strconv.Atoi(body[comma+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("hunk range length: %w", err)
		}
	} else {
		start, err = strconv.Atoi(body)
		if err != nil {
			return 0, 0, fmt.Errorf("hunk range start: %w", err)
		}
	}
	return start, length, nil
}
