// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package manifest manipulates Manifest files as described at
// https://gerrit.googlesource.com/git-repo/+/master/docs/manifest-format.txt.
package manifest

// Copyfile indicates that a file should be copied in a checkout
type Copyfile struct {
	Src  string `xml:"src,attr"`
	Dest string `xml:"dest,attr"`
}

// Linkfile indicates that a file should be symlinked in a checkout
type Linkfile struct {
	Src  string `xml:"src,attr"`
	Dest string `xml:"dest,attr"`
}

// Project represents a single git repository that should be stitched
// into the checkout.
type Project struct {
	Path         *string         `xml:"path,attr"`
	Name         string          `xml:"name,attr"`
	Remote       string          `xml:"remote,attr,omitempty"`
	Copyfile     []Copyfile      `xml:"copyfile,omitempty"`
	Linkfile     []Linkfile      `xml:"linkfile,omitempty"`
	GroupsString string          `xml:"groups,attr,omitempty"`
	Groups       map[string]bool `xml:"-"`

	Revision   string `xml:"revision,attr,omitempty"`
	DestBranch string `xml:"dest-branch,attr,omitempty"`
	SyncJ      string `xml:"sync-j,attr,omitempty"`
	SyncC      string `xml:"sync-c,attr,omitempty"`
	SyncS      string `xml:"sync-s,attr,omitempty"`

	Upstream   string `xml:"upstream,attr,omitempty"`
	CloneDepth string `xml:"clone-depth,attr,omitempty"`
	ForcePath  string `xml:"force-path,attr,omitempty"`

	// This is not part of the Manifest spec.
	CloneURL string `xml:"clone-url,attr,omitempty"`
}

// GetPath provides the path where to place the repository.
func (p *Project) GetPath() string {
	if p.Path != nil {
		return *p.Path
	}
	return p.Name
}

// Remote describes a host where a set of projects is hosted.
type Remote struct {
	Alias    string `xml:"alias,attr"`
	Name     string `xml:"name,attr"`
	Fetch    string `xml:"fetch,attr"`
	Review   string `xml:"review,attr"`
	Revision string `xml:"revision,attr"`
}

// Default holds default Project settings.
type Default struct {
	Revision   string `xml:"revision,attr"`
	Remote     string `xml:"remote,attr"`
	DestBranch string `xml:"dest-branch,attr"`
	SyncJ      string `xml:"sync-j,attr"`
	SyncC      string `xml:"sync-c,attr"`
	SyncS      string `xml:"sync-s,attr"`
}

// Manifest holds the entire manifest, describing a set of git
// projects to be stitched together
type Manifest struct {
	Default Default   `xml:"default"`
	Remote  []Remote  `xml:"remote"`
	Project []Project `xml:"project"`
}
