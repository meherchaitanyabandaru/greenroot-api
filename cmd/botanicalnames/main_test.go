package main

import (
	"bytes"
	"strings"
	"testing"
)

const wfoSample = "taxonID\tscientificNameID\tscientificName\ttaxonRank\tscientificNameAuthorship\tfamily\tgenus\ttaxonomicStatus\tacceptedNameUsageID\ttaxonRemarks\n" +
	"wfo-0000557668\t\t\"Azadirachta indica\"\tspecies\tA.Juss.\tMeliaceae\tAzadirachta\tAccepted\t\tx\n" +
	"wfo-0001008355\t\t\"Malus × domestica\"\tspecies\t\"(Suckow) Borkh.\"\tRosaceae\tMalus\tAccepted\t\tbad \xff byte\n" +
	"wfo-0000734376\t\t\"Sansevieria trifasciata\"\tspecies\tPrain\tAsparagaceae\tSansevieria\tSynonym\twfo-0001424422\t\n"

func TestClean_KeepsColumnsByNameAndMapsStatus(t *testing.T) {
	var out bytes.Buffer
	n, err := clean(strings.NewReader(wfoSample), &out)
	if err != nil || n != 3 {
		t.Fatalf("clean: n=%d err=%v", n, err)
	}
	want := "wfo_id,scientific_name,authorship,taxon_rank,family,genus,status,accepted_wfo_id\n" +
		"wfo-0000557668,Azadirachta indica,A.Juss.,species,Meliaceae,Azadirachta,ACCEPTED,\n" +
		"wfo-0001008355,Malus × domestica,(Suckow) Borkh.,species,Rosaceae,Malus,ACCEPTED,\n" +
		"wfo-0000734376,Sansevieria trifasciata,Prain,species,Asparagaceae,Sansevieria,SYNONYM,wfo-0001424422\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestClean_RejectsBadRows(t *testing.T) {
	head := "taxonID\tscientificName\ttaxonRank\tscientificNameAuthorship\tfamily\tgenus\ttaxonomicStatus\tacceptedNameUsageID\n"
	for name, row := range map[string]string{
		"unknown status":     "wfo-1\tA b\tspecies\t\t\t\tDoubtful\t\n",
		"synonym no target":  "wfo-1\tA b\tspecies\t\t\t\tSynonym\t\n",
		"accepted w/ target": "wfo-1\tA b\tspecies\t\t\t\tAccepted\twfo-2\n",
		"missing name":       "wfo-1\t\tspecies\t\t\t\tAccepted\t\n",
		"duplicate id":       "wfo-1\tA b\tspecies\t\t\t\tAccepted\t\nwfo-1\tA c\tspecies\t\t\t\tAccepted\t\n",
	} {
		if _, err := clean(strings.NewReader(head+row), &bytes.Buffer{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := clean(strings.NewReader("a\tb\n1\t2\n"), &bytes.Buffer{}); err == nil {
		t.Error("non-WFO file accepted")
	}
}
