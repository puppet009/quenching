package operationevidence

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"
)

func CaptureText(id, slot string, raw []byte, limit int) Text {
	if limit < 0 {
		limit = 0
	}
	if limit > TextLimit {
		limit = TextLimit
	}
	v := Text{Ref: "optext:" + id + ":" + slot, OperationID: id, Slot: slot, Coverage: EmptyCoverage()}
	v.Coverage.OriginalBytes = int64(len(raw))
	v.Coverage.Output = "complete"
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		v.Coverage.Output = "unavailable"
		v.Coverage.ReasonCodes = []string{"binary_text_omitted"}
		return v
	}
	data := string(raw)
	if len(raw) > limit {
		data = data[:limit]
		if i := strings.LastIndexByte(data, '\n'); i >= 0 {
			data = data[:i+1]
		} else {
			data = ""
		}
		v.Coverage.Output = "partial"
		v.Coverage.ReasonCodes = []string{"output_limit_exceeded", "unsafe_record_boundary"}
	}
	v.Content = RedactText(data)
	if len(v.Content) > limit {
		v.Content = ""
		v.Coverage.Output = "partial"
		v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "output_limit_exceeded")
	}
	v.ByteLength = len(v.Content)
	v.Coverage.SavedBytes = int64(v.ByteLength)
	return v
}
func ReadFragment(t Text, offset, limit int) (Fragment, error) {
	if limit < 1 || limit > 8192 || offset < 0 || offset > len(t.Content) || !utf8.ValidString(t.Content[:offset]) {
		return Fragment{}, errors.New("invalid source range")
	}
	end := offset + limit
	if end > len(t.Content) {
		end = len(t.Content)
	}
	for end > offset && !utf8.ValidString(t.Content[offset:end]) {
		end--
	}
	if end == offset && end < len(t.Content) {
		return Fragment{}, errors.New("invalid UTF8 boundary")
	}
	return Fragment{t.Ref, t.Content[offset:end], offset, end, end == len(t.Content)}, nil
}
