package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Archive interface {
	Next() (*File, error)
	Close() error
}

type File struct {
	io.Reader
	Name string
	Size int64
}

type tarArchive struct {
	tr     *tar.Reader
	closer io.Closer
}

func (a *tarArchive) Next() (*File, error) {
	for {
		hdr, err := a.tr.Next()
		if err != nil {
			return nil, err
		}

		// We only care about files
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}

		return &File{
			Reader: a.tr,
			Name:   hdr.Name,
			Size:   hdr.Size,
		}, nil
	}
}

func (a *tarArchive) Close() error {
	return a.closer.Close()
}

func detectContentType(r io.Reader) (string, io.Reader, error) {
	var buf [512]byte
	n, err := io.ReadFull(r, buf[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", nil, err
	}

	ct := http.DetectContentType(buf[:n])

	// Return a new reader which merges in the read bytes
	return ct, io.MultiReader(bytes.NewReader(buf[:n]), r), nil
}

func openReader(u string) (io.ReadCloser, error) {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		resp, err := http.Get(u)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			if err != nil {
				return nil, err
			}
			return nil, &url.Error{
				Op:  "Get",
				URL: u,
				Err: fmt.Errorf("%s: %s", resp.Status, string(b)),
			}
		}
		return resp.Body, nil
	} else if u == "-" {
		return ioutil.NopCloser(os.Stdin), nil
	}

	return os.Open(u)
}

// openArchive opens the tar at the URL or filepath u. Also supported is tgz
// files over http.
func openArchive(u string) (ar Archive, err error) {
	readCloser, err := openReader(u)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = readCloser.Close()
		}
	}()

	ct, r, err := detectContentType(readCloser)
	if err != nil {
		return nil, err
	}
	if ct == "application/x-gzip" {
		r, err = gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
	}

	return &tarArchive{
		tr:     tar.NewReader(r),
		closer: readCloser,
	}, nil
}
