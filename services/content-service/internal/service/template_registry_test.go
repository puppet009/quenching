package service

import (
	"context"
	"github.com/quenching/go-common/templates"
	"testing"
)

type ownedTemplateRegistry struct{ templates.Registry }

func (ownedTemplateRegistry) Get(slug string) (templates.Manifest, bool) {
	return templates.Manifest{Slug: "mall", Services: []templates.ServiceSpec{{Workload: "mall-portal"}, {Workload: "mysql"}}}, slug == "mall"
}
func TestTemplateBindingUsesOwnedRegistryWithoutServiceDNS(t *testing.T) {
	resolve := RegistryTemplateResolver(ownedTemplateRegistry{})
	got, e := resolve(context.Background(), "mall")
	if e != nil || len(got) != 2 || got[0] != "mall-portal" || got[1] != "mysql" {
		t.Fatalf("owned template: %v %v", got, e)
	}
	if _, e = resolve(context.Background(), "disabled"); e == nil {
		t.Fatal("unknown template accepted")
	}
}
