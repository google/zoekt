package codesearch

import (
	"reflect"
	"testing"
)

func TestParseQuery(t *testing.T) {
	type testcase struct {
		in  string
		out Query
		hasErr bool
	}

	for _, c := range []testcase{
		{"abc", &AndQuery{[]Query{&SubstringQuery{Pattern: "abc"}}}, false},
		{"\"abc bcd\"", &AndQuery{[]Query{&SubstringQuery{Pattern: "abc bcd"}}}, false},
		{"abc bcd", &AndQuery{[]Query{
			&SubstringQuery{Pattern: "abc"},
			&SubstringQuery{Pattern: "bcd"},
		}}, false},
		{"abc case:yes", &AndQuery{[]Query{&SubstringQuery{Pattern: "abc", CaseSensitive: true}}}, false},
		{"abc case:auto", &AndQuery{[]Query{&SubstringQuery{Pattern: "abc", CaseSensitive: false}}}, false},
		{"ABC case:auto", &AndQuery{[]Query{&SubstringQuery{Pattern: "ABC", CaseSensitive: true}}}, false},
		{"ABC case:\"auto\"", &AndQuery{[]Query{&SubstringQuery{Pattern: "ABC", CaseSensitive: true}}}, false},

		// errors.
		{"\"abc", nil, true},
		{"\"a\\", nil, true},
		{"case:foo", nil, true},
		{"", nil, true},
	} {
		q, err := Parse(c.in)
		if c.hasErr != (err != nil) {
			t.Errorf("Parse(%s): error %v", c.in, err)
		} else if q != nil {
			if !reflect.DeepEqual(q, c.out) {
				t.Errorf("Parse(%s): got %v want %v", c.in, q, c.out)
			}
		}
	}
}
