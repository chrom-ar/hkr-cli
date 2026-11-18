package api

import "encoding/json"

// The types follow passes on keep the JSON they came from, so --json
// prints HKr's JSON as is.

// UnmarshalJSON decodes a step and keeps its JSON in Raw.
func (s *Step) UnmarshalJSON(b []byte) error {
	type plain Step
	if err := json.Unmarshal(b, (*plain)(s)); err != nil {
		return err
	}
	s.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// UnmarshalJSON decodes a scan and keeps its JSON in Raw.
func (s *Scan) UnmarshalJSON(b []byte) error {
	type plain Scan
	if err := json.Unmarshal(b, (*plain)(s)); err != nil {
		return err
	}
	s.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// UnmarshalJSON decodes a finding and keeps its JSON in Raw.
func (f *Finding) UnmarshalJSON(b []byte) error {
	type plain Finding
	if err := json.Unmarshal(b, (*plain)(f)); err != nil {
		return err
	}
	f.Raw = append(json.RawMessage(nil), b...)
	return nil
}
