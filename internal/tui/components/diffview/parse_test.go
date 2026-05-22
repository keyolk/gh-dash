package diffview

import (
	"testing"
)

func TestParseUnified_minimal(t *testing.T) {
	input := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo
-
+import "fmt"
+var x = fmt.Sprint(1)
 // end
`
	files, err := ParseUnified(input)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Path() != "foo.go" {
		t.Fatalf("path = %q", f.Path())
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("want 1 hunk, got %d", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 {
		t.Fatalf("bad hunk header: %+v", h)
	}
	kinds := make([]LineKind, len(h.Lines))
	for i, ln := range h.Lines {
		kinds[i] = ln.Kind
	}
	want := []LineKind{LineContext, LineDel, LineAdd, LineAdd, LineContext}
	for i, k := range kinds {
		if k != want[i] {
			t.Fatalf("line %d kind = %d, want %d", i, k, want[i])
		}
	}
}

func TestBuildPending_single(t *testing.T) {
	refs := []CodeRef{
		{Path: "foo.go", Kind: LineAdd, New: 5},
	}
	pc := buildPending(refs, "lgtm")
	if pc.Side != "RIGHT" || pc.Line != 5 || pc.StartLine != 0 {
		t.Fatalf("bad single-line: %+v", pc)
	}
}

func TestBuildPending_multiAdd(t *testing.T) {
	refs := []CodeRef{
		{Path: "foo.go", Kind: LineAdd, New: 5},
		{Path: "foo.go", Kind: LineAdd, New: 6},
		{Path: "foo.go", Kind: LineAdd, New: 7},
	}
	pc := buildPending(refs, "nope")
	if pc.Side != "RIGHT" || pc.StartLine != 5 || pc.Line != 7 {
		t.Fatalf("bad multi: %+v", pc)
	}
}

func TestBuildPending_deletedLeft(t *testing.T) {
	refs := []CodeRef{
		{Path: "foo.go", Kind: LineDel, Old: 12},
	}
	pc := buildPending(refs, "why")
	if pc.Side != "LEFT" || pc.Line != 12 {
		t.Fatalf("bad LEFT: %+v", pc)
	}
}
