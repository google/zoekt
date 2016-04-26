package zoekt

import (
	"log"
	"reflect"
	"regexp/syntax"
	"strings"
	"testing"
)

var opnames = map[syntax.Op]string{
	syntax.OpNoMatch:        "OpNoMatch",
	syntax.OpEmptyMatch:     "OpEmptyMatch",
	syntax.OpLiteral:        "OpLiteral",
	syntax.OpCharClass:      "OpCharClass",
	syntax.OpAnyCharNotNL:   "OpAnyCharNotNL",
	syntax.OpAnyChar:        "OpAnyChar",
	syntax.OpBeginLine:      "OpBeginLine",
	syntax.OpEndLine:        "OpEndLine",
	syntax.OpBeginText:      "OpBeginText",
	syntax.OpEndText:        "OpEndText",
	syntax.OpWordBoundary:   "OpWordBoundary",
	syntax.OpNoWordBoundary: "OpNoWordBoundary",
	syntax.OpCapture:        "OpCapture",
	syntax.OpStar:           "OpStar",
	syntax.OpPlus:           "OpPlus",
	syntax.OpQuest:          "OpQuest",
	syntax.OpRepeat:         "OpRepeat",
	syntax.OpConcat:         "OpConcat",
	syntax.OpAlternate:      "OpAlternate",
}

func printRegexp(r *syntax.Regexp, lvl int) {
	log.Printf("%s%s ch: %d", strings.Repeat(" ", lvl), opnames[r.Op], len(r.Sub))
	for _, s := range r.Sub {
		printRegexp(s, lvl+1)
	}
}

func TestRegexpParse(t *testing.T) {
	type testcase struct {
		in   string
		want Query
	}

	cases := []testcase{
		{"(foo|)bar", &SubstringQuery{Pattern: "bar"}},
		{"(foo|)", &TrueQuery{}},
		{"(foo|bar)baz.*bla", &AndQuery{[]Query{
			&OrQuery{[]Query{
				&SubstringQuery{Pattern: "foo"},
				&SubstringQuery{Pattern: "bar"},
			}},
			&SubstringQuery{Pattern: "baz"},
			&SubstringQuery{Pattern: "bla"},
		}}},
		{"^[a-z](People)+barrabas$",
			&AndQuery{[]Query{
				&SubstringQuery{Pattern: "People"},
				&SubstringQuery{Pattern: "barrabas"},
			}}},
	}

	for _, c := range cases {
		r, err := syntax.Parse(c.in, 0)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}

		got := regexpToQuery(r)
		if !reflect.DeepEqual(c.want, got) {
			t.Errorf("regexpToQuery(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLowerRegexp(t *testing.T) {
	in := "[a-zA-Z]fooBAR"
	re := mustParseRE(in)
	got := lowerRegexp(re)
	want := "[a-za-z]foobar"
	if got.String() != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
