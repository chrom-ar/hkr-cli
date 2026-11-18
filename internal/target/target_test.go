package target

import (
	"context"
	"errors"
	"testing"

	"github.com/chrom-ar/hkr-cli/internal/api"
)

type fakeAPI struct {
	orgs    []string
	meCalls int
}

func (f *fakeAPI) Me(context.Context) (*api.Me, []byte, error) {
	f.meCalls++
	me := &api.Me{}
	for _, o := range f.orgs {
		me.Organizations = append(me.Organizations, api.Organization{Slug: o})
	}
	return me, nil, nil
}

func TestOrg(t *testing.T) {
	ctx := context.Background()
	one := &fakeAPI{orgs: []string{"acme"}}
	if got, _ := (&Resolver{API: one, Flag: "flag", Env: "env", Default: "def"}).Org(ctx); got != "flag" {
		t.Errorf("flag: %s", got)
	}
	if got, _ := (&Resolver{API: one, Env: "env", Default: "def"}).Org(ctx); got != "env" {
		t.Errorf("env: %s", got)
	}
	if got, _ := (&Resolver{API: one, Default: "def"}).Org(ctx); got != "def" {
		t.Errorf("default: %s", got)
	}
	if one.meCalls != 0 {
		t.Error("asked /me with the org given")
	}
	r := &Resolver{API: one}
	if got, _ := r.Org(ctx); got != "acme" {
		t.Errorf("only: %s", got)
	}
	_, _ = r.Org(ctx)
	if one.meCalls != 1 {
		t.Errorf("/me asked %d times, want once", one.meCalls)
	}

	_, err := (&Resolver{API: &fakeAPI{orgs: []string{"a", "b", "c"}}}).Org(ctx)
	if !errors.Is(err, ErrAmbiguous) || err.Error() != "you're in 3 organizations; pass --org a (or b, c)" {
		t.Errorf("several: %v", err)
	}
	_, err = (&Resolver{API: &fakeAPI{}}).Org(ctx)
	if !errors.Is(err, ErrAmbiguous) || err.Error() != "you're in no organizations" {
		t.Errorf("none: %v", err)
	}
}

func TestParseFinding(t *testing.T) {
	const origin = "https://app.hkr.test"
	for ref, want := range map[string]string{
		"Fx7Kq2XpAb2m":                                         "Fx7Kq2XpAb2m",
		origin + "/findings/Fx7Kq2XpAb2m":                      "Fx7Kq2XpAb2m",
		origin + "/findings/Fx7Kq2XpAb2m/":                     "Fx7Kq2XpAb2m",
		origin + "/domains/Dm2Kq2XpAb2m/findings/Fx7Kq2XpAb2m": "Fx7Kq2XpAb2m",
		"HTTPS://App.Hkr.Test/findings/Fx7Kq2XpAb2m?x=1":       "Fx7Kq2XpAb2m",
	} {
		got, err := ParseFinding(ref, origin)
		if err != nil || got != want {
			t.Errorf("ParseFinding(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
	for ref, msg := range map[string]string{
		"nope!":        `"nope!" isn't a finding id or a finding URL`,
		"Fx7K":         `"Fx7K" isn't a finding id or a finding URL`,
		"Fx7Kq2XpAb20": `"Fx7Kq2XpAb20" isn't a finding id or a finding URL`, // 0 isn't in HKr's alphabet.
		"https://other.test/findings/Fx7Kq2XpAb2m": "that finding is on https://other.test, but the HKr in use is " + origin + "; pass --url https://other.test",
		origin + "/domains/Dm2Kq2XpAb2m":           `"` + origin + `/domains/Dm2Kq2XpAb2m" isn't a finding URL (expected …/findings/<id>)`,
	} {
		_, err := ParseFinding(ref, origin)
		if !errors.Is(err, ErrBadRef) || err.Error() != msg {
			t.Errorf("ParseFinding(%q) = %v; want %q", ref, err, msg)
		}
	}
}
