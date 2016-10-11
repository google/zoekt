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

package rest

// SearchRequest is the entry point for the /api/search POST endpoint.
type SearchRequest struct {
	Query string

	// A list of OR'd restrictions.
	Restrict []SearchRequestRestriction
}

// A REST search query must provide a restriction.
type SearchRequestRestriction struct {
	Repo     string
	Branches []string

	// TODO - provide way to set number of search results.
}

// SearchResponse is the return type for /api/search endpoint
type SearchResponse struct {
	Files []*SearchResponseFile
	Error *string

	// TODO - provide statistics.
}

// SearchResponseFile holds the matches within a single file.
type SearchResponseFile struct {
	Repo     string
	Branches []string
	FileName string
	Lines    []*SearchResponseLine
}

// SearchResponseLine holds the matches within a single line.
type SearchResponseLine struct {
	LineNumber int
	Line       string
	Matches    []*SearchResponseMatch
}

// SearchResponseMatch is the matching segment of the line.
type SearchResponseMatch struct {
	// Start of match, in (unicode) characters.
	Start int

	// End of match, in (unicode) characters.
	End int
}
