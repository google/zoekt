
CTAGS
=====

Ctags generates indices of symbol definitions in source files. It
started its life as part of the BSD Unix, but there are several more
modern flavors. Zoekt supports both [exuberant
ctags](http://ctags.sourceforge.net/) and
[universal-ctags](https://github.com/universal-ctags).

It is strongly recommended to use Universal Ctags, [version
`db3d9a6`](https://github.com/universal-ctags/ctags/commit/4ff09da9b0a36a9e75c92f4be05d476b35b672cd)
or newer, running on the Linux platform.

From this version on, universal ctags will be called using seccomp,
which guarantees that security problems in ctags cannot escalate to
access to the indexing machine.

Use the following invocation to compile and install universal-ctags:

```
sudo apt install \
  pkg-config autoconf python3-docutils \
  libseccomp-dev libseccomp2 \
  libjansson-dev libjansson4

git clone --depth=1 https://github.com/universal-ctags/ctags.git && cd ctags
./autogen.sh
LDFLAGS=-static ./configure --enable-json --enable-seccomp
make -j4

# create tarball
NAME=ctags-$(date --iso-8601=minutes | tr -d ':' | sed 's|\+.*$||')-$(git show --pretty=format:%h -q)
mkdir ${NAME}
cp ctags ${NAME}/universal-ctags
tar zcf ${NAME}.tar.gz ${NAME}/
```
