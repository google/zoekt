package build

import (
	"flag"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFlags(t *testing.T) {
	cases := []struct {
		args []string
		want Options
	}{{
		// Defaults
		args: []string{},
		want: Options{},
	}, {
		args: []string{"-index", "/tmp"},
		want: Options{
			IndexDir: "/tmp",
		},
	}, {
		// single large file pattern
		args: []string{"-large_file", "*.md"},
		want: Options{
			LargeFiles: []string{"*.md"},
		},
	}, {
		// multiple large file pattern
		args: []string{"-large_file", "*.md", "-large_file", "*.yaml"},
		want: Options{
			LargeFiles: []string{"*.md", "*.yaml"},
		},
	}}

	for _, c := range cases {
		c.want.SetDefaults()
		// depends on $PATH setting.
		c.want.CTags = ""

		got := Options{}
		fs := flag.NewFlagSet("", flag.ContinueOnError)
		got.Flags(fs)
		if err := fs.Parse(c.args); err != nil {
			t.Errorf("failed to parse args %v: %v", c.args, err)
		} else if !cmp.Equal(got, c.want) {
			t.Errorf("mismatch for %v (-want +got):\n%s", c.args, cmp.Diff(c.want, got))
		}
	}
}

func TestIgnoreSizeMax(t *testing.T) {
	cases := []struct {
		opts Options
		path string
		want bool
	}{
		{
			Options{},
			"/foo",
			false,
		},
		{
			Options{LargeFiles: []string{"/*.lock"}},
			"/foo.lock",
			true,
		},
		{
			Options{LargeFiles: []string{"/*.lock"}},
			"/bar/foo.lock",
			false,
		},
		{
			Options{LargeFiles: []string{"**/*.lock"}},
			"/bar/foo.lock",
			true,
		},
		{
			Options{LargeFiles: []string{"**/*.lock"}},
			"/bar/baz/foo.lock",
			true,
		},
		{
			Options{LargeFiles: []string{"/baz/**/*.lock"}},
			"/bar/baz/foo.lock",
			false,
		},
		{
			Options{LargeFiles: []string{"/baz/**/*.lock"}},
			"/baz/a/b/c/d/foo.lock",
			true,
		},
		{
			Options{LargeFiles: []string{"**.lock"}},
			"/baz/a/b/c/d/foo.lock",
			false,
		},
	}

	for _, c := range cases {
		got := c.opts.IgnoreSizeMax(c.path)
		if got != c.want {
			t.Errorf("mismatch for %v %#v (-want +got): %v %v\n",
				c.opts.LargeFiles, c.path, c.want, got)
		}
	}
}
