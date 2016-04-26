package zoekt

import (
	"log"
	"regexp/syntax"
)

var _ = log.Println

func lowerRegexp(r *syntax.Regexp) *syntax.Regexp {
	newRE := *r
	switch r.Op {
	case syntax.OpLiteral, syntax.OpCharClass:
		for i, r := range newRE.Rune {
			if r >= 'A' && r <= 'Z' {
				newRE.Rune[i] = r + 'a' - 'A'
			}
		}
	default:
		for i, s := range newRE.Sub {
			newRE.Sub[i] = lowerRegexp(s)
		}
	}

	return &newRE
}

func regexpToQuery(r *syntax.Regexp) Query {
	q := regexpToQueryRecursive(r)
	q = simplify(q)
	return q
}

// regexpToQuery tries to distill a substring search query that
// matches a superset of the regexp.
func regexpToQueryRecursive(r *syntax.Regexp) Query {
	// TODO - we could perhaps transform Begin/EndText in '\n'?
	// TODO - we could perhaps transform CharClass in (OrQuery )
	// if there are just a few runes, and part of a OpConcat?
	switch r.Op {
	case syntax.OpLiteral:
		s := string(r.Rune)
		if len(s) >= ngramSize {
			return &SubstringQuery{Pattern: s}
		}
	case syntax.OpCapture:
		return regexpToQuery(r.Sub[0])

	case syntax.OpPlus:
		return regexpToQuery(r.Sub[0])

	case syntax.OpRepeat:
		if r.Min >= 1 {
			return regexpToQuery(r.Sub[0])
		}

	case syntax.OpConcat, syntax.OpAlternate:
		var qs []Query
		for _, sr := range r.Sub {
			if sq := regexpToQuery(sr); sq != nil {
				qs = append(qs, sq)
			}
		}
		if r.Op == syntax.OpConcat {
			return &AndQuery{qs}
		}
		return &OrQuery{qs}
	}
	return &TrueQuery{}
}
