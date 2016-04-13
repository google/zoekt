"Zoekt, en gij zult spinazie eten" - Jan Eertink

  ("seek, and ye shall eat spinach" - My primary school teacher)

This is a fast text search engine, intended for use with source code.

INSTRUCTIONS
============

Indexing:

    go build github.com/hanwen/zoekt/cmd/index
    ./index .

Searching

    go build github.com/hanwen/zoekt/cmd/search
    ./search "some string"


BACKGROUND
==========

This uses ngrams (n=3) for searching data, and builds an index
containing the offset of each ngram's occurrence within a file.

This means that if we look for "the quick brown fox", we can look for just the
trigrams "the" and "fox", and check that they are found at the right distance
apart.

Regular expressions can be handled (but aren't yet) by extracting
normal strings from the regular expressions, and running the full
regular expression match on candidates that this yields. For example,

  (Path|PathFragment).*=.*/usr/local

would be transformed in

  (AND (OR "Path" "PathFragment") "/usr/local")

and any documents thus found would be searched for the regular
expression.

Compared to indexing 3-grams on a per-file basis, as described
[here](https://swtch.com/~rsc/regexp/regexp4.html), there are some advantages:

* for each pattern, we only have to intersect just two posting-lists:
  one for the beginning, and one for the end.

* we can select any pair of trigrams from the pattern for which the
  number of matches is minimal. For example, we could search for "qui"
  rather than "the".

There are some downsides compared to trigrams:

* The index is large. Emprically, it is about 3x the corpus size, composed of 2x
  (offsets), and 1x (original content). However, since we have to look at just a
  limited number of ngrams, we don't have to keep the index in memory.

Compared to [suffix
arrays](https://blog.nelhage.com/2015/02/regular-expression-search-with-suffix-arrays/),
there are the following advantages:

* The index construction is straightforward, and can easily be made
  incremental.

* It uses a less memory.

* All the matches are returned in document order. This makes it
  straightforward to process compound boolean queries with AND and OR.

Downsides compared to suffix array:

* there is no way to transform regular expressions into index ranges into
  the suffix array.



ACKNOWLEDGEMENTS
================

Thanks to Alexander Neubeck for coming up with this idea.


DISCLAIMER
==========

This is not an official Google product
