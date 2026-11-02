package service

import (
	"encoding/json"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
)

type growthCoverage struct {
	Fragments []rc.Fragment `json:"fragments,omitempty"`
	Sources   []rc.Source   `json:"sources"`
}

func BuildGrowthBatch(page rc.SourcePage, metadata json.RawMessage) (GrowthInferenceInput, rc.Cursor, error) {
	in := GrowthInferenceInput{Mode: "analyze", Fragments: page.Fragments, Metadata: metadata}
	raw, err := json.Marshal(page.Fragments)
	if err != nil || len(raw) > 48*1024 {
		return in, rc.Cursor{}, fmt.Errorf("source batch exceeds budget")
	}
	raw, err = json.Marshal(in)
	if err != nil || len(raw) > 79*1024 {
		return in, rc.Cursor{}, fmt.Errorf("growth input exceeds budget")
	}
	return in, page.Next, nil
}

func growthSourceIndex(artifacts []rc.AnalysisArtifact) []rc.Source {
	refs := map[string]bool{}
	for _, a := range artifacts {
		for _, c := range a.Claims {
			for _, r := range c.SourceRefs {
				refs[r] = true
			}
		}
	}
	owned := map[string]rc.Source{}
	var out []rc.Source
	for _, a := range artifacts {
		var coverage growthCoverage
		_ = json.Unmarshal(a.Coverage, &coverage)
		for _, s := range coverage.Sources {
			if refs[s.ID] {
				if _, ok := owned[s.ID]; !ok {
					s.Text = ""
					owned[s.ID] = s
					out = append(out, s)
				}
			}
		}
	}
	return out
}
func growthArtifactCoverage(fragments []rc.Fragment, sources []rc.Source) json.RawMessage {
	stubs := map[string]rc.Source{}
	for _, f := range fragments {
		s := f.Source
		s.Text = ""
		stubs[s.ID] = s
	}
	for _, s := range sources {
		s.Text = ""
		stubs[s.ID] = s
	}
	var rows []rc.Source
	for _, s := range stubs {
		rows = append(rows, s)
	}
	receipts := make([]rc.Fragment, len(fragments))
	for i, f := range fragments {
		f.Source.Text = ""
		receipts[i] = f
	}
	raw, _ := json.Marshal(growthCoverage{Fragments: receipts, Sources: rows})
	return raw
}
func validateGrowthPage(page rc.SourcePage, cursor rc.Cursor, total int64) error {
	if cursor.Ordinal == 0 {
		cursor.Ordinal = 1
	}
	next := cursor
	for _, f := range page.Fragments {
		if f.Source.Ordinal != next.Ordinal || f.OffsetBytes != next.OffsetBytes || f.NextOffsetBytes-f.OffsetBytes != len(f.Source.Text) || f.NextOffsetBytes > f.Source.ByteLength || f.Complete != (f.NextOffsetBytes == f.Source.ByteLength) {
			return fmt.Errorf("non-contiguous source coverage")
		}
		if f.Complete {
			next = rc.Cursor{Ordinal: next.Ordinal + 1}
		} else {
			next.OffsetBytes = f.NextOffsetBytes
		}
	}
	if next != page.Next || page.Done != (next.Ordinal > total) || (!page.Done && len(page.Fragments) == 0) {
		return fmt.Errorf("invalid source cursor")
	}
	return nil
}
