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

	"github.com/mxk/go-flowrate/flowrate"
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
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
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

// openArchive opens the tar at the URL or filepath u. Also supported is tgz
// files over http.
//
// If non-zero, limitMbps is used to limit the download speed of archives to
// the specified amount in megabits per second.
func openArchive(u string, limitMbps int64) (Archive, error) {
	var (
		r      io.Reader
		closer io.Closer
	)

	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		resp, err := http.Get(u)
		if err != nil {
			return nil, err
		}
		body := resp.Body
		if limitMbps != 0 {
			const megabit = 1000 * 1000
			body = flowrate.NewReader(body, (limitMbps*megabit)/8)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, err := ioutil.ReadAll(io.LimitReader(body, 1024))
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			return nil, &url.Error{
				Op:  "Get",
				URL: u,
				Err: fmt.Errorf("%s: %s", resp.Status, string(b)),
			}
		}
		closer = resp.Body
		r = body
	} else if u == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(u)
		if err != nil {
			return nil, err
		}
		closer = f
		r = f
	}

	ct, r, err := detectContentType(r)
	if err != nil {
		return nil, err
	}
	if ct == "application/x-gzip" {
		r, err = gzip.NewReader(r)
		if err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, err
		}
	}

	return &tarArchive{
		tr:     tar.NewReader(r),
		closer: closer,
	}, nil
}
