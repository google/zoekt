
    "Zoekt, en gij zult spinazie eten" - Jan Eertink

    ("seek, and ye shall eat spinach" - My primary school teacher)

This is a fast text search engine, intended for use with source
code. (Pronunciation: roughly as you would pronounce "zooked" in English)

INSTRUCTIONS
============

Downloading:

    go get github.com/google/zoekt/

Indexing:

    go install github.com/google/zoekt/cmd/zoekt-index
    $GOPATH/bin/zoekt-index .

Searching

    go install github.com/google/zoekt/cmd/zoekt
    $GOPATH/bin/zoekt 'ngram f:READ'

Indexing git repositories (requires libgit2 + git2go):

    go install github.com/google/zoekt/cmd/zoekt-git-index
    $GOPATH/bin/zoekt-git-index -branches master,stable-1.4 -prefix origin/ .

Starting the web interface

    go install github.com/google/zoekt/cmd/zoekt-webserver
    $GOPATH/bin/zoekt-webserver -listen :6070


SEARCH SERVICE
==============

Zoekt comes with a small service management program:

    go install github.com/google/zoekt/cmd/zoekt-server

    cat << EOF > config.json
    [{"GithubUser": "username"},
     {"GitilesURL": "https://gerrit.googlesource.com", Name: "zoekt" }
    ]
    EOF

    $GOPATH/bin/zoekt-server -mirror_config config.json

This will mirror all repos under 'github.com/username' as well as the
'zoekt' repository. It will index the repositories and start the
webserver.

It takes care of fetching and indexing new data, restarting crashed
webservers and cleaning up logfiles


SYMBOL SEARCH
=============

It is recommended to install CTags to improve ranking:

   * [Universal ctags](https://github.com/universal-ctags/ctags) is more up to date, but not commonly packaged for distributions. It must be compiled from source.
   * [Exuberant ctags](http://ctags.sourceforge.net/) is a languishing, but commonly available through Linux distributions. It has several known vulnerabilities.

If you index untrusted code, it is strongly recommended to also
install Bazel's sandbox, to avoid vulnerabilities of ctags opening up
access to the indexing machine. The sandbox can be compiled as follows:

    for f in namespace-sandbox.c namespace-sandbox.c process-tools.c network-tools.c \
       process-tools.h network-tools.h ; do \
      wget https://raw.githubusercontent.com/bazelbuild/bazel/master/src/main/tools/$f \
    done
    gcc -o namespace-sandbox -std=c99 \
       namespace-sandbox.c process-tools.c network-tools.c  -lm
    cp namespace-sandbox /usr/local/bin/




ACKNOWLEDGEMENTS
================

Thanks to Alexander Neubeck for coming up with this idea, and helping me flesh
it out.


DISCLAIMER
==========

This is not an official Google product
