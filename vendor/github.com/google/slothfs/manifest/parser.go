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

package manifest

import (
	"encoding/xml"
	"io/ioutil"
	"sort"
	"strings"
)

func (p *Project) parse() {
	for _, s := range strings.Split(p.GroupsString, ",") {
		if s == "" {
			continue
		}
		if p.Groups == nil {
			p.Groups = map[string]bool{}
		}
		p.Groups[s] = true
	}
}

func (p *Project) prepare() {
	var keys []string
	for k, v := range p.Groups {
		if v {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)
	p.GroupsString = strings.Join(keys, ",")
}

// Parse parses the given XML data.
func Parse(contents []byte) (*Manifest, error) {
	var m Manifest
	if err := xml.Unmarshal(contents, &m); err != nil {
		return nil, err
	}

	for i := range m.Project {
		m.Project[i].parse()
	}
	return &m, nil
}

// MarshalXML serializes the receiver to XML.
func (m *Manifest) MarshalXML() ([]byte, error) {
	for i := range m.Project {
		m.Project[i].prepare()
	}

	content, err := xml.MarshalIndent(m, "", " ")
	if err != nil {
		return nil, err
	}
	return content, nil
}

// ParseFile reads and parses an XML file
func ParseFile(name string) (*Manifest, error) {
	content, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return Parse(content)
}

func (mf *Manifest) ProjectRevision(p *Project) string {
	if p.Revision != "" {
		return p.Revision
	}

	return mf.Default.Revision
}

// Filter removes all notdefault projects from a manifest.
func (mf *Manifest) Filter() {
	filtered := *mf
	filtered.Project = nil
	for _, p := range mf.Project {
		if p.Groups["notdefault"] {
			continue
		}
		filtered.Project = append(filtered.Project, p)
	}
	*mf = filtered
}
