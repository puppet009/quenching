package config

import "testing"

func TestTemplateMediaIsPublicWithoutOpeningTemplateConfiguration(t *testing.T) {
	t.Setenv("ROUTE_0", "")
	media, templates := false, false
	for _, rule := range loadRoutes() {
		if rule.PathPrefix == "/api/v1/template-media" {
			media = !rule.Auth && rule.TargetURL == "http://localhost:8082"
		}
		if rule.PathPrefix == "/api/v1/templates" {
			templates = rule.Auth
		}
	}
	if !media || !templates {
		t.Fatal("only curated template media should be public")
	}
}
