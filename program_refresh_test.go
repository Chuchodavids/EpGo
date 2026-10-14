package main

import (
	"sort"
	"strings"
	"testing"
)

func TestGetRequiredProgramIDs(t *testing.T) {
	c := cache{
		Schedule: map[string][]EPGoCache{
			"1": {
				{ProgramID: "EP01", Md5: "same"},
				{ProgramID: "EP02", Md5: "new"},
				{ProgramID: "EP03", Md5: "any"},
			},
			"2": {
				{ProgramID: "EP01", Md5: "same"},
				{ProgramID: "EP04", Md5: "any"},
			},
		},
		Program: map[string]EPGoCache{
			"EP01": {Md5: "same"},
			"EP02": {Md5: "old"},
			"EP04": {},
		},
	}

	ids := c.GetRequiredProgramIDs()
	sort.Strings(ids)
	// EP01 unchanged, EP02 changed, EP03 not cached, EP04 cached without md5.
	if got := strings.Join(ids, ","); got != "EP02,EP03,EP04" {
		t.Fatalf("got %s", got)
	}
}

func TestGetRequiredProgramIDsChecksEveryAiring(t *testing.T) {
	c := cache{
		Schedule: map[string][]EPGoCache{
			"1": {
				{ProgramID: "EP01", Md5: "old"},
				{ProgramID: "EP01", Md5: "new"},
				{ProgramID: "EP01", Md5: "new"},
			},
		},
		Program: map[string]EPGoCache{"EP01": {Md5: "old"}},
	}

	// The first airing still has the cached md5, a later one the changed md5.
	if got := strings.Join(c.GetRequiredProgramIDs(), ","); got != "EP01" {
		t.Fatalf("got %q", got)
	}
}
